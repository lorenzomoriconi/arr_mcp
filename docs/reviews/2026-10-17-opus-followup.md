Claude Code 2.1.295; claude-opus-5-5; xhigh; read-only static remediation review. Final local race/lint and GitHub checks passed independently; see response.

## Verdict

**Source baseline: OK to merge once the full race/lint rerun passes.** I haven't seen that run's results. **Not approved for production cutover.**

- M1–M4 are fixed in the source, as far as a read-only review can tell.
- No new finding blocks the merge. There are three small new CI/release problems and some stale docs (N1–N4). Each fix is one or two lines, so they're worth including in this merge.
- I ran nothing; this is from reading the files only.

## Fixes checked against the source

| Item | Status | Evidence |
|---|---|---|
| M1 | Fixed | `pkg/arr/shared.go:42-46` emits `fileId`. Decoding uses `encoding/json` v1 (`pkg/arr/client.go:565`), which matches keys case-insensitively, so `FileId` and `fileId` both work. The fixture uses `FileId` (`history_verification_test.go:13`). |
| M2 | Fixed | The tool name is recorded before any filtering (`pkg/server/tools.go:39`). `AddTool` is called in one place only (`tools.go:64`), and no registration depends on config, so the catalogue is complete. Unknown names are rejected at `server.go:38,70,80`, in direct tool calls (`tools.go:71`), and in `main.go:72-76`, which runs before `--check`. |
| M3 | Fixed; remaining race is documented | Active commands are checked at preview and at validation (`guarded_import.go:212`) and again just before the POST (`:152`). An unknown command status counts as active, so it fails closed (`:185-190`). There are tests for a restart, a command starting during lookup, and a target path outside the configured roots. |
| M4 | Fixed in config | `.release-please-config.json:54-55`. Whether release-please behaves as expected won't be known until the first release PR. |
| L1/L2 | Fixed | `ci.yml:30`; `release-please.yml:115,151` (but see N1). |
| L3/L5 | Fixed | `guarded_import.go:114-131`: the lock is released before any network call, and the reservation is per service/instance. The plan is deleted when validation starts. Refusals because the app is busy or recently submitted, bad input, and confirm-mode declines (handled before `fn`) don't use up the plan. |
| L4 | Fixed | `:354` requires an exact, non-zero target. |
| L7, L8, L11 | Fixed | `shared.go:58`; `server.go:94-96`; `shared.go:62-71` and `guarded_import.go:167`. |
| L12 | Fixed | `guarded_import.go:98,162,166,170`; `main.go:89`. A caller's downloadId is only logged after it matches a native queue row, so it can't inject log lines. |
| L10 | Partly fixed, documented | `register_library.go:111` marks the preview non-idempotent. It's still marked read-only, and the schemas are shared with non-guarded mode. |
| Example | Fixed | `qbittorrent_add_torrent` is removed. `TestHermesExampleOnlyNamesExistingTools` covers the remaining names. |

## New findings (none blocking)

**N1. Low: push/PR CI no longer pins the commit that triggered it.** `.github/workflows/ci.yml:40,71,96,114`
- **Trigger:** the branch or PR merge ref moves between the event and checkout.
- **Why:** an explicit `ref:` makes actions/checkout fetch the ref's current tip instead of `github.sha`.
- **Consequence:** a status can be reported against a commit other than the one tested. `cancel-in-progress` hides most of this. The release path passes a SHA, so it isn't affected.
- **Fix:** `ref: ${{ inputs.ref }}`. When it's empty, checkout falls back to its default.

**N2. Low: a branch can shadow the release tag.** `.github/workflows/release-please.yml:41`
- **Trigger:** someone with push access creates a branch named `arr_mcp/vX.Y.Z`.
- **Why:** checkout resolves an unqualified name as a branch before a tag.
- **Consequence:** CI and publish build the branch's commit under the tag's version. The main-ancestry and manifest checks limit this to another reviewed `main` commit with the same manifest version.
- **Fix:** `ref: refs/tags/${{ steps.version.outputs.tag }}`.

**N3. Low: `latest` and the major tag can roll back.** `release-please.yml:98-102`
- **Trigger:** a dispatch that retries an older tag, or a patch release on an older version line.
- **Consequence:** `latest` and `1` move to the older image. TrueNAS pins a version or digest, so the impact is limited.
- **Fix:** in the `release` job, check whether the tag is the newest one (`git tag -l 'arr_mcp/v*' --sort=-v:refname | head -1`). Add `enable=` to the `latest` and major tag lines based on that.

**N4. Low: docs that agents follow still describe the old release flow.** `AGENTS.md:94-96`, `docs/development.md:29`, `.github/workflows/security.yml:13`
- **Problem:** these files still say that merging the release PR tags and publishes, and that the release PR regenerates by itself. Neither is true any more, and `FORK.md:38` tells Hermes to follow `AGENTS.md`.
- **Consequence:** a person or Hermes waits for a release PR that never appears, or assumes a merge published an image.
- **Fix:** add one sentence to FORK.md's Hermes section that overrides AGENTS.md's release section, and fix the comment in `security.yml`.

**Residual risk, not a finding:**
- **What's left:** the pre-POST check looks at commands again, but not at queue or target state.
- **Why it's acceptable:** the state checks run before the scan, which can take up to 2 minutes. A native import that finishes during the scan changes the candidates or rejections, and the snapshot comparison then refuses. What remains is the short gap between the lookups and the POST, which FORK.md:18,20 already admits.
- **Side effect:** the command check covers the whole app, so a running `ProcessMonitoredDownloads` will sometimes cause a temporary refusal. Retrying is safe.

## Remaining production-cutover prerequisites

1. **Live checks against the Sonarr/Radarr versions on the NAS:**
   - the casing of history `data` keys;
   - the shape of `name`, `status` and `result` from `/command`. A numeric enum would make every guarded preview fail closed;
   - the `importBlocked` state;
   - candidate `downloadId`;
   - the snapshot is the same across two scans;
   - `totalRecords`, and how many queue rows a season pack uses against the 1,000-row cap.
2. **Telegram:** the prompt is still generic and not tied to the plan. Approve, decline and expiry need live tests, and the 5-minute TTL needs checking against how long replies actually take.
3. **Hermes client:** it needs elicitation support and must send the bearer header. The download watches need rebuilding on the native tools.
4. **Write tools:**
   - Magnet-add needs a save-path/category fence before it goes back into the allowlist.
   - Keep `*_run_command`, refresh, root-folder and config tools out.
   - Quality-profile upgrades are accepted as native behaviour.
5. **Repository and supply chain:** the release job's ancestry and manifest checks run from the workflow at the tag or dispatched ref. They catch mistakes, not someone with write access. The real controls are:
   - Hermes gets a separate identity without Workflows permission;
   - `main` is protected, with required review and CI plus Security as required checks;
   - CODEOWNERS covers release and security files;
   - a tag ruleset restricts who can create `arr_mcp/v*` tags;
   - optionally, a protected `release` environment holds `packages: write`;
   - GHCR access is settled, and deployment is by digest.
6. **First release-please run:** confirm the first release PR contains only fork commits and bumps from 1.13.1. From memory, the CLI only reads `--token` as a file when the value contains a path separator, so use an absolute path. I haven't verified this.
7. **Download cleanup:** check "Remove Completed Downloads" and the seeding limits in each app, because `acceptCleanup` accepts cleanup across the board.
8. **Sign off the gaps against the old broker:** no qBittorrent manifest, category, per-file completion or symlink checks, and no quality revision or indexer metadata (L6).
9. **Token entropy** is left to deployment policy.

## Unverified

- How actions/checkout resolves refs (N1/N2), the Sonarr/Radarr command names and statuses, and the release-please behaviour are all from memory.
- I haven't seen the race, lint, actionlint or other check results for this follow-up.