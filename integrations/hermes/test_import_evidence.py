import unittest
from download_watch import collect

class ImportEvidenceTests(unittest.TestCase):
    def test_old_or_different_file_is_not_import_success(self):
        config={"hash":"a"*40,"app":"sonarr","item_id":1,"expected":[2],"seasons":[1],"title":"Example","imported":[]}
        def rpc(operation,args):
            if operation=="downloads":return {"downloads":[]}
            if operation=="history":return {"records":[{"eventType":"downloadFolderImported","downloadId":"a"*40,"episodeId":2,"data":{"fileId":"8","importedPath":"/library/old.mkv"}}]}
            if operation=="episodes":return {"total":1,"episodes":[{"id":2,"hasFile":True,"episodeFileId":9}]}
            if operation=="files":return {"files":[{"id":9,"absolutePath":"/library/new.mkv"}]}
            raise AssertionError(operation)
        result=collect(config,rpc)
        self.assertFalse(set(config["expected"]) <= set(result["imported"]) & set(result["files"]))
