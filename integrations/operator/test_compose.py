import unittest
from compose import compose

class DeploymentTests(unittest.TestCase):
    def test_digest_nonroot_config_only_mount_and_literal_runtime_secrets(self):
        result=compose('ghcr.io/example/arr_mcp@sha256:'+'a'*64,'/mnt/apps/arr_mcp/config.yaml',{'TOKEN':'literal$value'},'192.0.2.10',18788)
        service=result['services']['arr-mcp']
        self.assertNotIn('network_mode',service)
        self.assertNotIn('volumes',service)
        self.assertEqual(service['user'],'65532:65532')
        self.assertTrue(service['read_only'])
        self.assertEqual(service['cap_drop'],['ALL'])
        self.assertEqual(result['configs']['arr_mcp'],{'file':'/mnt/apps/arr_mcp/config.yaml'})
        self.assertEqual(service['environment']['TOKEN'],'literal$$value')
    def test_unpinned_image_and_wildcard_bind_are_refused(self):
        for image,address in [('ghcr.io/example/arr_mcp:latest','192.0.2.10'),('ghcr.io/example/arr_mcp@sha256:'+'a'*64,'0.0.0.0')]:
            with self.assertRaises(ValueError):compose(image,'/mnt/apps/arr_mcp/config.yaml',{},address,18788)


    def test_unsafe_or_nonpersistent_config_paths_are_refused(self):
        for config_file in ['token: ${TOKEN}','relative/config.yaml','/var/run/docker.sock','/mnt/apps/../config.yaml','/mnt/apps/private.key','/mnt/apps/$HOME/config.yaml','/mnt/config.yaml','/mnt/pool/config.yaml','/mnt/apps/bad\tname/config.yaml','/mnt/apps/bad\x01name/config.yaml','/mnt/apps/bad\x7fname/config.yaml']:
            with self.assertRaises(ValueError):compose('ghcr.io/example/arr_mcp@sha256:'+'a'*64,config_file,{},'192.0.2.10',18788)
