# Radarr tools

| Area | Tools | Access |
|---|---|---|
| Library | `radarr_list_movies`, `radarr_get_movie`, `radarr_search_movies`, `radarr_list_collections`, `radarr_calendar` | read |
| Wanted | `radarr_wanted_missing`, `radarr_wanted_cutoff` | read |
| Search & import | `radarr_list_releases`, `radarr_manual_import_preview` | read |
| Files | `radarr_list_movie_files`, `radarr_rename_preview` | read |
| Profiles | `radarr_list_quality_profiles`, `radarr_get_quality_profile`, `radarr_list_quality_definitions`, `radarr_list_custom_formats`, `radarr_get_custom_format`, `radarr_list_delay_profiles`, `radarr_list_release_profiles` | read |
| Config | `radarr_list_root_folders`, `radarr_naming_config`, `radarr_media_management_config`, `radarr_list_indexers`, `radarr_list_download_clients`, `radarr_list_import_lists`, `radarr_list_notifications` | read |
| Providers | `radarr_provider_schemas`, `radarr_get_provider` | read |
| Tags | `radarr_list_tags`, `radarr_tag_details` | read |
| Operations | `radarr_queue`, `radarr_queue_status`, `radarr_history`, `radarr_blocklist`, `radarr_health`, `radarr_disk_space`, `radarr_system_status`, `radarr_list_tasks`, `radarr_list_updates` | read |
| Add & edit | `radarr_add_movie`, `radarr_edit_movies`, `radarr_update_collection`, `radarr_create_tag`, `radarr_update_tag` | write |
| Search & import | `radarr_grab_release`, `radarr_grab_queue_item`, `radarr_manual_import`, `radarr_mark_history_failed` | write |
| Files | `radarr_rename_files`, `radarr_update_files` | write |
| Providers | `radarr_add_provider`, `radarr_update_provider`, `radarr_test_provider` | write |
| Automation | `radarr_trigger_search`, `radarr_refresh_movies`, `radarr_run_command` | write |
| Profiles | `radarr_create_quality_profile`, `radarr_update_quality_profile`, `radarr_create_custom_format`, `radarr_update_custom_format`, `radarr_update_delay_profile` | write |
| Config | `radarr_add_root_folder`, `radarr_update_naming_config`, `radarr_update_media_management_config` | write |
| Deletion | `radarr_delete_movie`, `radarr_delete_movie_files`, `radarr_delete_queue_item`, `radarr_delete_queue_items`, `radarr_delete_blocklist_item`, `radarr_delete_tag`, `radarr_delete_quality_profile`, `radarr_delete_custom_format`, `radarr_delete_root_folder`, `radarr_delete_provider` | destructive |

## Fork command receipts

`radarr_list_commands` lists native background command receipts; `radarr_get_command` reads one by its positive `id`. Both are read-only and return command id, name, status, queued/started/ended times, result and message. A completed command is not proof of an import: verify exact-download history and current library files. See [fork configuration](../../FORK.md) for guarded imports and history filters.

## Guarded public magnets and import evidence

`radarr_magnet_preview` takes an existing itemId and one original public/DHT v1 magnet. It binds native parsing, current files/profile/overlaps and the configured qBittorrent endpoint/category into a five-minute plan. Tracker hints are omitted; source/peer/web-seed hints and arbitrary torrent URLs/paths are refused. `radarr_add_magnet` takes that planId plus acceptUnverified and, when required, acceptReplacementRisk. It never resumes or recategorizes an existing hash. Uncertain submissions consume the plan; inspect exact-hash state before retrying.

With downloads.client configured, guarded manual imports also require exact completed wanted qBittorrent file paths/sizes and native categories, preserving revision/repack/indexer/release metadata. Preview reports competing hashes and requires acceptOverlap when necessary, in addition to native cleanup acceptance. All evidence is rechecked before fixed Copy-mode submission. Queue tools now include download/media IDs, blocked-import reasons and native page totals. See [native integration](../../integrations/README.md) for limitations.
