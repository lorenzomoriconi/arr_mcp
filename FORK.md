# arr_mcp fork

This is Lorenzo's fork of [GauranshMathur/ARR_MCP](https://github.com/GauranshMathur/ARR_MCP), initially based on v1.13.1 (`90fd1578ec1e5f0dee40a7ca9fc8b384ce0834b5`). The MIT licence and upstream history remain intact. The Go module name stays unchanged to keep the patch set small; builds use this checkout's source.

## Scope and status

The first change set adds:

- A global `tools` policy that cannot be widened by instance permission overrides. `allow` accepts exact tool names; omitting it preserves upstream behaviour, while `allow: []` denies all tools. `deny` takes precedence and `denyDestructive: true` permanently excludes destructive tools. Both discovery and calls enforce the policy. Allowed tools still accept their native argument range; this policy does not add per-argument restrictions to torrent or library operations. Wildcards, duplicate entries and unknown YAML fields are rejected so a misspelled safety setting cannot silently disappear. Unknown but syntactically valid names grant no capability.
- History pagination, exact download/media filters, download and media identifiers, and a small allowlist of import evidence (`episodeFileId`, `movieFileId`, `droppedPath`, `importedPath`). Other native history data is omitted because it can contain authenticated download URLs. Episode file IDs, absolute library file paths, and read-only command receipt tools support subsequent verification.
- Optional `imports.guarded: true`. Preview requires an exact native queue download and one target series/movie. The download must be completed, have zero bytes left, and be blocked on import. Only unrejected, unambiguous files for missing library items are eligible. Sources inside library roots and duplicate mappings are refused. Applying a five-minute server-owned plan rechecks native state and the selected file metadata, fixes the mode to `copy`, and consumes the plan immediately before submission. Caller file paths/mappings are forbidden. Explicit `acceptCleanup: true` is required because native completed-download handling may remove torrents and source files even in copy mode.
- Optional `server.bearerToken`, expanded from an operator environment variable, for `/mcp`. Guarded HTTP refuses to start without it. `/health` contains only liveness status and remains unauthenticated. TLS and network restrictions belong to the deployment.

Defaults preserve upstream behaviour unless these options are enabled. The example below opts in. These changes are a source baseline, not a production cutover.

### Import limits

The guard trusts Sonarr/Radarr's current queue, scanner, and library database. It does **not** independently verify a qBittorrent file manifest, filesystem symlinks, torrent category, competing downloads, or native cleanup configuration. It cannot atomically exclude changes by native jobs or other API clients. These differences must be evaluated before replacing an existing connector that performs those checks.

Plans are in-memory, capped at 256, invalidated by restart, and bound to service and instance. All callers authenticated with the deployment token have the same privileges; plans are not separate user identities. A recent-submission hold prevents another plan from submitting the same download for five minutes. A failed POST is reported as uncertain and its token cannot be replayed. This is not durable exactly-once delivery across restarts or after the hold expires. Inspect native commands, exact-download history, and current library file IDs/paths before trying again. A queued or completed command alone does not prove import success.

## Deployment and updates

Source belongs at `https://github.com/lorenzomoriconi/arr_mcp`. CI tests PRs. Merging a feature updates the release-please release PR; merging that release PR creates a fork-component-prefixed tag. A release runs the reusable CI workflow, scans the amd64 image, then publishes amd64/arm64 images to `ghcr.io/lorenzomoriconi/arr_mcp` with version tags, provenance and SBOM. No image is published merely by opening a feature PR. CI publication has to be enabled and verified on the fork; do not assume inherited workflow files have run.

TrueNAS runs the published image, not a Git checkout. Use an explicit version (or recorded digest) in the Custom App definition. Keep the previous image reference and private app configuration for rollback, update the image reference, and let TrueNAS recreate the container. There is no GitHub-to-home administrative connection or unattended NAS deployment in this setup. The upstream release image build uses upstream module linker paths intentionally.

Keep private API credentials and the MCP token in TrueNAS environment/configuration, outside the repository and image. Use a non-root container, read-only root filesystem, dropped capabilities and no host networking, media datasets, Docker socket or NAS administrator credentials. The deployment should retain private VPN access and its source/Host/path restrictions. Do not deploy the upstream wide-open Compose quickstart unchanged.

The existing Hermes connector stays active until authentication, native MCP delivery/approvals, import verification, and finite download watches have been validated for this fork. Telegram preferences, the media skill and scheduling remain outside the application fork. The previous connector's RPC client and watch helper cannot be pointed at native MCP without adaptation. No media operation is part of the initial source setup.

## Hermes contributing

Hermes can maintain this public repository through a separate development checkout and repository-scoped GitHub access. Use branches and PRs, fake app APIs and local tests. Never put credentials, private deployment dumps, home topology, download history, or actual media paths into commits. Repository write access does not require TrueNAS administrator access. Production service code is updated by replacing the released image, not by editing running containers. Maintenance and deployment are separate capabilities.

Follow `AGENTS.md` for development conventions. For this fork, retain upstream history/module naming; use release-please's fork component tags and the fork's image namespace. Do not publish to upstream's registry or rewrite upstream tags.

## Tracking upstream

Keep `origin` pointing at this fork and `upstream` at GauranshMathur/ARR_MCP. Upstream push is disabled locally. Fetch upstream branches normally and fetch future upstream tags into a namespace, for example `git fetch upstream '+refs/tags/*:refs/tags/upstream/*'`, so fork releases do not collide with them. Review and test upstream updates on a branch before merging them. Small, generally useful changes can be proposed upstream separately; making this fork does not authorize sending upstream PRs.

Run the tests and checks in `AGENTS.md`. A source or fake-API test is not evidence of a live Telegram approval or a real media import. Document validation honestly, including anything not exercised.
