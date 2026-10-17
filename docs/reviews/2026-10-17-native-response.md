# Native Hermes review response and operating scope

Two independent actual Claude Code Opus 5.5 xhigh reviews evaluated architecture and source, followed by a focused final remediation review. Raw reports distinguish source findings, configuration checks and recommendations; this record explains the implementation and operator choices.

## Remediation

- H1: intentionally trusted delegated automation, with the same service-user token available to Hermes and its terminal. No separate human-authenticated approval, terminal sandbox or per-chat isolation is claimed. The installed Hermes native client rejects modern input-required approval results, so the bounded catalogue explicitly uses full mode with destructive tools denied. It never silently enables a confirmation fallback. The server’s argument, category, root and plan guards remain effective regardless of the client.
- H2/M2/S3: endpoint configuration is backed up/restored, schema cache is disposable on rollback and reread after gateway stop, real configuration is checked again after stop, directory mode/group are restored, and internal rollback does not wait for already-stopped gateway leases. Fake-root tests cover existing/new/rebuilt caches, shutdown edits and directory metadata.
- S1: only profiles created in the reserved `Hermes - ...` namespace may be configured; an existing operator preset cannot be claimed by renaming. Assigned profiles remain immutable through this route. Sonarr and Radarr positive/negative cases are covered. Operators must not use reserved profiles as import-list/Seerr defaults.
- S2/L1: guarded title creation refuses tags and embedded searchNow. Search is a separate scoped action. Guard checks apply to non-read access, and torrent controls require one exact hash in the native media categories of the selected client.
- S4/S5: descriptions state guarded limits; install validates rendered skill text, watch delivery policy and root:service-group 0640 token permissions before stopping anything.
- The native queue’s exact downloadId casing is preserved for scanner lookups. Quality revision/repack and indexer/release metadata survive the guarded import command. Current file IDs/paths must match exact-download history before a watch reports import success.

## Operator deployment decision

Accept this connector as a bounded delegated capability for the user-authorized media assistant purposes: explicit title/download requests, release/preset exceptions, one-hash controls, disclosed magnet/import actions, read-only diagnosis and requested finite watches. Keep conversational agreement for a new material exception. Read visibility is intentionally shared. The scoped token is not a human identity, and the watcher’s client allowlist is not a separate credential privilege.

Native release searches contact indexers and can consume their quotas. Forced grabs can override native rejections. Native upgrades/imports can replace library files, stop/start seeding or clean up sources/torrents; denying explicitly destructive tools does not prevent these native side effects. These capabilities are not authorization for automatic repairs, broad searches, background download loops or new schedules. No real media write, new watch or Telegram message is needed to test cutover.

A recycle bin or suitable snapshots are recommended for media recovery. Preserve the installation’s existing media-management settings unless separately authorized; this cutover does not enable recycle bins or claim media recovery. Connector rollback restores the previous app/client/configuration only, not media files. This is an explicit variance from the follow-up report’s blanket recycle-bin deployment recommendation, and does not create permission to perform an import or replacement.

Require exact proxy peers, verified TLS, private scoped token, explicit guarded/client settings, no host/media/admin mounts, digest-pinned nonroot image, a staging test and private rollback snapshot. Check DNS pin persistence and watcher readability. If preparation aborts before writing targets after stopping the gateway, inspect the snapshot and manually restart the unchanged gateway. Never restore retired SSH credentials.

## Remaining limits

Approval-message details and argument binding in the upstream confirmation implementation are not production guarantees; this Hermes deployment does not use confirm mode. Native writers can race validation, in-memory plans reset on restart, and no media mount permits independent symlink inspection. The short post-submit hold can temporarily delay watch creation until the native queue sees a new torrent. Large queue scans are bounded and fail closed. Hermes internal SDK/cron helpers need compatibility checks after an upgrade. Separate read-only tokens and a separate terminal identity remain a distinct future isolation project.


## Final verification

The focused final review closed S1–S5 and found no code blocker. Its requested cleanup/rollback, reserved-namespace and monitoring-scope notes are included in the public runbook/skill. The exact code at 0d1e0ed passed full Go race/vet/lint/gosec/actionlint and GitHub CI; 27 deterministic watch tests and 11 operator tests passed. A fresh ARM build passed the actual installed Hermes native connection task and tool-call handler against a fake backend, with one fake POST, zero real media writes and zero Telegram messages. Subsequent source changes only record this review and clarify documentation; final PR CI gates the merged commit.

All three reviews used actual Claude Code `claude-opus-5-5` with `--effort xhigh`, `--permission-mode plan --restricted`, empty strict MCP config, disabled hooks, and Read/Grep/Glob only. CLI results reported `is_error=false` and canonical model `claude-opus-5-5`, provider `firstParty`. They had no production credentials or private runtime definitions.
