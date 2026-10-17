import unittest
import vm_install
try:
    import yaml
except ImportError:
    yaml=None

@unittest.skipIf(yaml is None,'PyYAML required for operator configuration')
class NativeConfigInstallTests(unittest.TestCase):
    def test_only_arr_configuration_changes(self):
        before='model: example\n# keep comment\nmcp_servers:\n  arr:\n    command: old-client\n  other:\n    url: https://example.invalid/mcp\nauxiliary:\n  reasoning: medium\n'
        after=vm_install.replace_arr_config(before,{'url':'https://private.invalid/rpc'},yaml)
        parsed=yaml.safe_load(after)
        expected=yaml.safe_load(before)
        expected['mcp_servers']['arr']={'url':'https://private.invalid/rpc'}
        self.assertEqual(parsed,expected)
        self.assertIn('# keep comment',after.decode())


import base64
import json
import os
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

@unittest.skipUnless(os.name == 'posix' and hasattr(os, 'geteuid') and os.geteuid() == 0, 'POSIX root fake-filesystem test')
class NativeInstallRollbackTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.user_home = root / 'user'
        self.home = self.user_home / '.hermes'
        self.workspace = self.user_home / 'workspace'
        self.settings = root / 'etc-arr'
        self.helpers = root / 'opt-arr'
        self.backups = root / 'backups'
        self.hosts = root / 'hosts'
        for folder in [self.home / 'skills/media/arr-downloads', self.home / 'cache', self.workspace, self.settings, self.helpers]:
            folder.mkdir(parents=True, exist_ok=True)
        self.settings.chmod(0o751)
        self.hosts.write_text('127.0.0.1 localhost\n')
        (self.settings / 'broker.token').write_text('a' * 64)
        (self.settings / 'broker.token').chmod(0o640)
        self.paths = {
            self.home / 'config.yaml': b'model: unchanged\nmcp_servers:\n  arr:\n    command: old-client\n',
            self.home / '.env': b'EXISTING=keep\n',
            self.home / 'skills/media/arr-downloads/SKILL.md': b'old skill',
            self.workspace / 'AGENTS.md': b'old instructions',
        }
        for path, raw in self.paths.items():path.write_bytes(raw)
        (self.home / 'cache/mcp_schema_cache.json').write_text('{"arr":{"old":true},"other":{}}')
        (self.settings / 'endpoint.json').write_text('{"url":"https://old.invalid/rpc","address":"192.0.2.2"}')
        with sqlite3.connect(self.home / 'state.db') as connection:
            connection.execute('CREATE TABLE session_turn_leases (expires_at REAL)')
        owner = SimpleNamespace(pw_dir=str(self.user_home), pw_uid=0, pw_gid=0)
        self.patchers = [patch.object(vm_install,name,value,create=True) for name,value in [('SETTINGS',self.settings),('HELPERS',self.helpers),('BACKUPS',self.backups),('HOSTS',self.hosts)]]
        self.patchers += [patch('pwd.getpwnam',return_value=owner), patch.object(vm_install.subprocess,'run')]
        for patcher in self.patchers:patcher.start();self.addCleanup(patcher.stop)
        encode=lambda value:base64.b64encode(value).decode()
        self.payload = {'user':'fixture','home':str(self.home),'workspace':str(self.workspace),
            'expected':{str(p):vm_install.sha(v) for p,v in self.paths.items()},
            'server':{'url':'https://new.invalid/rpc','ssl_verify':'/etc/ssl/certs/ca-certificates.crt','protocol':'stateless'},
            'endpoint':{'url':'https://new.invalid/rpc','address':'192.0.2.1'},
            'skill':encode(b'new skill'),'agents':encode(b'new instructions'),
            'watch':encode(b'new watch'),'native_client':encode(b'new client'),'watch_policy':{'chats':[]}}

    def test_install_tracks_endpoint_and_rollback_handles_gateway_cache_rewrite(self):
        before_endpoint = (self.settings / 'endpoint.json').read_bytes()
        backup = vm_install.install(self.payload)
        self.assertEqual(json.loads((self.settings / 'endpoint.json').read_text()),self.payload['endpoint'])
        (self.home / 'cache/mcp_schema_cache.json').write_text('{"arr":{"new":true},"other":{}}')
        vm_install.rollback(backup,start=False)
        for path, raw in self.paths.items():self.assertEqual(path.read_bytes(),raw)
        self.assertEqual((self.settings / 'endpoint.json').read_bytes(),before_endpoint)
        self.assertEqual(self.settings.stat().st_mode & 0o777,0o751)
        self.assertFalse((self.helpers / 'native_client.py').exists())

    def test_rollback_preserves_later_operator_edits(self):
        backup = vm_install.install(self.payload)
        (self.workspace / 'AGENTS.md').write_text('later edit')
        with self.assertRaisesRegex(RuntimeError,'changed after cutover'):vm_install.rollback(backup,start=False)
        self.assertEqual((self.workspace / 'AGENTS.md').read_text(),'later edit')

    def test_configuration_race_after_gateway_stop_is_refused(self):
        def stop_gateway(*args,**kwargs):
            if args[0][1] == 'stop':(self.workspace / 'AGENTS.md').write_text('racing edit')
        vm_install.subprocess.run.side_effect=stop_gateway
        with self.assertRaisesRegex(RuntimeError,'changed'):vm_install.install(self.payload)
        self.assertEqual((self.workspace / 'AGENTS.md').read_text(),'racing edit')


    def test_rollback_removes_cache_created_after_install(self):
        cache=self.home / 'cache/mcp_schema_cache.json'
        cache.unlink()
        backup=vm_install.install(self.payload)
        cache.write_text('{"arr":{"rebuilt":true}}')
        vm_install.rollback(backup,start=False)
        self.assertFalse(cache.exists())
