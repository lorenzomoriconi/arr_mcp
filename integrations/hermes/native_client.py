"""Read-only native MCP access for deterministic Hermes download watches.

The interactive agent uses Hermes's native HTTP MCP connector directly. This
module is only a cron helper; it uses Hermes's installed official Python SDK.
Credentials and endpoint policy are operator-owned files outside the workspace.
"""
from __future__ import annotations

import asyncio
import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import ssl
import stat
from urllib.parse import urlsplit

SETTINGS = Path('/etc/hermes-arr/endpoint.json')
TOKEN = Path('/etc/hermes-arr/broker.token')
WATCH_POLICY = Path('/etc/hermes-arr/watch-policy.json')
READ_TOOLS = {
    'qbittorrent_list_torrents', 'sonarr_list_series', 'radarr_list_movies',
    'sonarr_list_episodes', 'sonarr_history', 'radarr_history',
    'sonarr_queue', 'radarr_queue', 'sonarr_magnet_preview', 'radarr_magnet_preview',
    'sonarr_list_episode_files', 'radarr_list_movie_files',
}


def operator_read(path: Path, limit=16384):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > limit:
            raise RuntimeError('Untrusted operator-managed media settings')
        with os.fdopen(descriptor, 'r', encoding='utf-8', closefd=False) as stream:
            value = stream.read(limit + 1)
        if len(value) > limit:
            raise RuntimeError('Oversized operator-managed media settings')
        return value
    finally:
        os.close(descriptor)


def authorized_chats():
    policy = json.loads(operator_read(WATCH_POLICY))
    chats = policy.get('authorized_chats')
    if not isinstance(chats, list) or not chats or any(not isinstance(c, str) or not re.fullmatch(r'-?[0-9]+', c) for c in chats):
        raise RuntimeError('A valid operator-owned watch delivery policy is required')
    return set(chats)


def endpoint():
    settings = json.loads(operator_read(SETTINGS))
    uri = urlsplit(settings['url'])
    address = str(ipaddress.ip_address(settings['address']))
    if uri.scheme != 'https' or not uri.hostname or uri.username or uri.password or uri.query or uri.fragment:
        raise RuntimeError('A private verified HTTPS MCP endpoint is required')
    resolved = {str(ipaddress.ip_address(row[4][0])) for row in socket.getaddrinfo(uri.hostname, uri.port or 443, type=socket.SOCK_STREAM)}
    if resolved != {address}:
        raise RuntimeError('MCP hostname is not pinned to its operator-managed private address')
    return settings['url']


async def _call(name, arguments):
    from tools import mcp_tool as core
    core._ensure_mcp_sdk()
    if name not in READ_TOOLS:
        raise ValueError('Download watches only use allowlisted read tools')
    url = endpoint()  # Verify routing before reading/transmitting the token.
    token = operator_read(TOKEN, 512).strip()
    if not token or '\n' in token or '\r' in token:
        raise RuntimeError('Invalid scoped MCP token')
    httpx = core.sdk_httpx()
    context = ssl.create_default_context(cafile='/etc/ssl/certs/ca-certificates.crt')
    async with httpx.AsyncClient(headers={'Authorization': 'Bearer ' + token}, verify=context,
                                 trust_env=False, follow_redirects=False, timeout=100.0) as http:
        async with core.streamable_http_client(url, http_client=http, terminate_on_close=False) as streams:
            async with core.ClientSession(streams[0], streams[1], read_timeout_seconds=100.0) as session:
                if hasattr(session, 'discover'):
                    await session.discover()
                else:
                    await session.initialize()
                result = await session.call_tool(name, arguments)
                if getattr(result, 'is_error', getattr(result, 'isError', False)):
                    raise RuntimeError('Native media read returned an error')
                structured = getattr(result, 'structured_content', getattr(result, 'structuredContent', None))
                if isinstance(structured, dict):
                    return structured
                for entry in result.content:
                    if getattr(entry, 'type', '') == 'text':
                        return json.loads(entry.text)
    raise RuntimeError('Native media read returned no structured result')


def call(name, arguments):
    if name not in READ_TOOLS:
        raise ValueError('Download watches only use allowlisted read tools')
    try:
        return asyncio.run(_call(name, arguments))
    except Exception:
        raise RuntimeError('Native media read failed; inspect the private MCP connection') from None


def rpc(operation, args):
    """Small read-only data adapter for the stable watch state machine."""
    if operation == 'downloads':
        parameters = {'limit': args.get('limit', 100)}
        if args.get('hashes'):
            parameters['hashes'] = args['hashes']
        result = call('qbittorrent_list_torrents', parameters)
        mapped = []
        names = {'amountLeft': 'amount_left', 'downloadSpeed': 'dlspeed', 'uploadSpeed': 'upspeed', 'seeds': 'num_seeds', 'leechers': 'num_leechs'}
        for row in result['torrents']:
            mapped.append({names.get(key, key): value for key, value in row.items()})
        return {'downloads': mapped}
    app = args.get('app', 'sonarr')
    if app not in ('sonarr', 'radarr'):
        raise ValueError('Unknown media app')
    if operation == 'library':
        key = 'series' if app == 'sonarr' else 'movies'
        result = call(app + '_list_' + key, {})
        query = args.get('query', '').casefold()
        return {'items': [row for row in result[key] if query in row['title'].casefold()]}
    if operation == 'episodes':
        result = call('sonarr_list_episodes', {'seriesId': args['series_id']})
        rows = result['episodes']
        if args.get('season_number') is not None:
            rows = [row for row in rows if row['seasonNumber'] == args['season_number']]
        return {'episodes': rows, 'total': len(rows)}
    if operation == 'files':
        parameters = {'seriesId': args['item_id']} if app == 'sonarr' else {'movieId': args['item_id']}
        name = 'sonarr_list_episode_files' if app == 'sonarr' else 'radarr_list_movie_files'
        return call(name, parameters)
    if operation == 'queue':
        result = call(app + '_queue', {'page': args.get('page', 1), 'pageSize': args.get('page_size', 100)})
        return {'records': result['items'], 'total': result['total']}
    if operation == 'history':
        records = []
        parameters = {'seriesId' if app == 'sonarr' else 'movieId': args['item_id'], 'limit': 100}
        if args.get('download_id'):
            parameters['downloadId'] = args['download_id']
        for page in range(1, 11):
            result = call(app + '_history', dict(parameters, page=page))
            records.extend(result['records'])
            if page * 100 >= result['total']:
                return {'records': records}
        # Partial history is not an assertion of complete import. The collector
        # only accepts exact-hash evidence matching current file IDs and paths.
        return {'records': records, 'truncated': True}
    if operation == 'magnet_preview':
        result = call(app + '_magnet_preview', {'itemId': args['item_id'], 'magnet': args['magnet']})
        return {'item': {'id': result['itemId']}, 'hash': result['hash'],
                'mapped_episodes': [{'id': value} for value in result.get('episodeIds', [])],
                'mapped_episodes_truncated': False}
    raise ValueError('Download watch operation is not supported')
