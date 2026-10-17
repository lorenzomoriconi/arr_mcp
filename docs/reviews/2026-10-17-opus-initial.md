Claude Code 2.1.295; claude-opus-5-5; xhigh; read-only static design/code review. This is the initial snapshot review; see the response and follow-up for the final state.

# Fork review: arr_mcp source baseline (static read-only review of this snapshot)

## Verdict

**Source baseline: OK to merge with conditions. Not approved for production cutover.**

- **Nothing here harms an upstream-default deployment.** With `tools`, `imports` and `bearerToken` unset, the risky paths stay off.
- **A few defaults do change without opting in.** These are strict YAML parsing, history `limit` validation, and absolute paths in file listings (L8). They are minor but should be documented.
- **Before merging:** fix M1 and M2, or correct FORK.md, which currently overstates both.
- **Before merging the first release PR:** fix M4.
- **Cutover needs everything in the "Cutover prerequisites" section.** It also needs live validation, which nothing in this snapshot provides.

I ran nothing. CI, race and lint status are only what the context file says, and I can't tell whether the final race run passed.

## Architecture and maintenance trade-off

The fork approach is the right size.
- **Policy has one choke point.** `mcp.AddTool` is called only at `pkg/server/tools.go:58`, so one check there covers both discovery and calls.
- **The guarded import is well isolated.** It lives in `guarded_import.go`, with two branches in `register_library.go` and one hook in `library.go`.

Ways to shrink the fork delta:
- **Propose the general-purpose parts upstream.** History filters and IDs, command receipts, tool allow/deny and bearer auth would be useful upstream. Only the guarded import needs to stay fork-only.
- **Avoid reordering upstream struct fields.** `HistoryRecord`, `Server` and `ServerConfig` put the new fields first. The enabled linters don't require that (`.golangci.yml` uses plain `govet`, no fieldalignment), and it adds merge conflicts.

## Findings

### Medium

**M1. History evidence keys probably don't match what Sonarr/Radarr actually send.** `pkg/arr/shared.go:42-47`, `FORK.md:10`, fixture at `pkg/arr/history_verification_test.go:13`
- **Trigger:** any real `downloadFolderImported` history record.
- **What I expect:** from memory of `HistoryService.cs`, both apps write `FileId`, `DroppedPath` and `ImportedPath`, which come out as `fileId`, `droppedPath`, `importedPath`.
- **Impact:** `episodeFileId` and `movieFileId` would always be empty. FORK.md advertises evidence that never arrives, and verification silently falls back to paths. The OpenAPI schema only types `data` as a string map, so it can't confirm key names. The test fixture invents them.
- **Fix:** check the key names against a sanitised real record or the app source. Add `FileID string \`json:"fileId,omitempty"\``, remove keys that don't exist, and base the fixture on a real response.

**M2. A misspelled `deny` entry silently blocks nothing.** `pkg/config/tool_policy.go:32-45`, `pkg/server/tools.go:38`
- **Trigger:** for example `deny: [sonarr_delete_seris]`. It passes the name pattern, matches nothing, and the tool stays available.
- **Impact:** this contradicts FORK.md:9 ("a misspelled safety setting cannot silently disappear"). The note that unknown names "grant no capability" is only true for `allow`.
- **Fix:** `register` is called for every tool whatever the service config (`register.go:13-41`). Record `meta.name` into a set on `Server` before the filters run. After `registerAll`, reject any allow/deny name not in that set. Return the error from `HTTPHandler`/`RunStdio`, or from a `Server.PolicyError()` that `main` checks.

**M3. The recent-submission hold can expire while the native import is still running, and a restart clears it.** `pkg/server/guarded_import.go:88,122,148`
- **Trigger, either of:**
  - A large copy that can't use hardlinks, or a ManualImport queued behind another disk-access command, takes longer than 5 minutes. Then someone previews and applies again.
  - The container restarts after a submission.
- **Why every check passes:** while the copy runs, the download is still `importBlocked` and the episodes still have no file. So a second ManualImport gets queued.
- **Impact:** from memory, the native ManualImport command does not re-run import specifications. The second import would either replace the file just imported, or fail after cleanup has removed the source. FORK.md does admit the hold isn't durable, but a cheap durable check exists.
- **Fix:** in `guardedImportFiles`, which covers both preview and apply, GET `/command` and refuse if any `ManualImport` is `queued` or `started`. Optionally match the `downloadId` values in its body. The endpoint is already wrapped at `register.go:459`.

**M4. release-please probably won't find a previous fork release.** `.release-please-config.json:4,10`
- **Trigger:** the first push to `main`.
- **Why:** release-please will look for `arr_mcp-v1.13.1`. That tag doesn't exist: upstream's tag is `v1.13.1`, and GitHub doesn't copy releases into forks.
- **Impact (expected):** release-please walks back through upstream history. The first release PR would carry a polluted changelog and a version bump worked out from upstream commits.
- **Fix:** add top-level `"bootstrap-sha": "90fd1578ec1e5f0dee40a7ca9fc8b384ce0834b5"`, or create a GitHub release `arr_mcp-v1.13.1` on that commit.
- *Assumption:* this is release-please behaviour as I remember it; check it against the first release PR.

### Low

**L1. A second push to `main` can cancel the release check and skip publishing.** `.github/workflows/ci.yml:24-26` together with `release-please.yml:50-58`
- **Why:** the push-triggered CI and the CI called by `verify-release` share the group `ci-refs/heads/main` with `cancel-in-progress`. The manifest bump is not in `paths-ignore`, so both runs happen.
- **Impact:** another merge to `main` during a release can cancel `verify-release`. `publish` is then skipped silently, leaving a tag with no image.
- **Fix:** use `group: ci-${{ github.workflow }}-${{ github.ref }}`. In a called workflow, `github.workflow` is the caller's name, so the groups no longer collide.

**L2. The binary reports the wrong version string.** `release-please.yml:106,142`
- With the component in the tag, the binary and MCP `initialize` report `arr_mcp-v1.x.y`.
- **Fix:** `VERSION=v${{ needs.release-please.outputs.version }}`.

**L3. One mutex is held across all network calls in apply.** `guarded_import.go:114-155`
- **Impact:** apply can make up to 10 queue pages of 30-second calls, plus a 2-minute manualimport scan, while holding the lock. A slow Sonarr stalls every preview and apply for every service and instance.
- **Fix:** reserve the download key under the lock, do the I/O unlocked, then re-check the plan under the lock before the POST.

**L4. Queue-to-target binding allows a row with no target.** `guarded_import.go:304`
- A row with `seriesId`/`movieId` of 0 passes, so the target rests on the caller's hint alone. The default queue excludes unknown items, so this is rarely reachable.
- **Fix:** require `row.SeriesID == targetID` (Sonarr) or `row.MovieID == targetID` (Radarr).

**L5. A plan survives a failed validation.** `guarded_import.go:125-135`
- On a snapshot mismatch or refusal, the plan stays live even though the error says "obtain a fresh preview". If the state flips back within the TTL, the old plan becomes usable again.
- This is safe, because the snapshot is identical, but it's inconsistent.
- **Fix:** delete the plan on a content refusal, but not on transport errors.

**L6. The guarded payload drops native quality details.** `guarded_import.go:138`, `library.go:380-393`
- Only quality and language names are resubmitted. Quality revision (PROPER/REPACK), release type and indexer flags are lost, which can lead to unnecessary "upgrade" grabs.
- This is an existing upstream limitation, but here the server owns the payload and could carry these fields through.

**L7. `omitempty` on the `Data` struct does nothing.** `shared.go:55`
- Every record emits `"data":{}`. Use `omitzero` (the module targets Go 1.26).

**L8. Bearer-token handling details.**
- `server.go:85`: the scheme check is case-sensitive, so `bearer x` gets a 401. RFC 7235 makes the scheme case-insensitive.
- `loader.go:224`: there's no minimum token length.
- Literal tokens are accepted, although FORK.md says the token is "expanded from an operator environment variable".

**L9. Some default behaviour changes without opting in, despite FORK.md:14.**
- `loader.go:140-141`: unknown keys and top-level YAML anchors are now fatal.
- `shared.go:149`: a history `limit` below 0 or above 1000 is now an error.
- `shared.go:585`: file listings now include absolute paths.
- **Fix:** document these as deliberate exceptions.

**L10. Tool descriptions and schemas don't change with the mode.** `register_library.go:114,135`
- The guarded-mode text appears even when guarded mode is off.
- In guarded mode, the schemas still advertise `folder`, `files`, `importMode` and `filterExistingFiles`.
- The preview tool is annotated read-only and idempotent, yet it creates plans and uses up the 256-plan cap.

**L11. Command receipts are too thin to match an uncertain submission.** `shared.go:63-67`, `register.go:459`
- After an "uncertain" error, the operator is told to inspect commands. The receipts have no queued, started or ended times, no `result` and no `message`, so there's no way to tell which ManualImport was ours.
- **Fix:** add those fields to the receipt (still without the command body). Include the `downloadId` and the submission time in the uncertain error.

**L12. The guarded import leaves no audit trail.**
- Nothing is logged at plan creation, submission or an uncertain outcome.
- `main.go:80-81` logs permissions but not the tool policy, guarded mode, or whether HTTP auth is on.

**L13. Documentation and test housekeeping.**
- FORK.md:14 says "The example below opts in", but FORK.md has no example.
- `docs/configuration.md` doesn't cover `tools`, `imports` or `bearerToken`.
- The fixture serves `/api/v3/series/1` (`guarded_import_test.go:46`), but no code calls it. That suggests a dropped check, which would also be worth adding: refuse sources under the target's own series or movie path.
- **Missing tests:**
  - a queue row mapped to a different target;
  - a multi-page queue and the "queue too large" case;
  - multi-episode files and duplicate episode mappings;
  - two applies running concurrently;
  - confirm mode with a guarded import: a decline must leave the plan intact, and an approval arriving after expiry must fail.

## Cutover prerequisites (separate from merging)

1. **High: allowed write tools can get around the guard.**
   - `config.hermes.example.yaml:52` allows `qbittorrent_add_torrent`, which passes the caller's `savePath`, `category` and `autoTMM` through unchanged (`register_qbittorrent.go:85-89`).
   - A `savePath` inside a library folder puts files there without any guard check. Sonarr/Radarr's scheduled refresh then picks them up in place.
   - When a file with the same name already exists, qBittorrent re-checks it and overwrites pieces that don't match. That can damage existing library files.
   - **Fix:** either remove the tool from the allowlist and acquire through `*_grab_release`, or add a save-path fence modelled on Synology's `allowedPaths` (`loader.go:357-390`).
   - Also keep these out of the allowlist: `*_run_command` (its `ProcessMonitoredDownloads` imports with the app's own mode, which may be move), `*_refresh_*`, root-folder tools and media-management config tools.
   - Confirm that `*_update_quality_profile` (lines 32 and 49) is meant to be allowed: it can trigger native upgrades that replace library files.
2. **Medium–High: what Telegram asks the user to approve is not tied to the plan.**
   - In confirm mode the prompt is the generic "Allow sonarr_manual_import to run?" (`permissions.go:124-125`, `tools.go:73-82`).
   - The plan ID is opaque, so the human approves whatever plan the agent picked.
   - **Fix:** render the plan in the prompt: service/instance, downloadId, target, file count, copy mode and accepted cleanup.
   - Check the 5-minute TTL against how long Telegram replies actually take.
3. **Repository and supply-chain controls for Hermes's write access:**
   - Hermes's token must **not** have the GitHub `Workflows` permission. Otherwise a same-repo PR can change `ci.yml`, which runs before review and can request `packages: write`.
   - Protect `main` with a required review from you.
   - Add CODEOWNERS for `.github/`, `pkg/config/`, `guarded_import.go` and `FORK.md`.
   - Restrict which repositories' Actions can publish to the GHCR package.
   - Deploy on TrueNAS **by digest only**, since version tags can be overwritten. Settle GHCR visibility and pull credentials.
4. **Live checks against the exact Sonarr/Radarr versions on the NAS** (everything so far is fake-API only):
   - `trackedDownloadState: importBlocked` exists;
   - manual-import candidates carry `downloadId`;
   - the snapshot is identical across two scans (candidate `id`, language order, quality detection);
   - the history `data` keys (M1);
   - `totalRecords` is present on the queue.
5. **Hermes's MCP client:**
   - It must support elicitation/input requests. With `fallback: deny`, every write fails otherwise.
   - It must send the bearer header over Streamable HTTP.
   - The download watches need rebuilding on the native tools.
6. **Fix M3 and L12, and confirm** "Remove Completed Downloads" and the qBittorrent seeding limits in each app. `acceptCleanup` accepts cleanup across the board.
7. **Explicitly sign off the gaps against the old broker.** This fork does no qBittorrent manifest, category, per-file completion or symlink checks.

## Unverified assumptions

- **History key names (M1)** and the claim that the native ManualImport command skips import specifications (M3) are from memory of the Sonarr/Radarr source.
- **The release-please behaviour (M4)** and the concurrency-group behaviour in called workflows (L1) are as I remember them.
- **qBittorrent overwriting pieces** of existing same-named files on add is libtorrent's usual behaviour, not checked against your settings.
- **The pinned upstream digest** in `deploy/kubernetes/deployment.yaml` was not checked.
- **CI results:** none of the race, vet, lint or Trivy results were seen by me.

## What holds up

- **Policy enforcement:** checked both at registration and at call time. Deny and `denyDestructive` take precedence, and an omitted `allow` (upstream behaviour) is correctly distinguished from an empty `allow: []` (deny everything).
- **Plan handling:**
  - the plan is consumed before the POST;
  - a failed POST is reported as uncertain and can't be replayed;
  - the payload is built by the server, and copy mode is fixed;
  - the guarded HTTP endpoint refuses to start without a token.
- **Source and target checks:**
  - source paths and library roots are validated;
  - only missing episodes or movies are eligible;
  - one file per target, with duplicate paths refused.
- **Trimming:** history is trimmed through a typed struct, so download URLs can't leak. Command receipts are trimmed too.
- **Auth gate:**
  - every `/mcp` method is behind the bearer check;
  - the token comparison is constant-time;
  - more than one `Authorization` header is rejected.
- **Release:** publishing now waits for CI to pass.