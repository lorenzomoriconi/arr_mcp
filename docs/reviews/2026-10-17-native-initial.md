# Initial native-cutover review

Actual Claude Code `claude-opus-5-5`, effort `xhigh`, public-source-only frozen snapshot. Completed 2026-10-17. This is the original report; the final response/follow-up records subsequent changes and supersedes deployment findings. No live deployment was approved by this initial report.

I ran nothing and edited nothing; this review comes from reading the source in the snapshot only.

# Native Hermes cutover review

## Verdicts

- **Source merge: OK once the full race/lint rerun passes.** I found no merge blocker in the Go server code. The magnet plans and guarded imports hold up when I trace them: plans are single-use and rechecked, a spent or uncertain plan can't be replayed, a receipt requires the exact hash to be visible, and imports are bound to qBittorrent's file list. Please put the small fixes for H2, M2 and L1 in the same PR, since the operator scripts are what will actually be run.
- **Deployment: not approved.** H1 and H2 block it. M2 should be fixed or checked before cutover, and M1 is strongly recommended.

## Findings

### H1 — High, blocks deployment: the agent can approve its own writes
- **Where the token is readable from:**
  - `vm_install.py:150-159` writes the token from `broker.token` into `~/.hermes/.env` as `ARR_MCP_HTTP_TOKEN`. That is the token Hermes uses for writes.
  - `vm_install.py:207,213-214` makes `/etc/hermes-arr` readable by the Hermes group.
  - `native_client.py:72` reads that same token.
  - `SKILL.md:8,45-47` has the agent run the watch helper from its own terminal. So the agent's terminal must be able to read the token, or watch creation doesn't work.
- **Why that matters:** `confirm.go:30-44` and `permissions.go:112-121` accept any `inputResponses["arr-mcp-confirm"]` with `action: accept`, even on the first call. Nothing ties an approval to a question the server actually asked.
- **Consequence:** the agent can read the token and send its own "accept". It can be steered into doing that by injected text in release titles, torrent names or chat messages. Every allowlisted write then runs without a Telegram tap, including `*_update_quality_profile` (changes every title on that profile), `*_grab_release`, and `*_add_magnet` with `acceptReplacementRisk=true`. The server-side guards still limit the damage, but the human approval step reduces to the skill's instruction "never fabricate approval responses".
- **Fix:** no server-side change can stop someone who holds the token, so the token has to stay out of the agent's reach:
  1. Add server support for several bearer tokens, each with its own server-enforced tool list. Give the watcher a read-only token.
  2. Run the watch helper as a dedicated user through a narrow sudo rule, or as a Hermes tool, instead of from the agent's terminal.
  3. Run the Hermes terminal in an isolated backend or under a separate user, so it can't read `~/.hermes/.env`, `/etc/hermes-arr` or the gateway's `/proc/*/environ`.
- **If you keep the design:** record in writing that approval is advisory, and keep in the allowlist only writes whose server-side guards are acceptable without a human.

### H2 — High, blocks deployment: rollback refuses to run after the gateway has started
- **Where:** `vm_install.py:177-185` saves the schema cache with the `arr` entry removed. `vm_install.py:231-243` refuses to roll back if any tracked file differs from both its "before" and "after" copies.
- **What goes wrong:** once the gateway starts, Hermes rebuilds `mcp_schema_cache.json` with the new `arr` schema. That file now matches neither copy.
- **Consequence:** the README's step 5 rollback ("start the gateway, check discovery, roll back on failure") stops with "Installation file changed after cutover" — exactly when it's needed.
- **Tests:** `test_vm_install.py` only covers `replace_arr_config`. Nothing tests `install()` or `rollback()`.
- **Fix:**
  - Treat the cache as disposable: on rollback, restore the "before" copy or delete it, without the hash check.
  - Back up and restore the mode and group of `/etc/hermes-arr`, which `install()` changes.
  - Add `install()`/`rollback()` tests against a fake root, including the case where the cache was rewritten after start.

### M1 — Medium: the approval prompt doesn't say what is being approved
- **Where:** `permissions.go:124-125`. The prompt is only "Allow <tool> to run? This is a write operation".
- **What's missing:** the magnet title, hash and episode scope; the replacement or cleanup flags; the profile changes.
- **Other gap:** the approval isn't tied to the arguments, so a retry with different arguments is accepted.
- **Consequence:** the Telegram tap approves a tool name, not an action. The skill's "reuse clear agreement already present" makes approving without looking more likely. The earlier review raised this and it is still open.
- **Fix:**
  - Add an optional `describe(ctx, In)` hook to `toolMeta` that builds the prompt from server-owned state. For example, look up a pending magnet or import plan without consuming it and show its title, hash, units and accept flags.
  - Tie the approval to a hash of the tool and its canonical arguments, using `requestState` if SDK v1.7.0 supports it, or a short-lived server-side nonce otherwise.

### M2 — Medium: the installer never writes the watcher's endpoint file
- **Where:** `native_client.py:20,54-63` reads `/etc/hermes-arr/endpoint.json` (`url`, `address`). The change set at `vm_install.py:157-176` validates and pins `payload['endpoint']` but doesn't write that file, back it up or roll it back.
- **Consequence:** watches either fail, or silently use a leftover file from the old bridge that may not match the newly pinned hostname and address.
- **Fix:** write `endpoint.json` from `payload['endpoint']` (root:hermes, 0640) in the same backed-up change set, and check that its URL equals `server['url']`.

### Low (not blocking)
- **L1 — guarded argument checks skip destructive tools.** `tools.go:83` runs `validateMediaInput` only for `AccessWrite`. If an operator later allows a destructive tool, `["all"]` and bulk selectors get through. Fix: use `meta.access != AccessRead`.
- **L2 — qBittorrent controls aren't limited to the media categories.** `media_arguments.go:24-27` and `register_qbittorrent.go:28-42,97-107` let stop, start, recheck and reannounce act on any exact hash. `qbittorrent_list_torrents` shows every category to shared chats. Fix: in guarded mode, require `exactTorrent(hash).Category` to be one of the apps' own categories, and filter the listing the same way.
- **L3 — guarded settings can be weakened silently.**
  - `loader.go:227-229` requires `downloads.client` for `downloads.guarded` but not for `imports.guarded`. Without it, the file-list binding is quietly skipped (`import_manifest.go:61-63`).
  - Guarded HTTP doesn't require `allowedPeers` (`server.go:113`), and it is commented out in the example config.
  - Fix: require both in config validation.
- **L4 — a watch can't be created for about 5 minutes after `add_magnet`.**
  - `guarded_magnet.go:278-280` refuses a preview while the recent-submission hold is active.
  - The watcher (`download_watch.py:74-92`) then falls back to the app's queue, which may not show the torrent yet. Watch creation fails with "Hash cannot be tied".
  - Fix: return a mapping without creating a plan when the hash is already present, or add a read-only mapping tool.
- **L5 — the completeness check pages through the general queue.** `guarded_import.go:346-386` reads `/queue` with the default sort, which shifts as downloads progress, and caps the scan at 1,000 rows. It fails safe, but `/queue/details?seriesId|movieId` (already used by `queueOverlap`) is unpaged and stable.

## Architecture assessment

**What works:**
- Stateless JSON HTTP on POST `/rpc` fits the existing HAProxy rule without widening it.
- Peer filtering uses the real TCP peer and runs before the bearer check, and the bearer check is constant-time.
- Magnet and import plans are owned by the server. They are fingerprinted, checked again just before the POST, single-use, and logged as "uncertain" without retrying.
- The image is pinned by digest, runs as non-root with a read-only filesystem, and has no host mounts.
- Quality revision, indexer flags and release type are kept through imports.
- The watcher can only end, notify or time out; it never acts on downloads or files.

**The structural weakness** is the trust boundary on the VM. The human-approval step relies on the client, and the same write-capable token is available to code the agent runs (H1).

**Fragility:** the watcher depends on internal Hermes modules (`tools.mcp_tool._ensure_mcp_sdk`, `cron.jobs`). A Hermes upgrade would make it fail closed rather than misbehave, but it's worth re-checking after each upgrade.

## Not verified
- Whether go-sdk v1.7.0 decodes forged `inputResponses` the same way as genuine ones, and whether it offers `requestState`. H1 doesn't depend on this, because the token holder can complete the real round trip.
- Hermes behaviour: the terminal backend, the cache rewrite timing in H2, and what the approval message shows in Telegram.
- How Sonarr and Radarr actually shape their responses.