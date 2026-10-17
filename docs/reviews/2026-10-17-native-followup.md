# Native-cutover follow-up review

Actual Claude Code `claude-opus-5-5`, effort `xhigh`, frozen public-source-only snapshot, completed 2026-10-17. See the response record for subsequent remediation and the operational decision.

I ran nothing and edited nothing. This review is based only on reading the snapshot.

# Native Hermes cutover: follow-up review

## Verdicts

- **Source merge: OK once the full race/vet/lint/gosec/actionlint rerun passes on the exact commit.** I found no source blocker.
  - **H2 and M2 are fixed**, with tests that exercise the real failure. The rollback test rewrites the cache after install, and the endpoint file is checked on both install and rollback.
  - **L1 is fixed** at `tools.go:83`.
  - **The canonical `downloadId` fix is correct.** `importDownloadComplete` returns the queue's own casing, and that value goes to `ListManualImportCandidates` (`guarded_import.go:232,280`).
  - **The new profile and torrent fences work as written.** S1 below shows the profile fence is narrower than its documented intent.
- **Deployment: no source-level blocker remains. Cutover is approved only if all of these hold:**
  - (a) CI is green on the deployed commit and image digest.
  - (b) The operator explicitly accepts, in writing, the permission limits listed in the next section. I have not assumed that acceptance.
  - (c) The deployment checks D1–D8 are done.
  - (d) Either S1 is fixed, or D6 confirms the deployment doesn't hit its gap.
- **The rescoped H1 is acceptable as documented.**
  - `integrations/README.md:11-17`, `SKILL.md:15` and the example config's comments state honestly that there is no human approval boundary.
  - `mode: full` is the only setting that works with Hermes today, since installed Hermes doesn't handle InputRequiredResult retries.
  - `fallback: deny` and `denyDestructive` stay as a backstop.

## What the token can still do beyond the user's request types

In `mode: full`, any token holder can do everything below without anyone confirming in Telegram. So can the model, if text injected into release names, torrent names or chat steers it. Each item needs the operator's explicit acceptance or a fix:

1. **Edit operator presets, not only separate ones (S1).** The fence only checks whether a profile is assigned to a library title.
2. **Add a title with any tags** (S2). Tags can route grabs to tag-restricted download clients or indexers, and can trigger tag-scoped notifications or scripts.
   - The edit guard refuses tags; the add guard doesn't.
   - `monitor` and `searchNow` are also unbounded within one title, so a season-only request can turn into whole-series monitoring and search.
   - The monitoring tools are excluded and the edit guard refuses `monitored`, so the connector can't narrow monitoring afterwards.
3. **Force a grab** (`register_library.go:64-75`). `*_grab_release` overrides every native rejection: blocklist, size, unwanted quality, language. Unlike the magnet route (`guarded_magnet.go:244,327`), it has no server-side acknowledgment when existing files could be replaced. (Under delegation that flag would be advisory anyway.)
4. **Cause native replacement and cleanup.** This is the worst realistic outcome:
   - Assigning a profile with upgrades allowed, a forced grab or an accepted magnet can lead Sonarr/Radarr to replace, and possibly permanently delete, existing library files.
   - An accepted import can lead the apps to remove torrents and source files.
   - `denyDestructive` doesn't cover these side effects. This is documented, but it is the real upper limit of what the token can do.
5. **Control torrents after import.** Torrent controls also reach torrents in the apps' imported categories (`media_arguments.go:132-134`). So the token can stop seeding, or restart torrents that qBittorrent stopped at its share limit.
6. **Run live indexer searches through a "read" tool.** `*_list_releases` is classified as read but queries every indexer, with no rate limit, which can use up indexer API quotas.
7. **Read everything, and use the watcher's write-capable token.** All torrents and categories plus the full library are visible, and the watcher's read-only limit is enforced only in its own client (`native_client.py:23-28,72`). Both are accepted by the user's stated trust model.

Not reachable: deleting anything, changing categories or save paths, adding arbitrary torrents or URLs, adding roots, editing indexers, download clients or app config, or changing monitoring.

## Source findings (none blocks merge)

### S1 — Medium: the profile fence protects titles, not presets
- **Where:** `media_arguments.go:82-103` only scans `/series` or `/movie` for `qualityProfileId`.
- **What it misses:**
  - Sonarr/Radarr import lists and Radarr collections that reference the profile.
  - Seerr default profiles, which the server can't see from here.
  - Any operator preset that currently has no titles.
- **How a used preset can be freed:** a preset used by exactly one title can be released by assigning that title another profile through the guarded edit. The preset is then editable.
- **Consequence:** the claim "only an unassigned separate preset" isn't enforced. Changes silently affect future list or Seerr additions.
- **Fix:**
  - Also refuse a profile that `/importlist` (and Radarr `/collection`) references.
  - Better: enforce a server-owned marker. Creates must use a reserved name prefix, updates require it, and renames into or out of it are refused. This survives restarts without any server-side state.
- **Tests:** add a positive test for updating an unassigned profile, and a Radarr variant. Today only the refusal path is tested (`guarded_media_integration_test.go:310`).

### S2 — Low: guarded add doesn't refuse `tags`
- **Where:** `media_arguments.go:40-49`, against the edit guard at lines 15-22.
- **Fix:** refuse non-empty `Tags` in guarded mode. Consider requiring an explicit `monitor` value when `searchNow` is set.

### S3 — Low: install and rollback handle the cache and the stop step inconsistently
- **Install:** `vm_install.py:216-219` re-checks every file in `originals`, including the disposable cache (`:184-185`).
  - If Hermes writes its schema cache while stopping (not verified), install aborts after stopping the gateway and leaves it stopped. No target file is changed, so this fails safe.
  - Fix: exclude the cache from the strict comparison, re-read it after the stop, and build `changes[cache]` and its backup from that read.
- **Rollback:** `vm_install.py:248-263` verifies the hashes *before* `systemctl stop` and never re-checks afterwards. Anything the gateway writes during shutdown is overwritten, against the rule "refuse later edits rather than overwrite". Mirror the install order: stop, verify, then write.
- **Failure path:** `rollback(start=False)` calls `assert_idle` after the stop. A turn lease taken just before the stop can therefore abort the automatic rollback and leave a partly written install. The window is narrow.

### S4 — Low: tool descriptions contradict the guarded behaviour
- **Where:**
  - `register_qbittorrent.go:97-105` still advertises `["all"]`.
  - `register.go:130,337` say "one or more series/movies".
  - `register_profiles.go:64` doesn't mention the unassigned-profile rule.
- **Consequence:** the model will make calls the server predictably refuses, and `SKILL.md:14` says repeated failures can trip Hermes's connector cooldown.
- **Fix:** describe guarded mode in these descriptions, as `reannounce` already does.

### S5 — Low: the installer doesn't validate payload content it depends on
- **Watch policy:** it writes `watch_policy` without checking the `authorized_chats` shape that `native_client.py:46-51` requires. The test fixture even uses `{'chats': []}` (`test_vm_install.py:69`).
- **Skill placeholder:** it doesn't refuse a skill that still contains `__HERMES_PYTHON__` (`SKILL.md:47`).
- Both currently fail later, when the first watch is created. Validate at install time instead.

### S6 — Nits and test gaps
- **Stale comment:** `guarded_import.go:213-214` says the manifest isn't checked independently, but `guardedImportSnapshot` does check it.
- **Lost comments:** `replace_arr_config` drops column-0 comments that directly follow the `mcp_servers` block. Settings are preserved; only comments are lost.
- **Group restore untested:** the fixture uses `pw_gid=0` (`test_vm_install.py:59`), so restoring the directory's group isn't actually tested; only its mode is.
- **Missing tests:**
  - The "cache didn't exist before install" path, which creates the cache and then deletes it on rollback.
  - A cache rewritten during the stop.

## Deployment configuration checks

- **D1 — HTTP auth and peers.** Set `allowedPeers` to the proxy's exact IP and use a private random bearer token of at least 32 bytes.
  - The example now uses `mode: full` but still has `allowedPeers` commented out (`config.hermes.example.yaml:9`).
  - `server.go:87` only requires a token when imports are guarded.
- **D2 — Token readable by the watcher.** `broker.token` must be readable by the service group: root-owned, `0640`, with the Hermes group. The installer only checks `& 0o027`, and if the group can't read it, watches fail closed.
- **D3 — `/etc/hosts` pin.** Confirm `/etc/hosts` isn't regenerated by cloud-init (`manage_etc_hosts`) or NetworkManager, and that nsswitch consults `files` first. Otherwise:
  - The pin disappears on reboot. TLS still fails closed.
  - Rollback refuses because the file changed.
- **D4 — Recycle bin.** Enable the Sonarr/Radarr recycle bin with retention. It is the only recovery path for the native replacement risk in item 4.
- **D5 — Download client setup.** Every Sonarr/Radarr instance needs exactly one enabled qBittorrent client bound to `downloads.client`, plus completed-download handling enabled. If any instance fails the binding check, every torrent control fails closed (`media_arguments.go:125-135`).
- **D6 — Profile references (until S1 is fixed).** Check that no profile without titles, and no preset used by a single title, is referenced by import lists, Radarr collections or Seerr defaults.
- **D7 — Guard settings.** Keep `imports.guarded: true` and `downloads.client` set (L3).
- **D8 — Runbook.** If install aborts after the stop, the gateway stays stopped, so the runbook needs a manual restart step.

## Optional follow-ups
- The deferred isolation design: a separate read-only watcher token and a separate terminal identity or sandbox.
- L4 (no watch possible for about 5 minutes after `add_magnet`) and L5 (the completeness check pages the general queue). Neither is claimed fixed, and both fail safe.
- A server-side replacement acknowledgment on `*_grab_release` for titles that already have files.
- A rate limit on `*_list_releases`.
- Re-check the Hermes internals the watcher uses (`tools.mcp_tool`, `cron.jobs`, `create_job` arguments) after every Hermes upgrade.

## Not verified
- Whether Hermes writes `mcp_schema_cache.json` or `config.yaml` while stopping (matters for S3).
- How long Hermes turn leases last after a crash.
- That qBittorrent's `hashes` filter matches regardless of letter case. `exactTorrent` relies on this when given the queue's original casing.
- Whether Seerr's default profiles or import-list references exist in production (D6).