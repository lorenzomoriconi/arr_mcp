"""Create a restricted TrueNAS Custom App definition from private operator input."""
from __future__ import annotations
import ipaddress
import re


def compose(image, config_text, environment, listen_ip, port, extra_hosts=None):
    if not re.fullmatch(r'ghcr\.io/[a-z0-9_-]+/arr_mcp@sha256:[a-f0-9]{64}', image):
        raise ValueError('A recorded arr_mcp registry digest is required')
    address = ipaddress.ip_address(listen_ip)
    if not address.is_private or address.is_unspecified or address.is_loopback:
        raise ValueError('An exact private LAN listening address is required')
    if not isinstance(port, int) or not 1024 <= port <= 65535:
        raise ValueError('Invalid private listener port')
    def escape(value):
        # Compose interpolation also applies inside config content and env values.
        return value.replace('$', '$$') if isinstance(value, str) else value
    service = {
        'image': image, 'pull_policy': 'always', 'read_only': True,
        'user': '65532:65532', 'cap_drop': ['ALL'],
        'security_opt': ['no-new-privileges:true'],
        'ports': [f'{address}:{port}:8080/tcp'],
        'configs': [{'source':'arr_mcp', 'target':'/etc/arr-mcp/config.yaml', 'mode':0o444}],
        'command': ['--config','/etc/arr-mcp/config.yaml'],
        'environment': {key:escape(value) for key,value in environment.items()},
        'restart': 'unless-stopped', 'mem_limit':'256m', 'cpus':0.5,
        'pids_limit':64, 'stop_grace_period':'20s',
        'labels': {'org.opencontainers.image.source': 'https://github.com/lorenzomoriconi/arr_mcp'},
    }
    if extra_hosts:
        service['extra_hosts'] = dict(extra_hosts)
    return {'services': {'arr-mcp': service}, 'configs': {'arr_mcp': {'content': escape(config_text)}}}
