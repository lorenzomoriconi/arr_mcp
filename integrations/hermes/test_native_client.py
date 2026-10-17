import unittest
from unittest.mock import patch
import native_client

class NativeReadAdapterTests(unittest.TestCase):
    def test_watcher_requests_exact_hash_and_normalizes_transfer_evidence(self):
        with patch.object(native_client, 'call', return_value={'torrents': [{'hash':'a'*40,'amountLeft':0,'downloadSpeed':5,'seeds':3}]}) as call:
            result=native_client.rpc('downloads',{'hashes':['a'*40],'limit':1})
        call.assert_called_once_with('qbittorrent_list_torrents',{'hashes':['a'*40],'limit':1})
        self.assertEqual(result['downloads'][0]['amount_left'],0)
        self.assertEqual(result['downloads'][0]['dlspeed'],5)
    def test_media_writes_never_reach_native_sdk(self):
        with patch.object(native_client, '_call') as call:
            with self.assertRaises(ValueError):native_client.call('sonarr_manual_import',{})
            with self.assertRaises(ValueError):native_client.rpc('add_magnet',{})
        call.assert_not_called()
    def test_native_queue_total_and_exact_hash_evidence_survive_adapter(self):
        with patch.object(native_client,'call',return_value={'items':[{'downloadId':'HASH'}],'total':519}) as call:
            result=native_client.rpc('queue',{'app':'sonarr','page':2,'page_size':100})
        self.assertEqual(result,{'records':[{'downloadId':'HASH'}],'total':519})
        call.assert_called_once_with('sonarr_queue',{'page':2,'pageSize':100})
