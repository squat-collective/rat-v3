"""sqlite-backed catalog for rat-catalog-sqlite-py — a ROUND-2 reference (ADR-003).

A technologically-divergent backend: branches, the snapshot of each, and the
idempotency ledger live in a real embedded transactional SQL database (sqlite,
file-on-disk, WAL) instead of an in-memory dict. The MODEL matches the in-memory
references exactly (global merge counter; main seeded at snap-0) so it passes the
SAME golden vectors — but it earns two properties the in-memory catalog can only
fake (see harness_test.py): DURABILITY (branches + the merge ledger survive a
restart) and CONCURRENT-MERGE lost-update prevention (the optimistic-concurrency
guard enforced by sqlite's BEGIN IMMEDIATE, not an in-process mutex — the publish
gate of the pipeline model, reviews/06 #8).
"""

import sqlite3
import threading

import grpc

SEED_TABLE = "warehouse.sales.orders"

_SCHEMA = """
CREATE TABLE IF NOT EXISTS branches (name TEXT PRIMARY KEY, snapshot TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tables   (identifier TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS merges   (idempotency_key TEXT PRIMARY KEY, snapshot TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS meta     (id INTEGER PRIMARY KEY CHECK(id=0), counter INTEGER NOT NULL);
INSERT OR IGNORE INTO branches(name, snapshot) VALUES ('main', 'snap-0');
INSERT OR IGNORE INTO tables(identifier)       VALUES ('warehouse.sales.orders');
INSERT OR IGNORE INTO meta(id, counter)        VALUES (0, 0);
"""


class CatalogError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class Catalog:
    def __init__(self, path: str) -> None:
        self._path = path
        self._local = threading.local()
        self._conn().executescript(_SCHEMA)

    def _conn(self) -> sqlite3.Connection:
        c = getattr(self._local, "conn", None)
        if c is None:
            c = sqlite3.connect(self._path, isolation_level=None)  # autocommit; we drive BEGIN IMMEDIATE
            c.execute("PRAGMA busy_timeout=5000")
            c.execute("PRAGMA journal_mode=WAL")
            self._local.conn = c
        return c

    def get_table(self, identifier: str, branch: str):
        if not identifier:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "identifier is required")
        c = self._conn()
        if c.execute("SELECT 1 FROM tables WHERE identifier=?", (identifier,)).fetchone() is None:
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown table {identifier!r}")
        branch = branch or "main"
        if c.execute("SELECT 1 FROM branches WHERE name=?", (branch,)).fetchone() is None:
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
        return branch, f"catalog://{identifier}@{branch}"

    def create_branch(self, branch: str, from_branch: str) -> None:
        if not branch:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "branch is required")
        from_branch = from_branch or "main"
        c = self._conn()
        row = c.execute("SELECT snapshot FROM branches WHERE name=?", (from_branch,)).fetchone()
        if row is None:
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown from_branch {from_branch!r}")
        c.execute("INSERT OR REPLACE INTO branches(name, snapshot) VALUES (?, ?)", (branch, row[0]))

    def merge_branch(self, branch: str, into: str, expected: str, key: str):
        """Optimistic-concurrency + idempotent merge, in a BEGIN IMMEDIATE transaction
        so concurrent merges into the same target serialize and can't lost-update.
        Returns (snapshot, already_applied)."""
        c = self._conn()
        c.execute("BEGIN IMMEDIATE")
        try:
            if key:
                row = c.execute("SELECT snapshot FROM merges WHERE idempotency_key=?", (key,)).fetchone()
                if row is not None:
                    c.execute("COMMIT")
                    return row[0], True
            if c.execute("SELECT 1 FROM branches WHERE name=?", (branch,)).fetchone() is None:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
            into_row = c.execute("SELECT snapshot FROM branches WHERE name=?", (into,)).fetchone()
            if into_row is None:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown into_branch {into!r}")
            cur = into_row[0]
            if expected and expected != cur:
                raise CatalogError(
                    grpc.StatusCode.FAILED_PRECONDITION,
                    f"into_branch {into!r} is at {cur!r}, not the expected {expected!r} (concurrent merge?)",
                )
            c.execute("UPDATE meta SET counter = counter + 1 WHERE id=0")
            counter = c.execute("SELECT counter FROM meta WHERE id=0").fetchone()[0]
            snap = f"snap-{counter}"
            c.execute("UPDATE branches SET snapshot=? WHERE name=?", (snap, into))
            if key:
                c.execute("INSERT INTO merges(idempotency_key, snapshot) VALUES (?, ?)", (key, snap))
            c.execute("COMMIT")
            return snap, False
        except BaseException:
            c.execute("ROLLBACK")
            raise

    def close(self) -> None:
        c = getattr(self._local, "conn", None)
        if c is not None:
            c.close()
            self._local.conn = None
