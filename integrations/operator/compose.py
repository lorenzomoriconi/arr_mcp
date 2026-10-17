"""Create a restricted TrueNAS Custom App definition from private operator input."""
from __future__ import annotations
import ipaddress
import re
from pathlib import PurePosixPath


def compose(image, config_file, environment, listen_ip, port, extra_hosts=None):
    if not re.fullmatch(r'ghcr\.io/[a-z0-9_-]+/arr_mcp@sha256:[a-f0-9]{64}', image):
        raise ValueError('A recorded arr_mcp registry digest is required')
    address = ipaddress.ip_address(listen_ip)
    if not address.is_private or address.is_unspecified or address.is_loopback:
        raise ValueError('An exact private LAN listening address is required')
    if not isinstance(port, int) or not 1024 <= port <= 65535:
        raise ValueError('Invalid private listener port')
    policy = PurePosixPath(config_file)
    if len(policy.parts) < 5 or not policy.is_absolute() or not config_file.startswith('/mnt/') or str(policy) != config_file or '..' in policy.parts or policy.name != 'config.yaml' or any(char in config_file for char in '\\$') or any(ord(char) < 32 or ord(char) == 127 for char in config_file):
        raise ValueError('An exact persistent /mnt/ operator config.yaml file is required')
    def escape(value):
        # Compose interpolation also applies to literal environment values.
        return value.replace('$', '$$') if isinstance(value, str) else value
    service = {
        'image': image, 'pull_policy': 'always', 'read_only': True,
        'user': '65532:65532', 'cap_drop': ['ALL'],
        'security_opt': ['no-new-privileges:true'],
        'ports': [f'{address}:{port}:8080/tcp'],
        # File-backed configs inherit host ownership/permissions; Compose
        # ignores config mode/uid/gid. Require root:65532 0440, no extended ACL.
        'configs': [{'source':'arr_mcp', 'target':'/etc/arr-mcp/config.yaml'}],
        'command': ['--config','/etc/arr-mcp/config.yaml'],
        'environment': {key:escape(value) for key,value in environment.items()},
        'restart': 'unless-stopped', 'mem_limit':'256m', 'cpus':0.5,
        'pids_limit':64, 'stop_grace_period':'20s',
        'labels': {'org.opencontainers.image.source': 'https://github.com/lorenzomoriconi/arr_mcp'},
    }
    if extra_hosts:
        service['extra_hosts'] = dict(extra_hosts)
    return {'services': {'arr-mcp': service}, 'configs': {'arr_mcp': {'file': config_file}}}
