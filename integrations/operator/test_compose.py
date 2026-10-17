import unittest
from compose import compose

class DeploymentTests(unittest.TestCase):
    def test_digest_nonroot_no_host_mounts_and_literal_runtime_secrets(self):
        result=compose('ghcr.io/example/arr_mcp@sha256:'+'a'*64,'token: ${TOKEN}',{'TOKEN':'literal$value'},'192.0.2.10',18788)
        service=result['services']['arr-mcp']
        self.assertNotIn('network_mode',service)
        self.assertNotIn('volumes',service)
        self.assertEqual(service['user'],'65532:65532')
        self.assertTrue(service['read_only'])
        self.assertEqual(service['cap_drop'],['ALL'])
        self.assertEqual(result['configs']['arr_mcp']['content'],'token: $${TOKEN}')
        self.assertEqual(service['environment']['TOKEN'],'literal$$value')
    def test_unpinned_image_and_wildcard_bind_are_refused(self):
        for image,address in [('ghcr.io/example/arr_mcp:latest','192.0.2.10'),('ghcr.io/example/arr_mcp@sha256:'+'a'*64,'0.0.0.0')]:
            with self.assertRaises(ValueError):compose(image,'',{},address,18788)
