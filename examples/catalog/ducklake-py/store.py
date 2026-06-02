"""DuckLake-backed catalog for rat-catalog-ducklake-py — the data-dev plane catalog.

> 🛰️ EXPLORATORY (experiments/data-dev-plane). Additive; the frozen catalog/v1 surface
> is unchanged. This is the §10(b) resolution of the catalog/engine-boundary tension:
> the ENGINE owns compute (it writes data + produces the DuckLake snapshot), the
> CATALOG owns the RAT-axis metadata view (it RECORDS what landed and resolves refs).
> Both attach the SAME DuckLake (shared metadata DB + same Parquet data path).

What is genuinely DuckLake-backed (the real lake is the source of truth):
  * `GetTable` verifies the table exists in the lake (information_schema) and resolves
    the table's CURRENT real snapshot (lake.snapshots()).
  * `CommitTable` records the snapshot id the engine reported (a real `snap-N` from the
    DuckLake transaction) — durable, idempotent, optimistic-concurrency-guarded.

What is a deliberately-thin tracker (the acknowledged spike, README §10 Q2):
  * branches. This DuckLake build has no native branch primitive, so branch tips +
    merge live in the catalog's OWN sqlite bookkeeping DB (NOT DuckLake's internal
    metadata — we never write peer state). Mapping RAT branches onto DuckLake snapshot
    lineage is the open design question; this keeps the capability surface complete so
    the rest of the stack can exercise it while the model is decided.

NOTE: we only ever SELECT `snapshot_id` from lake.snapshots() — selecting
`snapshot_time` pulls a timestamptz conversion that needs `pytz` (a finding worth its
own line; avoided here so the catalog has no pytz dep).
"""

import sqlite3
import threading

import duckdb
import grpc

_SCHEMA = """
CREATE TABLE IF NOT EXISTS branches    (name TEXT PRIMARY KEY, snapshot TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tables      (identifier TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS merges      (idempotency_key TEXT PRIMARY KEY, snapshot TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS commits     (identifier TEXT, branch TEXT, snapshot TEXT NOT NULL, PRIMARY KEY(identifier, branch));
CREATE TABLE IF NOT EXISTS commit_keys (idempotency_key TEXT PRIMARY KEY, snapshot TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS meta        (id INTEGER PRIMARY KEY CHECK(id=0), counter INTEGER NOT NULL);
INSERT OR IGNORE INTO branches(name, snapshot) VALUES ('main', 'snap-0');
INSERT OR IGNORE INTO meta(id, counter)        VALUES (0, 0);
"""


class CatalogError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class Catalog:
    """A DuckLake-backed catalog. `tracking` is the catalog's own sqlite bookkeeping
    file; `meta`/`data` point at the shared DuckLake the engine also attaches."""

    def __init__(self, tracking: str, meta: str, data: str, alias: str = "lake") -> None:
        self._tracking = tracking
        self._meta = meta
        self._data = data
        self._alias = alias
        self._local = threading.local()
        self._conn().executescript(_SCHEMA)
        # Pre-install the ducklake extension ONCE (network/cache); per-read connections
        # then just LOAD it (fast, local).
        boot = duckdb.connect()
        boot.execute("INSTALL ducklake;")
        boot.close()

    # --- the catalog's own sqlite bookkeeping (branches, linkage, idempotency) -----

    def _conn(self) -> sqlite3.Connection:
        c = getattr(self._local, "conn", None)
        if c is None:
            c = sqlite3.connect(self._tracking, isolation_level=None)
            c.execute("PRAGMA busy_timeout=5000")
            c.execute("PRAGMA journal_mode=WAL")
            self._local.conn = c
        return c

    # --- the DuckLake view (the real lake is the source of truth here) -------------
    #
    # Opened SHORT-LIVED per read, never held: the DuckLake metadata is a single sqlite
    # file and sqlite is single-writer — a catalog connection held open would lock out
    # the engine's write COMMIT ("database is locked"). A real multi-writer deployment
    # uses a Postgres metadata DB; for the local sqlite demo, brief read connections
    # between the engine's (idle-at-read-time) writes are the right discipline. (Finding,
    # README §10.)

    def _lake_read(self, fn):
        con = duckdb.connect()
        try:
            con.execute("LOAD ducklake;")
            con.execute(f"ATTACH 'ducklake:{self._meta}' AS {self._alias} (DATA_PATH '{self._data}')")
            return fn(con)
        finally:
            con.close()

    def _lake_has_table(self, identifier: str) -> bool:
        def q(con):
            rows = con.execute(
                "SELECT table_schema, table_name FROM information_schema.tables "
                "WHERE table_catalog = ?", [self._alias]
            ).fetchall()
            return any(identifier in (name, f"{schema}.{name}") for schema, name in rows)
        return self._lake_read(q)

    def _lake_snapshot(self) -> str:
        def q(con):
            row = con.execute(
                f"SELECT max(snapshot_id) FROM {self._alias}.snapshots()"
            ).fetchone()
            return "snap-0" if row is None or row[0] is None else f"snap-{row[0]}"
        return self._lake_read(q)

    def _uri(self, identifier: str, branch: str, snapshot: str) -> str:
        return f"ducklake://{self._alias}/{identifier}@{branch}#{snapshot}"

    # --- catalog/v1 surface --------------------------------------------------------

    def get_table(self, identifier: str, branch: str):
        """Resolve identifier → (branch, uri). Existence + snapshot come from the REAL
        lake; branch from the catalog's bookkeeping. NOT_FOUND if the lake has no such
        table (error-model M2: a GetTable on an asserted-existing table is an error,
        not found=false)."""
        if not identifier:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "identifier is required")
        if not self._lake_has_table(identifier):
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown table {identifier!r}")
        branch = branch or "main"
        c = self._conn()
        if c.execute("SELECT 1 FROM branches WHERE name=?", (branch,)).fetchone() is None:
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
        return branch, self._uri(identifier, branch, self._lake_snapshot())

    def register_table(self, identifier: str, uri: str, branch: str):
        """Record that a (lake) table is tracked by the RAT catalog. Idempotent. The
        actual DuckLake table is created by the engine's DDL (README §10(b)); this is
        the RAT-axis registration half of create→write→register→commit. Returns
        (branch, uri)."""
        if not identifier:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "identifier is required")
        branch = branch or "main"
        c = self._conn()
        if c.execute("SELECT 1 FROM branches WHERE name=?", (branch,)).fetchone() is None:
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
        c.execute("INSERT OR IGNORE INTO tables(identifier) VALUES (?)", (identifier,))
        return branch, uri or self._uri(identifier, branch, self._lake_snapshot())

    def commit_table(self, identifier: str, branch: str, snapshot: str, expected: str, key: str):
        """Record the DuckLake snapshot a write produced for (table, branch) — the
        real commit-linkage. Safe under retry + concurrency (BEGIN IMMEDIATE +
        idempotency_key + expected_snapshot optimistic guard), exactly the ADR-010
        model. The snapshot value is the engine-reported `snap-N`. Durable. Returns
        (snapshot, already_applied)."""
        if not identifier:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "identifier is required")
        if not snapshot:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "snapshot_id is required")
        branch = branch or "main"
        c = self._conn()
        c.execute("BEGIN IMMEDIATE")
        try:
            if key:
                row = c.execute("SELECT snapshot FROM commit_keys WHERE idempotency_key=?", (key,)).fetchone()
                if row is not None:
                    c.execute("COMMIT")
                    return row[0], True
            if c.execute("SELECT 1 FROM tables WHERE identifier=?", (identifier,)).fetchone() is None:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown table {identifier!r} (register first)")
            if c.execute("SELECT 1 FROM branches WHERE name=?", (branch,)).fetchone() is None:
                raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown branch {branch!r}")
            row = c.execute("SELECT snapshot FROM commits WHERE identifier=? AND branch=?", (identifier, branch)).fetchone()
            cur = row[0] if row is not None else ""
            if expected and expected != cur:
                raise CatalogError(
                    grpc.StatusCode.FAILED_PRECONDITION,
                    f"table {identifier!r} on branch {branch!r} is at {cur!r}, not the expected {expected!r} (concurrent commit?)",
                )
            c.execute("INSERT OR REPLACE INTO commits(identifier, branch, snapshot) VALUES (?, ?, ?)", (identifier, branch, snapshot))
            if key:
                c.execute("INSERT INTO commit_keys(idempotency_key, snapshot) VALUES (?, ?)", (key, snapshot))
            c.execute("COMMIT")
            return snapshot, False
        except BaseException:
            c.execute("ROLLBACK")
            raise

    def create_branch(self, branch: str, from_branch: str) -> None:
        """Open a branch (thin tracker — the §10 Q2 spike). Branch tip = the
        from_branch's tip at creation time."""
        if not branch:
            raise CatalogError(grpc.StatusCode.INVALID_ARGUMENT, "branch is required")
        from_branch = from_branch or "main"
        c = self._conn()
        row = c.execute("SELECT snapshot FROM branches WHERE name=?", (from_branch,)).fetchone()
        if row is None:
            raise CatalogError(grpc.StatusCode.NOT_FOUND, f"unknown from_branch {from_branch!r}")
        c.execute("INSERT OR REPLACE INTO branches(name, snapshot) VALUES (?, ?)", (branch, row[0]))

    def merge_branch(self, branch: str, into: str, expected: str, key: str):
        """Merge a branch back (thin tracker). Optimistic-concurrency + idempotent,
        same safety model as commit_table. Returns (snapshot, already_applied)."""
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
            snap = f"merge-{counter}"
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
