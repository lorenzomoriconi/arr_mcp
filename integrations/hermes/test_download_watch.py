import copy
import unittest
from download_watch import decide, completed, read, collect, resolve_expected, validate_torrent, native_reason


class WatchTests(unittest.TestCase):
    def setUp(self):
        self.config = {"title": "Test show", "app": "sonarr", "item_id": 35,
                       "hash": "a" * 40, "seasons": [9], "expected": [1, 2],
                       "expires_at": 200000, "hours": 48, "last_progress": .1,
                       "last_progress_at": 1000, "milestone": 0, "imported": []}
        self.torrent = {"progress": .1, "state": "downloading", "amount_left": 90,
                        "size": 100, "dlspeed": 1000, "num_seeds": 2, "eta": 500}
        self.snap = {"torrent": self.torrent, "imported": [], "files": []}

    def test_no_change_silent_without_mutating_input(self):
        before = copy.deepcopy(self.config)
        state, message, stop = decide(self.config, self.snap, 2000)
        self.assertEqual((message, stop), ("", None))
        self.assertEqual(self.config, before)

    def test_progress_milestone_only_once(self):
        self.torrent["progress"] = .26
        state, message, stop = decide(self.config, self.snap, 2000)
        self.assertIn("26.0%", message)
        self.assertIsNone(stop)
        self.assertEqual(decide(state, self.snap, 3000)[1:], ("", None))

    def test_download_is_not_import(self):
        self.torrent.update(progress=1, amount_left=0, state="stalledUP")
        state, message, stop = decide(self.config, self.snap, 2000)
        self.assertIn("non è ancora confermata", message)
        self.assertIsNone(stop)
        self.assertEqual(decide(state, self.snap, 3000)[1:], ("", None))
        self.assertEqual(decide(state, self.snap, 9200)[2], "import needs attention")

    def test_import_requires_all_expected_files_and_exact_history(self):
        self.snap.update(imported=[1], files=[1, 2])
        self.assertIsNone(decide(self.config, self.snap, 2000)[2])
        self.snap["imported"] = [1, 2]
        state, message, stop = decide(self.config, self.snap, 2000)
        self.assertEqual(stop, "import confirmed")
        self.assertIn("tutti i 2 episodi", message)
        self.snap["files"] = [1]
        self.assertIsNone(decide(self.config, self.snap, 2000)[2])

    def test_stall_stops_after_two_hours(self):
        self.assertIsNone(decide(self.config, self.snap, 8199)[2])
        self.assertEqual(decide(self.config, self.snap, 8200)[2], "download stalled")

    def test_progress_resets_stall_timer(self):
        self.torrent["progress"] = .1002
        state, _, stop = decide(self.config, self.snap, 10000)
        self.assertIsNone(stop)
        self.assertEqual(state["last_progress_at"], 10000)

    def test_checking_and_metadata_not_download_completion(self):
        self.torrent.update(progress=1, amount_left=0, state="checkingUP")
        self.assertFalse(completed(self.torrent))
        self.assertEqual(decide(self.config, self.snap, 10000)[1:], ("", None))
        self.torrent.update(progress=0, amount_left=0, size=0, state="metaDL")
        self.assertFalse(completed(self.torrent))

    def test_pause_and_error_need_attention_without_repairs(self):
        for status, reason in [("error", "download error"), ("missingFiles", "download error"),
                               ("pausedDL", "download stopped"), ("stoppedDL", "download stopped")]:
            with self.subTest(status=status):
                self.torrent["state"] = status
                self.assertEqual(decide(self.config, self.snap, 2000)[2], reason)

    def test_connection_outage_debounced(self):
        state, message, stop = decide(self.config, None, 2000)
        self.assertEqual((message, stop), ("", None))
        self.assertIsNone(decide(state, None, 5599)[2])
        self.assertEqual(decide(state, None, 5600)[2], "connection unavailable")
        recovered = decide(state, self.snap, 3000)[0]
        self.assertNotIn("outage_since", recovered)

    def test_missing_torrent_never_implies_success(self):
        self.snap["torrent"] = None
        state, message, stop = decide(self.config, self.snap, 2000)
        self.assertEqual((message, stop), ("", None))
        self.assertEqual(decide(state, self.snap, 9200)[2], "not visible")
        self.snap.update(imported=[1, 2], files=[1, 2])
        self.assertEqual(decide(state, self.snap, 3000)[2], "import confirmed")

    def test_expiry_is_hard_bound_and_finished_does_not_renotify(self):
        self.assertEqual(decide(self.config, None, 200000)[2], "expired")
        self.config["finished"] = "expired"
        self.assertEqual(decide(self.config, None, 200100)[1:], ("", "expired"))

    def test_write_operation_rejected_before_rpc(self):
        calls = []
        with self.assertRaises(ValueError):
            read(lambda *args: calls.append(args), "download_action", action="resume")
        self.assertEqual(calls, [])

    def test_collector_excludes_other_hash_and_non_import_events(self):
        calls = []
        def rpc(op, args):
            calls.append(op)
            return {
                "downloads": {"downloads": [{**self.torrent, "hash": "a" * 40}]},
                "history": {"records": [
                    {"downloadId": "b" * 40, "eventType": "downloadFolderImported", "episodeId": 1},
                    {"downloadId": "a" * 40, "eventType": "grabbed", "episodeId": 2},
                    {"downloadId": "A" * 40, "eventType": "downloadFolderImported", "episodeId": 2, "data": {"fileId": "12", "importedPath": "/library/b.mkv"}}]},
                "episodes": {"total": 2, "episodes": [{"id": 1, "hasFile": True, "episodeFileId": 11}, {"id": 2, "hasFile": True, "episodeFileId": 12}]},
                "files": {"files": [{"id": 11, "path": "/library/a.mkv"}, {"id": 12, "path": "/library/b.mkv"}]}
            }[op]
        snap = collect(self.config, rpc)
        self.assertEqual(snap["imported"], [2])
        self.assertIsNone(decide(self.config, snap, 2000)[2])
        self.assertTrue(set(calls) <= {"downloads", "history", "episodes", "files"})

    def test_history_is_accumulated_across_checks(self):
        self.config["imported"] = [1]
        self.config["evidence"] = {"1": {"file_id": 11, "path": "/library/a.mkv"}}
        def rpc(op, args):
            return {"downloads": {"downloads": []},
                    "history": {"records": [{"downloadId": "A" * 40, "eventType": "downloadFolderImported", "episodeId": 2, "data": {"fileId": "12", "importedPath": "/library/b.mkv"}}]},
                    "episodes": {"total": 2, "episodes": [{"id": 1, "hasFile": True, "episodeFileId": 11}, {"id": 2, "hasFile": True, "episodeFileId": 12}]},
                "files": {"files": [{"id": 11, "path": "/library/a.mkv"}, {"id": 12, "path": "/library/b.mkv"}]}}[op]
        self.assertEqual(decide(self.config, collect(self.config, rpc), 2000)[2], "import confirmed")

    def test_radarr_requires_matching_movie_import(self):
        self.config.update(app="radarr", expected=[35])
        self.snap.update(imported=[35], files=[35])
        self.assertEqual(decide(self.config, self.snap, 2000)[2], "import confirmed")


    def test_queued_and_moving_wait_without_false_stall(self):
        for status in ["queuedDL", "moving", "allocating"]:
            self.torrent["state"] = status
            state, message, stop = decide(self.config, self.snap, 10000)
            self.assertEqual((message, stop), ("", None))
            self.assertEqual(state["last_progress_at"], 10000)

    def test_invalid_transfer_values_rejected_during_collection(self):
        for key in ["progress", "state", "size", "dlspeed", "eta", "amount_left"]:
            with self.subTest(key=key):
                torrent = {**self.torrent, key: None}
                with self.assertRaises(ValueError):
                    validate_torrent(torrent)

    def test_explicit_import_block_reports_reason_immediately(self):
        self.torrent.update(progress=1, amount_left=0, state="stoppedUP")
        self.snap["import_blocked"] = ["Multi-season download, unable to import automatically"]
        state, message, stop = decide(self.config, self.snap, 2000)
        self.assertEqual(stop, "import blocked")
        self.assertIn("Multi-season", message)
        self.snap.update(imported=[1, 2], files=[1, 2])
        self.assertEqual(decide(self.config, self.snap, 2000)[2], "import confirmed")

    def test_import_block_reads_paginated_exact_hash_and_title_only(self):
        self.torrent.update(progress=1, amount_left=0, state="stoppedUP")
        pages = []
        def rpc(op, args):
            if op == "downloads": return {"downloads": [{**self.torrent, "hash": "a" * 40}]}
            if op == "history": return {"records": []}
            self.assertEqual(op, "queue"); pages.append(args["page"])
            row = {"downloadId": "a" * 40, "seriesId": 35, "trackedDownloadState": "importBlocked",
                   "statusMessages": [{"messages": ["Multi-season\nblocked https://private.invalid/token"]}]}
            if args["page"] == 1:
                return {"total": 101, "records": [{**row, "downloadId": "b" * 40}, {**row, "seriesId": 99}]}
            return {"total": 101, "records": [row, row]}
        snap = collect(self.config, rpc)
        self.assertEqual(pages, [1, 2])
        self.assertEqual(snap["import_blocked"], ["Multi-season blocked [URL]"])

    def test_external_reason_sanitization_and_extra_count(self):
        self.assertEqual(native_reason("<b>*blocked*</b>\x00 magnet:?xt=secret"), "blocked [URL]")
        self.torrent.update(progress=1, amount_left=0, state="stoppedUP")
        self.snap.update(import_blocked=["Reason"], import_blocked_extra=4)
        self.assertIn("+4 altri motivi", decide(self.config, self.snap, 2000)[1])

    def test_native_mapping_uses_actual_partial_pack_scope(self):
        torrent = {"hash": "a" * 40, "name": "Test.S09E01"}
        def rpc(op, args):
            return {"magnet_preview": {"item": {"id": 35}, "hash": "a" * 40,
                                      "mapped_episodes": [{"id": 1}], "mapped_episodes_truncated": False},
                    "episodes": {"total": 2, "episodes": [{"id": 1}, {"id": 2}]}}[op]
        self.assertEqual(resolve_expected(rpc, "sonarr", 35, torrent, [9]), [1])

    def test_mapping_wrong_title_or_scope_rejected(self):
        for item_id, episode_id in [(36, 1), (35, 99)]:
            with self.subTest(item_id=item_id, episode_id=episode_id):
                def rpc(op, args):
                    return {"magnet_preview": {"item": {"id": item_id}, "hash": "a" * 40,
                                              "mapped_episodes": [{"id": episode_id}]},
                            "episodes": {"total": 1, "episodes": [{"id": 1}]}}[op]
                with self.assertRaises(ValueError):
                    resolve_expected(rpc, "sonarr", 35, {"hash": "a" * 40, "name": "Test"}, [9])

    def test_queue_fallback_requires_exact_hash_and_native_item(self):
        for series_id in [35, 36]:
            with self.subTest(series_id=series_id):
                def rpc(op, args):
                    if op == "magnet_preview":
                        raise RuntimeError("Public magnet preview not supported")
                    return {"queue": {"total": 2, "records": [
                        {"downloadId": "b" * 40, "seriesId": 35, "episodeId": 2},
                        {"downloadId": "A" * 40, "seriesId": series_id, "episodeId": 1}]},
                        "episodes": {"total": 2, "episodes": [{"id": 1}, {"id": 2}]}}[op]
                if series_id == 35:
                    self.assertEqual(resolve_expected(rpc, "sonarr", 35, {"hash": "a" * 40, "name": "Test"}, [9]), [1])
                else:
                    with self.assertRaises(ValueError):
                        resolve_expected(rpc, "sonarr", 35, {"hash": "a" * 40, "name": "Test"}, [9])


if __name__ == "__main__":
    unittest.main()
