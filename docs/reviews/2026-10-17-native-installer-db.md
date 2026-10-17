# Installer database-size fix review

Actual Claude Code `claude-opus-5-5`, effort `xhigh`, read-only review. Real-VM preflight exposed this before any gateway stop. Twelve operator tests and the real database idle check passed after the fix. The native server/image code is unchanged.

**Verdict: approve. I found no blocker and no regression.**

I read `DELTA.patch` and checked it against the full `vm_install.py` and `test_vm_install.py`. I didn't run or edit anything.

**Correctness**
- **Safety checks are unchanged.** The new code in `assert_idle` (`vm_install.py:97-106`) uses the same protections as `read_regular`: it opens the parent folder one level at a time without following links (`parent_fd`), opens `state.db` without following a link, uses non-blocking mode so a named pipe can't make it hang, and checks the file type on the open file. The only things removed are the 4 MB size limit and reading the file's contents, and both removals are intended.
- **Every open file is closed, even on errors.** If `os.open` fails (missing file, a symlink, permissions), `directory` is still closed. If `fstat` fails or the type check raises, `descriptor` is still closed. `parent_fd` cleans up after itself on failure.
- **The query is unchanged**: same read-only URI, same lease count. The `rollback()` path also calls `assert_idle` (`vm_install.py:303`), so it gets the same fix. Before, it would also have refused before stopping the gateway on a database over 4 MB.
- **Failure is still safe.** Any error is raised before `systemctl stop`, the backup, or any write.

**Test** (`test_vm_install.py:137-145`)
- A 5 MiB zeroblob in a normal (non-WAL) database committed by `with` makes the file larger than 4 MiB. So the first `assert_idle` call would have raised the "oversized" `RuntimeError` before the fix, which matches the failing run you saw. The lease check after that is real: one unexpired row leads to "active turn".
- Nit: the local `import time` is fine, but a module-level import would match the rest of the file.

**Pre-existing limits (not caused by this patch, not blockers)**
1. **Gap between the check and SQLite's open.** The checked file is closed before SQLite reopens the database by path. A service user could swap `state.db` in between. The worst cases are:
   - a read-only open of a different file as root;
   - a forged "idle" answer, but that user already controls whether Hermes is idle;
   - a hang, if a named pipe is swapped in, before anything has changed.

   Don't try to fix this by opening through `/proc/self/fd/N`. SQLite would then look for its `-wal`/`-shm`/`-journal` files next to that fake path, which breaks WAL databases.
2. **Database contents are trusted.** As root, SQLite reads a database file the service user controls, with SQLite's default settings for trusting the database schema. The connection is read-only and extension loading is off, so the impact is small. This was already the case before the patch.
3. **Optional cleanup.** The open-and-check-type code now repeats part of `read_regular`. You could share it in a small helper later, but it doesn't need to be part of this fix.