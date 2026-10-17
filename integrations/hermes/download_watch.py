"""Bounded, read-only arr download watches using Hermes no-agent cron delivery.

Run with Hermes's Python: download_watch.py create --help.
Uses native ARR MCP and exact file-id/path import evidence.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import sys
import time
import uuid
from urllib.parse import urlencode

HOME = Path(os.environ.get("HERMES_HOME", str(Path.home() / ".hermes")))
READ_OPS = {"downloads", "history", "episodes", "library", "queue", "magnet_preview", "files"}
CADENCE = 30 * 60
STALL = 2 * 3600
OUTAGE = 3600


def bootstrap():
    os.environ["HERMES_HOME"] = str(HOME)
    sys.path.insert(0, str(HOME / "hermes-agent"))
    import hermes_bootstrap  # noqa: F401


def client():
    spec = importlib.util.spec_from_file_location("arr_watch_native_client", str(Path(__file__).with_name("native_client.py")))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def read(rpc, operation, **arguments):
    if operation not in READ_OPS:
        raise ValueError("The watch can only read media state")
    return rpc(operation, arguments)


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temp = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.replace(temp, path)
    finally:
        temp.unlink(missing_ok=True)


def validate_torrent(torrent):
    if not isinstance(torrent.get("state"), str):
        raise ValueError("Invalid torrent state")
    for key in ["size", "amount_left", "dlspeed", "num_seeds", "eta"]:
        value = torrent.get(key)
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
            raise ValueError("Invalid transfer data")
    progress(torrent)
    return torrent


def resolve_expected(rpc, app, item_id, torrent, seasons):
    # The app's parse result binds the existing qB name/hash to this title and the
    # actual release scope, including individual episodes and partial packs.
    magnet = "magnet:?" + urlencode({"xt": "urn:btih:" + torrent["hash"], "dn": torrent["name"]})
    try:
        preview = read(rpc, "magnet_preview", app=app, item_id=item_id, magnet=magnet)
    except RuntimeError:
        # Non-public downloads may not support public-magnet preview. An exact
        # native queue mapping is an alternative, never a guess based on category.
        rows = []
        for page in range(1, 11):
            result = read(rpc, "queue", app=app, page=page, page_size=100)
            rows.extend(r for r in result["records"] if r.get("downloadId", "").lower() == torrent["hash"].lower())
            if page * 100 >= result["total"]:
                break
        else:
            raise ValueError("Queue mapping exceeds the supported scan bound")
        field = "seriesId" if app == "sonarr" else "movieId"
        if not rows or any(r.get(field) != item_id for r in rows):
            raise ValueError("Hash cannot be tied to this library title")
        expected = {r.get("episodeId") for r in rows} if app == "sonarr" else {item_id}
        if None in expected:
            raise ValueError("Queue does not identify episode scope")
    else:
        if (preview["item"]["id"] != item_id or preview["hash"].lower() != torrent["hash"].lower()
                or preview.get("mapped_episodes_truncated")):
            raise ValueError("Release mapping mismatch or truncation")
        expected = {e["id"] for e in preview["mapped_episodes"]} if app == "sonarr" else {item_id}
    if app == "sonarr":
        requested = set()
        for season in sorted(set(seasons)):
            result = read(rpc, "episodes", series_id=item_id, season_number=season)
            if result["total"] != len(result["episodes"]):
                raise ValueError("Episode response truncated")
            requested.update(e["id"] for e in result["episodes"])
        if not expected or not expected <= requested:
            raise ValueError("Release episode mapping differs from the requested season scope")
    return sorted(expected)


def native_reason(value, limit=250):
    value = re.sub(r"[A-Za-z][A-Za-z0-9+.-]{1,20}:(?://|\?)[^\s]+", "[URL]", str(value))
    value = re.sub(r"<[^>]*>", "", value)
    value = "".join(" " if ord(c) < 32 or 127 <= ord(c) < 160 else c for c in value)
    return " ".join(value.split()).translate(str.maketrans("", "", "*_`"))[:limit]


def collect(config, rpc):
    rows = read(rpc, "downloads", hashes=[config["hash"]], limit=1)["downloads"]
    torrent = next((row for row in rows if row.get("hash", "").lower() == config["hash"]), None)
    if torrent is not None:
        validate_torrent(torrent)
    events = read(rpc, "history", app=config["app"], item_id=config["item_id"], download_id=config["hash"], limit=100)["records"]
    evidence = dict(config.get("evidence", {}))
    for event in events:
        if event.get("downloadId", "").lower() != config["hash"] or event.get("eventType") != "downloadFolderImported":
            continue
        unit = event.get("episodeId") if config["app"] == "sonarr" else event.get("movieId")
        data = event.get("data") or {}
        raw_id = str(data.get("fileId") or "")
        imported_path = data.get("importedPath")
        if unit in config["expected"] and raw_id.isdigit() and int(raw_id) > 0 and isinstance(imported_path, str) and imported_path.startswith("/"):
            evidence[str(unit)] = {"file_id": int(raw_id), "path": imported_path}
    imported = set()
    files = set()
    if evidence:
        current_files = read(rpc, "files", app=config["app"], item_id=config["item_id"])["files"]
        file_paths = {row["id"]: (row.get("path") or row.get("absolutePath")) for row in current_files}
        units = {}
        if config["app"] == "sonarr":
            for season in config["seasons"]:
                result = read(rpc, "episodes", series_id=config["item_id"], season_number=season)
                if result["total"] != len(result["episodes"]):
                    raise ValueError("Episode response truncated")
                for episode in result["episodes"]:
                    if episode.get("hasFile"):
                        units[episode["id"]] = episode.get("episodeFileId")
        else:
            # The movie-specific file listing already binds each returned file
            # to this exact movie. No title-only hasFile flag is sufficient.
            entry = evidence.get(str(config["item_id"])) or {}
            units[config["item_id"]] = entry.get("file_id")
        for unit in config["expected"]:
            entry = evidence.get(str(unit)) or {}
            file_id = entry.get("file_id")
            if file_id and units.get(unit) == file_id and file_paths.get(file_id) == entry.get("path"):
                imported.add(unit)
                files.add(unit)
    blocked = []
    if torrent is not None and completed(torrent) and not set(config["expected"]) <= imported & files:
        field = "seriesId" if config["app"] == "sonarr" else "movieId"
        for page in range(1, 11):
            result = read(rpc, "queue", app=config["app"], page=page, page_size=100)
            for row in result["records"]:
                if (str(row.get("downloadId", "")).lower() != config["hash"]
                        or row.get(field) != config["item_id"] or row.get("trackedDownloadState") != "importBlocked"):
                    continue
                for entry in row.get("statusMessages") or [{"title": "Native importBlocked state (no further reason supplied)"}]:
                    if not isinstance(entry, dict):entry = {"title": "Native importBlocked state (invalid reason entry)"}
                    title = native_reason(entry.get("title") or "", 80)
                    reasons = entry.get("messages") or [title or "Import blocked"]
                    if not isinstance(reasons, list):reasons = [reasons]
                    for reason in reasons:
                        safe = native_reason(reason)
                        if title and title != safe:safe = title + ": " + safe
                        if safe and safe not in blocked:
                            blocked.append(safe)
            if page * 100 >= result["total"]:
                break
            if page == 10:
                raise ValueError("Queue diagnostic scan exceeded its bound")
    return {"torrent": torrent, "imported": sorted(imported), "files": sorted(files), "evidence": evidence,
            "import_blocked": blocked[:3], "import_blocked_extra": max(0, len(blocked)-3)}


def progress(torrent):
    raw = torrent.get("progress", 0)
    if isinstance(raw, bool) or not isinstance(raw, (int, float)) or not math.isfinite(raw):
        raise ValueError("Invalid progress")
    return max(0.0, min(1.0, raw))


def completed(torrent):
    state = torrent.get("state", "")
    return (progress(torrent) == 1 and torrent.get("amount_left") == 0
            and torrent.get("size", 0) > 0
            and state in {"uploading", "stalledUP", "forcedUP", "pausedUP", "stoppedUP", "queuedUP"})


def summary(torrent):
    pct = progress(torrent) * 100
    speed = torrent.get("dlspeed", 0) / 1_000_000
    seeds = torrent.get("num_seeds", 0)
    eta = torrent.get("eta", 0)
    tail = f"; stima ~{math.ceil(eta / 3600)} ore" if 0 < eta < 7 * 86400 else ""
    return f"{pct:.1f}% — {speed:.2f} MB/s, {seeds} seed{tail}. Stima variabile."


def decide(config, snapshot, now):
    """Return (new state, notification, stop reason); no I/O or media writes."""
    state = dict(config)
    label = state["title"]
    if state.get("finished"):
        return state, "", state["finished"]
    if now >= state["expires_at"]:
        return state, f"⏱ {label}: controllo automatico terminato dopo {state['hours']} ore; importazione completa non confermata. Chiedimi se vuoi un nuovo controllo.", "expired"
    if snapshot is None:
        state.setdefault("outage_since", now)
        if now - state["outage_since"] >= OUTAGE:
            return state, f"⚠️ {label}: lo stato non è accessibile o non è valido da un'ora. Interrompo il controllo; serve una verifica. Nessuna modifica ai download.", "connection unavailable"
        return state, "", None
    state.pop("outage_since", None)
    state["imported"] = snapshot["imported"]
    state["evidence"] = dict(snapshot.get("evidence", state.get("evidence", {})))
    expected = set(state["expected"])
    if expected and expected <= set(snapshot["imported"]) & set(snapshot["files"]):
        units = f"tutti i {len(expected)} episodi" if state["app"] == "sonarr" else "il film"
        return state, f"✅ {label}: download importato. Ho confermato {units} nella libreria e nello storico di questa release. Controllo automatico concluso.", "import confirmed"
    torrent = snapshot["torrent"]
    if torrent is None:
        state.setdefault("missing_since", now)
        if now - state["missing_since"] >= STALL:
            return state, f"⚠️ {label}: non compare nei risultati dei download da due ore e non posso confermare l'importazione completa. Controllo concluso; serve una verifica.", "not visible"
        return state, "", None
    state.pop("missing_since", None)
    current = progress(torrent)
    if current > state.get("last_progress", 0) + 0.000001:
        state["last_progress_at"] = now
    state["last_progress"] = current
    state.setdefault("last_progress_at", now)
    status = torrent.get("state", "")
    if status in {"error", "missingFiles"}:
        return state, f"⚠️ {label}: qBittorrent segnala {status}. Controllo concluso; serve una verifica. Non ho tentato riparazioni.", "download error"
    if completed(torrent):
        if snapshot.get("import_blocked"):
            reason = "; ".join(snapshot["import_blocked"])
            if snapshot.get("import_blocked_extra"):reason += f" (+{snapshot['import_blocked_extra']} altri motivi)"
            return state, f"⚠️ {label}: scaricato, ma l'importazione è bloccata: {reason}. Controllo concluso; serve verificare l'importazione. Nessuna modifica ai file.", "import blocked"
        state.setdefault("complete_since", now)
        if now - state["complete_since"] >= STALL:
            count = len(set(snapshot["imported"]) & set(snapshot["files"]) & expected)
            return state, f"⚠️ {label}: scaricato, ma l'importazione completa non è confermata dopo due ore ({count}/{len(expected)} elementi verificati). Controllo concluso; serve verificare la coda/importazione.", "import needs attention"
        if not state.get("complete_notified"):
            state["complete_notified"] = True
            return state, f"📦 {label}: scaricamento completato. Attendo l'importazione automatica; non è ancora confermata nella libreria.", None
        return state, "", None
    # Hash checking is not a failed download or evidence of completion.
    if status.startswith("checking") or status in {"queuedDL", "moving", "allocating"}:
        state["last_progress_at"] = now
        return state, "", None
    if status in {"pausedDL", "stoppedDL"}:
        return state, f"⏸ {label}: download in pausa/arrestato. Interrompo il controllo automatico senza riavviarlo.", "download stopped"
    if now - state["last_progress_at"] >= STALL:
        return state, f"⚠️ {label}: nessun avanzamento rilevato da due ore ({current * 100:.1f}%). Controllo concluso; serve una verifica. Non ho modificato il download.", "download stalled"
    milestone = int(current * 4)
    if milestone > state.get("milestone", 0):
        state["milestone"] = milestone
        return state, f"⬇️ {label}: {summary(torrent)}", None
    return state, "", None


def finish_after_this_run(job_id):
    # Do not pause/remove during the script: that can revoke the delivery fence.
    # Let the native scheduler deliver this run, then exhaust its repeat budget.
    from cron.jobs import get_job, update_job
    job = get_job(job_id)
    if job is None:
        raise RuntimeError("Watch job no longer exists")
    completed_runs = (job.get("repeat") or {}).get("completed", 0)
    update_job(job_id, {"repeat": {"times": completed_runs + 1, "completed": completed_runs}})


def collect_bounded(config, rpc):
    # This helper is Linux-only. Bound ALL reads below the scheduler script timeout,
    # including a hung bridge, so outages use the same quiet/debounced path.
    import signal
    def expired(signum, frame):
        raise TimeoutError("Media read deadline exceeded")
    previous = signal.signal(signal.SIGALRM, expired)
    signal.setitimer(signal.ITIMER_REAL, 40)
    try:
        return collect(config, rpc)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def tick(watch_id):
    import fcntl
    if not re.fullmatch(r"[0-9a-f]{16}", watch_id):
        raise ValueError("Invalid watch ID")
    path = HOME / "cron" / "download-watches" / (watch_id + ".json")
    with path.with_suffix(".lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        config = json.loads(path.read_text())
        now = time.time()
        snapshot = None
        if now < config["expires_at"] and not config.get("finished"):
            try:
                snapshot = collect_bounded(config, client().rpc)
            except Exception:
                # Never copy raw API responses, names/paths, credentials or traceback into alerts.
                snapshot = None
        try:
            state, message, stop = decide(config, snapshot, now)
        except (TypeError, ValueError, KeyError):
            state, message, stop = decide(config, None, now)
        if stop:
            finish_after_this_run(config["job_id"])
            state["finished"] = stop
        atomic_json(path, state)
        if message:
            print(message)


def create(args):
    from cron.jobs import create_job, get_job, is_job_runnable
    from cron.scheduler import _notify_provider_jobs_changed
    import fcntl
    if not re.fullmatch(r"[0-9a-fA-F]{40}", args.hash):
        raise ValueError("Expected one v1 info hash")
    if not re.fullmatch(r"telegram:-?[0-9]+(?::[0-9]+)?", args.deliver):
        raise ValueError("An explicit Telegram chat/topic target is required")
    from native_client import authorized_chats
    if args.deliver.split(":")[1] not in authorized_chats():
        raise ValueError("Target is outside the explicitly authorized conversations")
    if not 1 <= args.hours <= 168 or args.item_id < 1:
        raise ValueError("Invalid hours/item ID")
    directory = HOME / "cron" / "download-watches"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (directory / "create.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        for path in directory.glob("*.json"):
            prior = json.loads(path.read_text())
            if prior["hash"] == args.hash.lower() and prior["deliver"] == args.deliver:
                job = get_job(prior["job_id"])
                if job and is_job_runnable(job):
                    if (prior["app"] != args.app or prior["item_id"] != args.item_id
                            or set(prior["seasons"]) != set(args.seasons)):
                        raise ValueError("An active watch has a different title/scope; inspect it before replacing")
                    return {"already_watching": True, "job_id": job["id"], "next_run_at": job["next_run_at"]}
        rpc = client().rpc
        rows = read(rpc, "downloads", hashes=[args.hash.lower()], limit=1)["downloads"]
        torrent = next((r for r in rows if r.get("hash", "").lower() == args.hash.lower()), None)
        if torrent is None or not torrent.get("category"):
            raise ValueError("Requested hash is not visible in the selected app category")
        validate_torrent(torrent)
        items = read(rpc, "library", app=args.app, query=args.title, limit=100)["items"]
        item = next((i for i in items if i["id"] == args.item_id), None)
        if item is None:
            raise ValueError("Library title/item ID mismatch")
        if args.app == "sonarr" and (not args.seasons or any(s < 0 or s > 200 for s in args.seasons)):
            raise ValueError("Choose the TV season scope explicitly")
        expected = resolve_expected(rpc, args.app, args.item_id, torrent, args.seasons)
        if not expected:
            raise ValueError("No expected library items in scope")
        watch_id = uuid.uuid4().hex[:16]
        script = HOME / "scripts" / ("arr_watch_" + watch_id + ".py")
        source = ("import sys\nsys.path.insert(0, '/opt/hermes-arr')\n"
                  "from download_watch import bootstrap, tick\nbootstrap()\n"
                  f"tick({watch_id!r})\n")
        script.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        script.write_text(source, encoding="utf-8")
        script.chmod(0o600)
        now = time.time()
        config = {"version": 1, "watch_id": watch_id, "app": args.app, "item_id": args.item_id,
                  "hash": args.hash.lower(), "title": item["title"], "seasons": args.seasons,
                  "expected": sorted(set(expected)), "deliver": args.deliver, "hours": args.hours,
                  "created_at": now, "expires_at": now + args.hours * 3600,
                  "last_progress": progress(torrent), "last_progress_at": now,
                  "milestone": int(progress(torrent) * 4), "imported": [], "evidence": {}}
        # Create paused so the ticker cannot race config creation; native API takes the store lock.
        job = create_job(prompt="Read-only media progress/import watch; no model or repairs.",
                         schedule="every 30m", name="Download: " + item["title"],
                         repeat=args.hours * 2 + 16, deliver=args.deliver,
                         origin=None,
                         script=script.name, no_agent=True, enabled_toolsets=[],
                         attach_to_session=True, paused=True,
                         interpreter=sys.executable)
        config["job_id"] = job["id"]
        atomic_json(directory / (watch_id + ".json"), config)
        from cron.jobs import resume_job
        resumed = resume_job(job["id"])
        _notify_provider_jobs_changed()
        return {"job_id": job["id"], "watch_id": watch_id, "deliver": args.deliver,
                "no_agent": True, "interval_minutes": 30, "expires_at": config["expires_at"],
                "expected_units": len(expected), "next_run_at": resumed["next_run_at"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    p = sub.add_parser("create", help="Create one user-requested, bounded read-only watch")
    p.add_argument("--app", choices=["sonarr", "radarr"], required=True)
    p.add_argument("--item-id", type=int, required=True)
    p.add_argument("--hash", required=True)
    p.add_argument("--title", required=True, help="Existing library title, not release name")
    p.add_argument("--seasons", type=int, nargs="+", default=[])
    p.add_argument("--deliver", required=True, help="telegram:chat_id:thread_id")
    p.add_argument("--hours", type=int, default=48)
    args = parser.parse_args()
    bootstrap()
    print(json.dumps(create(args), ensure_ascii=False))


if __name__ == "__main__":
    main()
