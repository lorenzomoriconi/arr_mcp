# arr_mcp fork

This is Lorenzo's fork of [GauranshMathur/ARR_MCP](https://github.com/GauranshMathur/ARR_MCP), initially based on v1.13.1 (`90fd1578ec1e5f0dee40a7ca9fc8b384ce0834b5`). The MIT licence and upstream history remain intact. The Go module name stays unchanged to keep the patch set small; builds use this checkout's source.

## Scope and status

The first change set adds:

- A global `tools` policy that cannot be widened by instance permission overrides. `allow` accepts exact tool names; omitting it preserves upstream behaviour, while `allow: []` denies all tools. `deny` takes precedence and `denyDestructive: true` permanently excludes destructive tools. Both discovery and calls enforce the policy. Allowed tools still accept their native argument range; this policy does not add per-argument restrictions to torrent or library operations. Wildcards, duplicate entries and unknown YAML fields are rejected so a misspelled safety setting cannot silently disappear. Unknown allow or deny names are rejected against the complete registered tool catalogue before either transport starts.
- History pagination, exact download/media filters, download and media identifiers, and a small allowlist of import evidence (`fileId`, `droppedPath`, `importedPath`). Other native history data is omitted because it can contain authenticated download URLs. Episode file IDs, absolute library file paths, and read-only command receipt tools (including queued/started/ended times, result and message) support subsequent verification.
- Optional `imports.guarded: true`. Preview requires an exact native queue download and one target series/movie. The download must be completed, have zero bytes left, and be blocked on import. Only unrejected, unambiguous files for missing library items are eligible. Native active ManualImport, download-scan and monitored-download-processing commands block preview and apply; the command check is repeated just before POST. Sources inside library roots or the target's own path and duplicate mappings are refused. Applying a five-minute server-owned plan rechecks native state and the selected file metadata, fixes the mode to `copy`, and consumes the plan when validation begins, including failed validation. Caller file paths/mappings are forbidden. Explicit `acceptCleanup: true` is required because native completed-download handling may remove torrents and source files even in copy mode.
- Optional `server.bearerToken`, supporting environment references or operator-supplied literals, for `/mcp`. Guarded HTTP refuses to start without it. `/health` contains only liveness status and remains unauthenticated. Use a randomly generated token with at least 32 bytes of entropy, kept privately in an environment reference; string length alone is not treated as proof of entropy. TLS and network restrictions belong to the deployment.

These options are opt-in. Deliberate default changes are strict rejection of unknown YAML keys (including arbitrary top-level anchor-holder blocks), bounded history limits (0 defaults, 1-1000 accepted), expanded history/command evidence, and absolute paths in library file listings. [config.hermes.example.yaml](config.hermes.example.yaml) opts in. These changes are a source baseline, not a production cutover.

### Import limits

The guard trusts Sonarr/Radarr's current queue, scanner, and library database. It does **not** independently verify a qBittorrent file manifest, filesystem symlinks, torrent category, competing downloads, or native cleanup configuration. It cannot atomically exclude changes by native jobs or other API clients. These differences must be evaluated before replacing an existing connector that performs those checks.

Plans are in-memory, capped at 256, invalidated by restart, and bound to service and instance. All callers authenticated with the deployment token have the same privileges; plans are not separate user identities. A recent-submission hold prevents another plan from submitting the same download for five minutes. A failed POST is reported as uncertain and its token cannot be replayed. Active native command checks survive a server restart or hold expiry, but there is still no atomic or durable exactly-once delivery guarantee across independent API clients. Inspect native commands, exact-download history, and current library file IDs/paths before trying again. A queued or completed command alone does not prove import success.

## Deployment and updates

Source belongs at `https://github.com/lorenzomoriconi/arr_mcp`. CI tests PRs. Release-please still generates version/changelog PRs, but a maintainer runs it through their scoped GitHub access; Actions is not granted permission to create or approve PR reviews. After reviewing/merging a release PR, the maintainer runs release-please to publish its GitHub release. That event runs CI on the exact resolved tag commit, scans the amd64 image, then publishes amd64/arm64 images to `ghcr.io/lorenzomoriconi/arr_mcp` with version tags, provenance and SBOM. A manual dispatch can retry an existing fork release tag. The bootstrap SHA starts the first fork changelog after the evaluated upstream release. No image is published merely by opening a feature PR.

Release tags use `arr_mcp/vX.Y.Z`; container tags use the bare `X.Y.Z`. Maintainers use release-please 17.11.2's `manifest-pr` and `manifest-release` commands with `--repo-url lorenzomoriconi/arr_mcp --config-file .release-please-config.json --manifest-file .release-please-manifest.json` and `--token <private-token-file>`. That official CLI reads the token from a file path, so no credential needs to appear in shell arguments or the repository. Keep the short-lived token file outside the checkout; never use debug logging. Hermes can follow the same process when explicitly given repository-scoped credentials. The fork keeps `can_approve_pull_request_reviews=false`; no automated review approval or merge is configured.

TrueNAS runs the published image, not a Git checkout. Use an explicit version (or recorded digest) in the Custom App definition. Keep the previous image reference and private app configuration for rollback, update the image reference, and let TrueNAS recreate the container. There is no GitHub-to-home administrative connection or unattended NAS deployment in this setup. The upstream release image build uses upstream module linker paths intentionally.

Keep private API credentials and the MCP token in TrueNAS environment/configuration, outside the repository and image. Use a non-root container, read-only root filesystem, dropped capabilities and no host networking, media datasets, Docker socket or NAS administrator credentials. The deployment should retain private VPN access and its source/Host/path restrictions. Do not deploy the upstream wide-open Compose quickstart unchanged.

The existing Hermes connector stays active until authentication, native MCP delivery/approvals, import verification, and finite download watches have been validated for this fork. Telegram preferences, the media skill and scheduling remain outside the application fork. The previous connector's RPC client and watch helper cannot be pointed at native MCP without adaptation. No media operation is part of the initial source setup.

## Hermes contributing

Hermes can maintain this public repository through a separate development checkout and repository-scoped GitHub access. Use branches and PRs, fake app APIs and local tests. Never put credentials, private deployment dumps, home topology, download history, or actual media paths into commits. Repository write access does not require TrueNAS administrator access. Production service code is updated by replacing the released image, not by editing running containers. Maintenance and deployment are separate capabilities.

Follow `AGENTS.md` for development conventions. For this fork, retain upstream history/module naming; use release-please's fork component tags and the fork's image namespace. Do not publish to upstream's registry or rewrite upstream tags.

## Tracking upstream

Keep `origin` pointing at this fork and `upstream` at GauranshMathur/ARR_MCP. Upstream push is disabled locally. Fetch upstream branches normally and fetch future upstream tags into a namespace, for example `git fetch upstream '+refs/tags/*:refs/tags/upstream/*'`, so fork releases do not collide with them. Review and test upstream updates on a branch before merging them. Small, generally useful changes can be proposed upstream separately; making this fork does not authorize sending upstream PRs.

Run the tests and checks in `AGENTS.md`. A source or fake-API test is not evidence of a live Telegram approval or a real media import. Document validation honestly, including anything not exercised.

## Explicit follow-ups before cutover

The safe example excludes unrestricted `qbittorrent_add_torrent`: its native save path/category/automatic-management arguments need a server-side fence before Hermes gets magnet-add access. Fetching through the existing arr `grab_release` tools remains available. Profile updates are deliberately allowed, and may cause native upgrades/replacement according to the app's own rules; guarding manual import is not a prohibition on all native background writes.

The shared manual-import input schemas retain upstream optional arguments for compatibility; guarded mode refuses forbidden folder/file/mode input at runtime. Guarded preview advertises non-idempotence because it creates ephemeral plans. Native quality revision/repack and indexer metadata still follow upstream's name-based import payload limitations; this must be considered before claiming import metadata parity. Plans and audit logs contain no authorization token; operational plan/submission/uncertain-outcome events are logged without source paths or opaque plan IDs.

Before granting Hermes repository write access, use a separate scoped identity without Workflows permission, configure reviewed PRs/protected branches and ownership for release/security files, and choose GHCR package visibility/access. Deploy a recorded image digest. This source baseline does not configure those credentials or give Hermes NAS administration access.

History field names were verified from the native implementations: [Sonarr HistoryService](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/History/HistoryService.cs) and [Radarr HistoryService](https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/History/HistoryService.cs) write FileId, DroppedPath and ImportedPath. Typed Go decoding accepts their casing and emits the canonical camelCase keys; unrelated native data remains omitted.
