---
name: arr-downloads
description: Request films and series, choose releases and presets, diagnose downloads, safely handle public magnets and blocked imports, and create requested finite progress watches through native ARR MCP.
---

# Media requests and downloads

Use the `arr` native MCP connection (`mcp-arr`, tools named `mcp__arr__sonarr_*`, `radarr_*`, `qbittorrent_*`, and selected Prowlarr health tools). Discover current tools through Hermes tool search/describe. The previous 22-operation RPC client is retired; older conversation claims about unavailable magnets, imports or scheduling are obsolete. Do not use a terminal, browser, direct API key or NAS shell to bypass connector policy. The terminal is appropriate for the installed finite watch helper below.

## Requests and releases

1. Treat English as the language preference unless the current user specifies another language. Clarify the preset/quality for a new title when unspecified, one concise question at a time; retain existing title settings. A Sonarr profile named Any does not guarantee English. Inspect `*_get_quality_profile` and relevant custom formats when language matters, and distinguish release-label claims from actual audio verification.
2. Find the exact title/year using `sonarr_search_series` or `radarr_search_movies`; check the existing library before adding. Select a profile and an existing `*_list_root_folders` path. Guarded deployments refuse arbitrary roots. Add the chosen title without tags or searchNow, with the agreed monitoring scope; then launch only the requested series/season/episodes or movie search. Existing library additions are not authorization to search every title.
3. For a scarce or specific release, run `*_list_releases` for the requested target and scope, inspect quality/language/rejections/seed evidence, then use its exact guid/indexerId in `*_grab_release`. A grab overrides native release rejections: explain material exceptions before applying. Prefer a bounded search (30-50 results); narrow the scope instead of repeatedly increasing limits. An indexer search may take time. Do one diagnostic call before trying a corrected argument; repeated failures can trip Hermes's connector cooldown.
4. Act only on the user’s explicit request and agreed scope. This installation delegates the bounded allowlisted actions directly; it does not display native approval buttons. The token is available to the service user, so confirmations would not isolate the terminal. Never bypass the connector or blindly replay an uncertain action. Inspect current queue/history/library state first.

## Public magnets

Use `sonarr_magnet_preview` or `radarr_magnet_preview` with an existing itemId and the user's original v1 public/DHT magnet. Supply releaseTitle only when the magnet lacks its original dn label. Do not invent a label to force a title match. The server verifies the app's native download client/category and parsed episode/movie scope, strips tracker hints, and refuses source/web-seed/peer hints, external torrent URLs and caller-selected save paths. Private torrents needing tracker credentials are outside this route.

Show the preview's selected title, mapped episode scope, parsed quality/language, existing library count and overlapping downloads. Actual files/audio/seed availability remain unverified; manual selection bypasses automatic release choices. Existing files or competing downloads can lead native import to replace/recycle files. Apply a fresh five-minute plan with `*_add_magnet`, acceptUnverified=true, and acceptReplacementRisk=true only when the user agrees to that disclosed risk. Reuse clear agreement already present; ask only about new ambiguity or material consequences. An existing matching hash is left untouched. A spent, stale or expired plan needs a new preview. An uncertain result needs state inspection before another add. Do not copy raw tracker/passkey hints into replies or notes.

## Download diagnosis and controls

Start with `*_system_status`, `*_health`, `*_queue`, `qbittorrent_list_torrents`, file lists and target history. Queue tools return page/pageSize/total and downloadId; paginate before claiming a scan is complete. Correlate exact downloadId with the torrent hash. A season pack can produce many episode rows, so queue rows are not torrent counts. Distinguish stopped, stalled/no peers, error, completed-but-not-imported and actual import success.

On an explicit action request, stop/start/recheck/reannounce tools accept one exact hash in the native media apps’ categories in guarded deployments. Diagnosis alone does not authorize controls. No deletion, bulk selectors, arbitrary save paths, category changes, media moves, indexer/client edits, filesystem repairs or service administration are enabled.

## Preset changes

Read the current full profile and explain proposed changes and future upgrade/replacement effects. With agreement, use `*_create_quality_profile` to create a separate profile named `Hermes - ...` and `*_update_quality_profile` to configure it using current native names while it remains unassigned. Names must remain in that reserved namespace; existing operator presets cannot be claimed by renaming them. The server refuses changes to any profile already assigned to a library title; shared presets cannot be changed through this connection. Guarded `sonarr_edit_series` / `radarr_edit_movies` only assign one selected quality profile to one existing title; their other generic edit fields are refused. Assignment or profile edits do not independently authorize a search.

## Explicit blocked imports

Only an explicit request to finish/import authorizes `*_manual_import_preview` and `*_manual_import`. Enabling or diagnosing this capability does not request an actual import. Preview one completed native importBlocked downloadId with its exact seriesId/movieId. The production configuration binds every selected source to qBittorrent's completed wanted-file manifest and native category. Existing library files, rejected/ambiguous mappings and duplicate units are refused. Native quality revision/repack/indexer metadata is retained.

Explain cleanup warnings: even Copy mode can be followed by native completed-download removal of torrents/source files or category relocation, including skipped extras. Set acceptCleanup=true only with agreement. If requiresOverlapAcceptance is true, disclose the competing downloads and set acceptOverlap=true only with agreement. Use the server-owned five-minute plan; never supply paths, mappings or move/auto mode. The server revalidates evidence before submission, but cannot atomically exclude changes by independent native jobs/clients or inspect filesystem symlinks without a media mount.

A command receipt is queued work, not successful import. Verify every selected episode/movie through new exact-download import history, its fileId/importedPath and the current library file ID/path. Report partial counts, uncertain submission and native errors plainly. A completed torrent, completed command, existing hasFile flag or empty preview alone is insufficient. Do not force imports from a watch or automatically retry uncertain writes.

## Requested progress updates

User-requested bounded read-only watches and general reminders are permitted; the old morning-briefing-only pilot restriction is obsolete. A capability/setup question is not a request to monitor anything. Use native scheduling for requested reminders with an exact chat/topic delivery target. Scheduled agents must not create more schedules.

For a requested supported download watch, use Hermes's configured Python and the installed helper:

    __HERMES_PYTHON__ /opt/hermes-arr/download_watch.py create --app sonarr --item-id SERIES_ID --hash EXACT_HASH --title 'EXISTING LIBRARY TITLE' --seasons SEASON_NUMBERS --deliver telegram:CHAT_ID:THREAD_ID --hours 48

For Radarr, select radarr and omit seasons. Resolve the hash, title and requested release scope first. Use the actual requesting chat/topic; an operator-owned policy bounds authorized chats. Safely quote shell arguments. No raw magnet/passkey is needed. The helper deduplicates active watches and returns the native job ID. Manage that ID with native cron tools; do not add OS crontabs, daemons or another bot.

The no_agent helper polls every 30 minutes, sends 25% milestones and verified import/attention results, and uses no model tokens. It ends after full import, a native import block, stopped/error state, two hours without progress/import confirmation, one hour of connector outage, or the finite expiry (48 hours by default, maximum 168). Current file IDs/paths must match accumulated exact-hash import evidence. It never controls downloads, changes presets, triggers searches, imports files or creates another schedule during a run. No change means no message. Failed delivery remains visible in cron history; do not blindly create replacement watches.
