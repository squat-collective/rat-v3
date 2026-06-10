"""sqlite-backed key/value state for rat-state-sqlite-py — the ROUND-2 reference
(ADR-003): a technologically-divergent backend, not another in-memory twin.

Where inmemory-{go,py} hold state in a hashmap behind a mutex, this backs it with
a real embedded transactional SQL database (sqlite, file-on-disk, WAL). That gives
two properties the in-memory twins can only fake — and which round-2 actually tests
(see harness_test.py): DURABILITY (state survives a process/connection restart) and
LINEARIZABLE compare-and-set enforced by the *backend's* locking (BEGIN IMMEDIATE),
not an in-process mutex. The MODEL matches the in-memory references exactly (global
monotonic revision; append-only change log for Watch) so all three pass the SAME
golden vectors.

The CAS is the load-bearing bit: each Put runs read→check→write inside a
BEGIN IMMEDIATE transaction, so sqlite serializes concurrent writers and exactly one
CAS from a given expected revision can commit (the lease primitive — reviews/06 C-4).
"""

import sqlite3
import threading
from typing import List, Tuple

_SCHEMA = """
CREATE TABLE IF NOT EXISTS kv   (key TEXT PRIMARY KEY, value BLOB, revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS meta (id INTEGER PRIMARY KEY CHECK(id=0), rev INTEGER NOT NULL);
INSERT OR IGNORE INTO meta(id, rev) VALUES (0, 0);
CREATE TABLE IF NOT EXISTS log  (seq INTEGER PRIMARY KEY AUTOINCREMENT, key TEXT NOT NULL, value BLOB, revision INTEGER NOT NULL);
"""


class Store:
    """A sqlite-backed store. Connections are per-thread (sqlite3 connections are not
    shareable across threads); all threads contend on the same file, which is what
    makes the concurrency test a REAL linearizability test."""

    def __init__(self, path: str) -> None:
        self._path = path
        self._local = threading.local()
        self._conn().executescript(_SCHEMA)

    def _conn(self) -> sqlite3.Connection:
        c = getattr(self._local, "conn", None)
        if c is None:
            # isolation_level=None → autocommit; we drive BEGIN IMMEDIATE ourselves.
            c = sqlite3.connect(self._path, isolation_level=None)
            c.execute("PRAGMA busy_timeout=5000")  # wait on the write lock, don't error
            c.execute("PRAGMA journal_mode=WAL")
            self._local.conn = c
        return c

    def get(self, key: str):
        row = self._conn().execute("SELECT value, revision FROM kv WHERE key=?", (key,)).fetchone()
        if row is None:
            return False, b"", 0
        return True, bytes(row[0]) if row[0] is not None else b"", row[1]

    def put(self, key: str, value: bytes, if_rev: int):
        """Transactional compare-and-set. Returns (committed, revision). On commit,
        revision is the new global rev; on conflict, the current (conflicting) rev
        and no write. BEGIN IMMEDIATE makes the read→check→write atomic + serialized
        across writers, so exactly one CAS from a given if_rev can win."""
        c = self._conn()
        c.execute("BEGIN IMMEDIATE")
        try:
            row = c.execute("SELECT revision FROM kv WHERE key=?", (key,)).fetchone()
            cur = row[0] if row else 0
            if if_rev > 0 and cur != if_rev:
                c.execute("ROLLBACK")
                return False, cur
            c.execute("UPDATE meta SET rev = rev + 1 WHERE id=0")
            nr = c.execute("SELECT rev FROM meta WHERE id=0").fetchone()[0]
            c.execute("INSERT OR REPLACE INTO kv(key, value, revision) VALUES (?, ?, ?)", (key, value, nr))
            c.execute("INSERT INTO log(key, value, revision) VALUES (?, ?, ?)", (key, value, nr))
            c.execute("COMMIT")
            return True, nr
        except Exception:
            c.execute("ROLLBACK")
            raise

    def create_if_absent(self, key: str, value: bytes):
        """Transactional atomic create-if-absent (ADR-049). Returns (created, revision): created
        with the new global rev, or (False, existing rev) if the key already existed (no write).
        BEGIN IMMEDIATE serializes writers, so exactly one of N concurrent creators commits."""
        c = self._conn()
        c.execute("BEGIN IMMEDIATE")
        try:
            row = c.execute("SELECT revision FROM kv WHERE key=?", (key,)).fetchone()
            if row is not None:
                c.execute("ROLLBACK")
                return False, row[0]  # already exists
            c.execute("UPDATE meta SET rev = rev + 1 WHERE id=0")
            nr = c.execute("SELECT rev FROM meta WHERE id=0").fetchone()[0]
            c.execute("INSERT INTO kv(key, value, revision) VALUES (?, ?, ?)", (key, value, nr))
            c.execute("INSERT INTO log(key, value, revision) VALUES (?, ?, ?)", (key, value, nr))
            c.execute("COMMIT")
            return True, nr
        except Exception:
            c.execute("ROLLBACK")
            raise

    def list(self, prefix: str) -> List[str]:
        rows = self._conn().execute("SELECT key FROM kv ORDER BY key").fetchall()
        return [r[0] for r in rows if r[0].startswith(prefix)]

    def watch_backlog(self, prefix: str, from_rev: int) -> List[Tuple[str, bytes, int]]:
        if from_rev == 0:
            return []
        rows = self._conn().execute(
            "SELECT key, value, revision FROM log WHERE revision >= ? ORDER BY seq", (from_rev,)
        ).fetchall()
        return [(r[0], bytes(r[1]) if r[1] is not None else b"", r[2]) for r in rows if r[0].startswith(prefix)]

    def close(self) -> None:
        c = getattr(self._local, "conn", None)
        if c is not None:
            c.close()
            self._local.conn = None
