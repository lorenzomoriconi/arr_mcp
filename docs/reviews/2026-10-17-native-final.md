# Final focused native-cutover review

Actual Claude Code `claude-opus-5-5`, effort `xhigh`, read-only public-source snapshot. The final review found no code blocker and requested the cleanup/rollback limitation in the public runbook; that documentation is included with this report.

I ran nothing and edited nothing. This review is based only on reading the snapshot.

# Final focused review: FINAL_DELTA.patch

## Verdict

**No code blocker. The delta is OK to merge once full CI passes on the exact commit.** S1–S5 are closed in code, and I found no new defect.

**One doc item is required by your own condition before release (D4).** `REVIEW_CONTEXT.md` says the public docs must state the recycle-bin limitation honestly. They don't yet: "recycle bin" appears nowhere under `integrations/` or `docs/reviews/`, and nothing says connector rollback isn't media rollback. One sentence near `integrations/README.md:17` or `:31` fixes it.

## Closure of S1–S5

- **S1 is closed.**
  - New profiles must use the `Hermes - ` prefix (`media_arguments.go:41-44`).
  - Updates read the live profile and refuse anything outside that namespace, and refuse renames into or out of it (`:97-103`). The assignment check still runs after that.
  - Precedence in `:101` is correct (`A || (B && C)`).
  - None of the tools that edit custom formats, quality definitions or delay profiles is in `config.hermes.example.yaml`, so presets can't be changed indirectly.
  - Tests cover allowed and refused updates on both Sonarr and Radarr (`guarded_media_integration_test.go:342-368`).
- **S2 is closed.** Add-series and add-movie calls refuse `tags` and `searchNow` before the root-folder check (`media_arguments.go:45-60`).
- **S3 is closed.**
  - Install skips the cache in its strict re-check, then re-reads the cache after the stop. It backs up that latest copy, updates the manifest entry by reference, and removes only `arr` (`vm_install.py:222-244`).
  - Rollback now runs verify → stop → verify → write (`:290-294`).
  - The automatic rollback passes `gateway_stopped=True` (`:258`), so a leftover turn lease can't block it.
  - Moving `trusted_folder` inside the `try` is safe: those directories were already checked before the stop (`:200-205`).
- **S4 is closed.** In guarded mode, `guardedToolNote` is appended to tool descriptions (`tools.go:64-67`), and all five tool names it matches are in the example catalogue.
- **S5 is closed.**
  - The watch-policy check (`vm_install.py:151-153`) is the same as `native_client.py:46-51`.
  - `__HERMES_PYTHON__` is the only placeholder in `SKILL.md` (`:47`).
  - The token must be exactly root:service-group 0640.
  - All three checks run before `systemctl stop`, and tests assert the stop isn't called.
- **The S6 test nits are fixed.** Group restoration is now actually tested (gid 1234 → 0, `test_vm_install.py:81`), and the missing-cache path has a test (`:98`).

## Non-blocking items

1. **The namespace rule for the operator isn't in the public docs.** The README should say:
   - Don't point import lists, Radarr collections or Seerr defaults at `Hermes - …` profiles.
   - Before cutover, check that no existing operator profile already starts with that prefix. Any that does becomes editable.
2. **S2 is worded too strongly in the review context.** "Downloads require a separate explicit scoped search" isn't quite true: native RSS sync can still grab any monitored, missing item. The real limit is the `monitor`/`monitored` value on the add call. `README.md:17` doesn't claim more than that, but one clause saying so would help.
3. **Test gaps:**
   - Nothing tests the automatic rollback after a write error with an unexpired lease, even though S3 relies on it. A test could fail the Nth `write_atomic`, insert a lease, and assert that every target is restored.
   - No test checks that a bad token mode or group is refused.
   - `TestGuardedProfileCannotChangeAssignedLibraryPreset` (`:315-322`) is now refused by the rename-out rule before the assignment check runs, so it no longer tests the assignment check on its own. The new "assigned reserved" case covers it.
   - The add-title test doesn't check error messages and has no case where a movie add succeeds.
4. **Runbook edge cases.**
   - If rollback refuses after its stop, the gateway stays stopped. Extend the manual-restart note in `README.md:28` to cover rollback too.
   - A manual rollback before the gateway is restarted still calls `assert_idle`. A lease taken just before the stop can block it until that lease expires.
5. **Leftovers with no effect:**
   - If the cache is unreadable or not valid JSON after the stop, install aborts with the gateway stopped and no target changed. That fails safe, and the D8 note covers it.
   - If the cache disappears during the stop, a stale `N.before` file is left in the backup.
   - The input schemas still describe the `tags`, `searchNow` and `["all"]` fields. The appended notes are enough.

## D3

From my knowledge of upstream cloud-init (not checked against your VM's version), `manage_etc_hosts: localhost` edits only the local-hostname line in place. It rewrites `/etc/hosts` only when the hostname changes, and keeps other entries and their trailing comments. So the `# arr-mcp-private` pin should survive reboots. A hostname change would alter the file's hash, and rollback would then refuse rather than overwrite it.