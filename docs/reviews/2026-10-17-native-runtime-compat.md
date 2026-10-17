# Native real-platform compatibility review

Actual Claude Code `claude-opus-5-5`, effort `xhigh`, read-only frozen-source review. Its blocker was ensuring the new authentication test file was tracked; that file is included in this patch. The file-permission/path/test-isolation follow-ups are resolved below.

**Verdict: approve once the blocker below is fixed.** I found no weakening of authentication beyond the explicit trusted-source opt-in, no lost headers, no change to retries, and no regression in config-file isolation. I only read the snapshot; I didn't run or edit anything. My file access was limited to the snapshot folder.

### Blocker
1. **The new unit test file isn't in `DELTA.patch`.** `pkg/arr/qbittorrent_bypass_test.go` is in the snapshot, so it's probably untracked. It's the only test that covers skipping the login, not retrying after 401/403, keeping the Origin/Referer headers, and not affecting credentialed or other services. Add it to git and make sure it's in the pinned reviewed content before the 1.15.1 merge.

### Should fix (not blocking)
2. **`compose.py:27` `'mode': 0o440` does nothing.** Docker Compose bind-mounts file-based configs read-only and ignores `mode`/`uid`/`gid`, logging only a warning. The real permissions come entirely from the host file. Either remove the key or add a comment saying so, and make the host-side permission check a required step. On TrueNAS datasets, also confirm the file has no extended ACL (`acl: false` from `filesystem.stat`), because the mode bits alone don't prove 0440 there.
3. **The path check is looser than described.**
   - It rejects only `\r`, `\n` and `\x00`, not all control characters (for example `\t`, `\x01`–`\x1f`, `\x7f`).
   - It accepts `/mnt/config.yaml` directly under `/mnt`, which isn't on a pool. Consider requiring `/mnt/<pool>/<dir>/config.yaml`.
   - The risk is low because this is operator input.
4. **`test_compose.py:110` now passes `''` as `config_file`.** An empty path is itself invalid, so the test would still pass if the image or bind-address checks were removed. Pass a valid path instead.

### Checked and OK
- **Bypass condition (`client.go:469-470`):** it requires all of: the qbittorrent service name, the explicit flag, and all three credential fields empty. `loader.go:318` enforces the same rule. When bypass is off, `useSession` is identical to the old condition, so credentialed qBittorrent, the API-key services and DSM behave as before.
- **Bypass requests:** `send` still sets Origin and Referer (it checks the service's auth type) and sends no Cookie. A 401 or 403 becomes an error with no new login and no replay. A failed login never falls back to bypass.
- **Session cache:** bypass mode never stores a session. `redact` creates one empty entry, which is harmless.
- **`configs.file`:**
  - Compose doesn't substitute variables inside the file, so the `${VAR}` references reach the server unchanged.
  - `$` is rejected in the path, so the path itself isn't substituted either.
  - The read-only mount of a single file doesn't expose its parent folder, and the 0700 parent doesn't stop Docker from mounting it.
  - This also explains the original failure: Compose copies `content` configs into the container, which can't work on a read-only filesystem.

### Check before cutover (outside the snapshot)
- **Every qBittorrent client must be built through `InstanceCredentials`.** That includes the download-evidence client, `--check`/`Ping`, and guarded validation. The snapshot only shows `tools.go:86`. Any direct `arr.Credentials{...}` would drop the flag. That fails safely, but it would break staging. The guarded test fixture now answers `Fails.`, which only covers this if its qBittorrent instance has `authBypass` set.
- **Bypass whitelist scope:** on a bridge network, qBittorrent sees a Docker NAT or bridge address as the source. If the existing whitelist covers a Docker subnet, every container on the NAS can use the qBittorrent API without credentials. That policy existed before this change, but confirm it's no wider than needed.

## Resolution and verification

The commit tracks pkg/arr/qbittorrent_bypass_test.go and its server mapping regression. Every production qBittorrent constructor, including CLI --check and guarded evidence, uses InstanceCredentials. File configs omit the ineffective Compose mode option; the runbook requires host root:65532 0440 and acl=false. Paths require a dedicated folder under /mnt/<pool>/, reject ASCII control/backslash/dollar characters, and image/address tests use a valid path. All 13 operator tests pass. Full Go race/vet/lint/gosec/actionlint pass.

A separate existing CI flake came from a fresh fake Synology appliance inheriting an earlier fake endpoint's process-wide session when the OS reused its port. Fixture setup/cleanup now clears only its own key; production Synology code is unchanged. Ten randomized client-suite runs under the race detector passed after isolation. The readonly native NAS staging app is running; final qBittorrent/manifest and VM route checks require the patched published image and precede live cutover.
