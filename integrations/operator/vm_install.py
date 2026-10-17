"""Operator-only Hermes native-MCP installation and immediate rollback.

Run as root with a reviewed private JSON payload through stdin. No NAS/API
administrator credentials enter the VM. Installation leaves the gateway stopped
until the operator verifies the backend and explicitly starts it.
"""
from __future__ import annotations

import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import sqlite3
import stat
import subprocess
import time

SETTINGS = Path('/etc/hermes-arr')
HELPERS = Path('/opt/hermes-arr')
BACKUPS = Path('/var/backups/hermes-arr')
SERVICE = 'hermes-gateway'
HOSTS = Path('/etc/hosts')


def parent_fd(path):
    directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in Path(path).parts[1:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
            os.close(directory)
            directory = child
        return directory
    except BaseException:
        os.close(directory)
        raise


def read_regular(path, limit=4 * 1024 * 1024):
    directory = parent_fd(path)
    try:
        fd = os.open(Path(path).name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_size > limit:
                raise RuntimeError('Refusing a non-regular or oversized installation file')
            with os.fdopen(fd, 'rb', closefd=False) as stream:
                content = stream.read(limit + 1)
            if len(content) > limit:
                raise RuntimeError('Installation file grew beyond its bound')
            return content, info
        finally:
            os.close(fd)
    finally:
        os.close(directory)


def write_atomic(path, data, mode, uid, gid):
    path = Path(path)
    directory = parent_fd(path)
    temporary = path.name + '.' + secrets.token_hex(8) + '.tmp'
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode, dir_fd=directory)
        try:
            os.fchown(fd, uid, gid)
            with os.fdopen(fd, 'wb', closefd=False) as stream:
                stream.write(data)
                stream.flush()
                os.fsync(fd)
        finally:
            os.close(fd)
        os.replace(temporary, path.name, src_dir_fd=directory, dst_dir_fd=directory)
    finally:
        os.close(directory)


def trusted_folder(path, mode, gid=0):
    path.mkdir(exist_ok=True, mode=mode)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise RuntimeError('Untrusted operator installation directory')
    os.chmod(path, mode)
    os.chown(path, 0, gid)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def assert_idle(home):
    database = home / 'state.db'
    # A conversation database is not a bounded configuration document. Check
    # its type without reading its history or applying config-file size limits.
    directory = parent_fd(database)
    try:
        descriptor = os.open(database.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        try:
            if not stat.S_ISREG(os.fstat(descriptor).st_mode):
                raise RuntimeError('Refusing a non-regular session database')
        finally:
            os.close(descriptor)
    finally:
        os.close(directory)
    with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True) as connection:
        active = connection.execute('SELECT count(*) FROM session_turn_leases WHERE expires_at > ?', (time.time(),)).fetchone()[0]
    if active:
        raise RuntimeError('Hermes has an active turn; defer the cutover')


def replace_arr_config(text, server, yaml):
    before = yaml.safe_load(text)
    after = dict(before)
    after['mcp_servers'] = dict(before.get('mcp_servers') or {})
    after['mcp_servers']['arr'] = server
    replacement = yaml.safe_dump({'mcp_servers': after['mcp_servers']}, sort_keys=False, allow_unicode=True)
    lines = text.splitlines(keepends=True)
    starts = [i for i, line in enumerate(lines) if re.match(r'^mcp_servers:\s*(?:#.*)?$', line.rstrip('\r\n'))]
    if len(starts) != 1:
        raise RuntimeError('Expected one existing top-level mcp_servers block')
    start = starts[0]
    end = start + 1
    while end < len(lines) and (not lines[end].strip() or lines[end][0].isspace() or lines[end].startswith('#')):
        end += 1
    result = ''.join(lines[:start]) + replacement + ''.join(lines[end:])
    if yaml.safe_load(result) != after:
        raise RuntimeError('Config replacement would change unrelated settings')
    return result.encode()


def install(payload):
    import yaml
    import pwd
    if os.geteuid() != 0:
        raise RuntimeError('Operator installation requires root')
    user = payload['user']
    if not re.fullmatch(r'[a-z_][a-z0-9_-]*', user):
        raise ValueError('Invalid service user')
    owner = pwd.getpwnam(user)
    home = Path(payload['home'])
    workspace = Path(payload['workspace'])
    if home != Path(owner.pw_dir) / '.hermes' or workspace != Path(owner.pw_dir) / 'workspace':
        raise ValueError('Installation paths do not match the service user')
    config = home / 'config.yaml'
    skill = home / 'skills/media/arr-downloads/SKILL.md'
    agents = workspace / 'AGENTS.md'
    env = home / '.env'
    cache = home / 'cache/mcp_schema_cache.json'
    originals = {}
    for path in [config, skill, agents, env]:
        raw, info = read_regular(path)
        if sha(raw) != payload['expected'][str(path)]:
            raise RuntimeError('Live configuration changed; merge the change before installing')
        originals[path] = (raw, info)
    server = payload['server']
    if 'command' in server or server.get('ssl_verify') != '/etc/ssl/certs/ca-certificates.crt' or server.get('protocol') != 'stateless':
        raise ValueError('Expected native verified stateless HTTPS MCP')
    if server.get('url') != payload['endpoint']['url'] or not server['url'].startswith('https://'):
        raise ValueError('Native MCP endpoint mismatch')
    chats = payload['watch_policy'].get('authorized_chats')
    if not isinstance(chats,list) or not chats or any(not isinstance(chat,str) or not re.fullmatch(r'-?[0-9]+',chat) for chat in chats):
        raise ValueError('Invalid watch delivery policy')
    skill_content = base64.b64decode(payload['skill'],validate=True)
    if not skill_content.strip() or b'__HERMES_PYTHON__' in skill_content:
        raise ValueError('Unrendered skill interpreter placeholder')
    token, token_info = read_regular(SETTINGS / 'broker.token', 512)
    value = token.decode().strip()
    if token_info.st_uid != 0 or token_info.st_gid != owner.pw_gid or stat.S_IMODE(token_info.st_mode) != 0o640 or not re.fullmatch(r'[A-Za-z0-9_-]{32,}', value):
        raise RuntimeError('Existing scoped token or permissions are invalid')
    env_text = originals[env][0].decode()
    if re.search(r'^ARR_MCP_HTTP_TOKEN\s*=', env_text, flags=re.M):
        raise RuntimeError('Native MCP environment key already exists; inspect before replacing')
    changes = {
        config: replace_arr_config(originals[config][0].decode(), server, yaml),
        env: (env_text.rstrip() + '\nARR_MCP_HTTP_TOKEN=' + value + '\n').encode(),
        skill: skill_content,
        agents: base64.b64decode(payload['agents']),
        HELPERS / 'download_watch.py': base64.b64decode(payload['watch']),
        HELPERS / 'native_client.py': base64.b64decode(payload['native_client']),
        SETTINGS / 'watch-policy.json': (json.dumps(payload['watch_policy']) + '\n').encode(),
        SETTINGS / 'endpoint.json': (json.dumps(payload['endpoint']) + '\n').encode(),
    }
    # Pin this dedicated hostname through root-owned OS resolution. Both Hermes
    # native HTTP and the watch SDK retain normal hostname/CA verification.
    from urllib.parse import urlsplit
    import ipaddress
    hostname = urlsplit(server['url']).hostname
    address = str(ipaddress.ip_address(payload['endpoint']['address']))
    hosts, hosts_info = read_regular(HOSTS)
    if any(hostname in line.split('#', 1)[0].split()[1:] for line in hosts.decode().splitlines()):
        raise RuntimeError('Dedicated MCP hostname already has a hosts entry; inspect it first')
    changes[HOSTS] = hosts.rstrip() + ('\n' + address + ' ' + hostname + ' # arr-mcp-private\n').encode()
    originals[HOSTS] = (hosts, hosts_info)
    try:
        raw, info = read_regular(cache)
    except FileNotFoundError:
        data = {}
    else:
        data = json.loads(raw)
        originals[cache] = (raw, info)
    data.pop('arr', None)
    changes[cache] = (json.dumps(data) + '\n').encode()
    assert_idle(home)
    trusted_folder(BACKUPS, 0o700)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    backup = BACKUPS / ('native-' + stamp + '-' + secrets.token_hex(3))
    backup.mkdir(mode=0o700)
    manifest = {'home':str(home), 'user':user, 'files':[], 'directories':[]}
    for directory in [SETTINGS, HELPERS]:
        if directory.exists():
            info = directory.lstat()
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
                raise RuntimeError('Untrusted operator installation directory')
            manifest['directories'].append({'path':str(directory),'mode':stat.S_IMODE(info.st_mode),'uid':info.st_uid,'gid':info.st_gid})
    for index, (path, data) in enumerate(changes.items()):
        old = originals.get(path)
        if old is None:
            try:old = read_regular(path)
            except FileNotFoundError:pass
        item = {'path':str(path), 'before':None, 'after_sha':sha(data), 'disposable':path == cache}
        if old is not None:
            raw, info = old
            name = str(index) + '.before'
            write_atomic(backup / name, raw, 0o600, 0, 0)
            item.update(before=name, mode=stat.S_IMODE(info.st_mode), uid=info.st_uid, gid=info.st_gid)
        manifest['files'].append(item)
    write_atomic(backup / 'manifest.json', json.dumps(manifest).encode(), 0o600, 0, 0)
    subprocess.run(['systemctl','stop',SERVICE], check=True, capture_output=True)
    # The gateway can finish writing between the idle check and stop. Recheck
    # the snapshot now; refuse concurrent edits before touching any target.
    for path, (raw, _) in originals.items():
        if path == cache:
            continue
        current, _ = read_regular(path)
        if sha(current) != sha(raw):
            raise RuntimeError('Live configuration changed after gateway stop; inspect the backup before continuing')
    # Cache contents may change during shutdown. Preserve that latest snapshot
    # and invalidate only arr, rather than dropping unrelated late discoveries.
    index, item = next((i,item) for i,item in enumerate(manifest['files']) if item['path'] == str(cache))
    try:
        latest, info = read_regular(cache)
    except FileNotFoundError:
        data = {}
        item['before'] = None
    else:
        data = json.loads(latest)
        name = str(index) + '.before'
        write_atomic(backup / name,latest,0o600,0,0)
        item.update(before=name,mode=stat.S_IMODE(info.st_mode),uid=info.st_uid,gid=info.st_gid)
    data.pop('arr',None)
    changes[cache] = (json.dumps(data) + '\n').encode()
    item['after_sha'] = sha(changes[cache])
    write_atomic(backup / 'manifest.json',json.dumps(manifest).encode(),0o600,0,0)
    try:
        trusted_folder(HELPERS, 0o755)
        trusted_folder(SETTINGS, 0o750, owner.pw_gid)
        for item in manifest['files']:
            path = Path(item['path'])
            data = changes[path]
            mode, uid, gid = (item['mode'],item['uid'],item['gid']) if item['before'] else (0o644,0,0)
            if path.parent == SETTINGS:
                mode, uid, gid = 0o640, 0, owner.pw_gid
            elif path == cache and item['before'] is None:
                mode, uid, gid = 0o600, owner.pw_uid, owner.pw_gid
            write_atomic(path,data,mode,uid,gid)
    except BaseException:
        rollback(str(backup), start=False, gateway_stopped=True)
        raise
    print(json.dumps({'prepared':True,'gateway':'stopped','backup':str(backup),'files':len(changes)}))
    return str(backup)


def verify_rollback_files(backup, manifest):
    # Refuse to overwrite edits made after this immediate-cutover snapshot.
    for item in manifest['files']:
        if item.get('disposable'):
            continue
        try:current, _ = read_regular(item['path'])
        except FileNotFoundError:
            if item['before'] is not None:
                raise RuntimeError('Existing installation file disappeared; review rollback')
            continue
        allowed = {item['after_sha']}
        if item['before'] is not None:
            previous, _ = read_regular(backup / item['before'])
            allowed.add(sha(previous))
        if sha(current) not in allowed:
            raise RuntimeError('Installation file changed after cutover; merge before rollback')


def rollback(backup_path, start=True, gateway_stopped=False):
    backup = Path(backup_path)
    if backup.parent != BACKUPS or not backup.name.startswith('native-'):
        raise ValueError('Unexpected native-MCP backup path')
    raw, info = read_regular(backup / 'manifest.json')
    if info.st_uid != 0 or info.st_mode & 0o077:
        raise RuntimeError('Untrusted rollback manifest')
    manifest = json.loads(raw)
    verify_rollback_files(backup,manifest)
    if not gateway_stopped:
        assert_idle(Path(manifest['home']))
        subprocess.run(['systemctl','stop',SERVICE], check=True, capture_output=True)
    verify_rollback_files(backup,manifest)
    for item in manifest['files']:
        path = Path(item['path'])
        if item['before'] is not None:
            data, _ = read_regular(backup / item['before'])
            write_atomic(path,data,item['mode'],item['uid'],item['gid'])
        else:
            try:
                current, _ = read_regular(path)
                if not item.get('disposable') and sha(current) != item['after_sha']:
                    raise RuntimeError('New operator file changed; rollback needs review')
                directory = parent_fd(path)
                try:os.unlink(path.name,dir_fd=directory)
                finally:os.close(directory)
            except FileNotFoundError:
                pass
    for directory in manifest.get('directories', []):
        path = Path(directory['path'])
        info = path.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0:
            raise RuntimeError('Operator directory changed before rollback')
        os.chown(path,directory['uid'],directory['gid'])
        os.chmod(path,directory['mode'])
    if start:
        subprocess.run(['systemctl','start',SERVICE], check=True, capture_output=True)
    print(json.dumps({'restored':True,'gateway':'started' if start else 'stopped'}))
