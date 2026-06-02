"""DuckDB-ML engine for rat-engine-duckdb-ml-py — the heart of the data-dev plane.

This is the round-2 `duckdb-py` engine reference (a REAL SQL engine returning typed
Arrow) EXTENDED with the three things that turn "compute" into "compute + AI" without
adding an axis or a proto (experiment README §3):

  * extensions: `vss` (HNSW vector index + array_*_distance), `ducklake` (lakehouse
    read/write), `httpfs`/S3 — all loaded best-effort so the engine still starts and
    serves plain SQL offline if an extension can't install;
  * an `embed(text, model)` scalar UDF (embed.py) → the pluggable inference seam;
  * optional ATTACH of a DuckLake (shared metadata DB + Parquet data path) so engine
    SQL can read/write `lake.*` tables. Config via env (see DuckLakeConfig).

It keeps the exact EngineService surface of every engine reference — so it still
passes engine-real-v1 golden vectors (server.py/streams.py are the shared,
unmodified handoff). ML is just SQL inside Execute/Query.

FINDINGS (carried into README §10):
  * DuckLake does NOT support DuckDB's fixed-size ARRAY type (`FLOAT[N]`) — the very
    type `vss`'s HNSW index requires. So embeddings are stored as a VARIABLE list
    `FLOAT[]` and cast to `FLOAT[N]` at query time for array_cosine_distance. Brute-
    force cosine works directly on the lake; an HNSW *index* needs a derived (non-lake)
    fixed-array column.
  * list-returning UDFs (`embed`) require numpy installed (see requirements.txt).
"""

import os

import duckdb
import grpc
import pyarrow as pa

from embed import embed as embed_fn

ENGINE = "DuckDB-ML"

# Extensions we try to load. Best-effort: a failure (e.g. offline INSTALL) degrades
# the corresponding feature but never blocks plain-SQL serving / engine-real vectors.
# `postgres` enables a Postgres-backed DuckLake metadata catalog (remote/scale mode,
# step 3) — harmless when unused (local uses sqlite metadata).
_EXTENSIONS = ("vss", "ducklake", "httpfs", "postgres")


class EngineError(Exception):
    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class DuckLakeConfig:
    """Where the shared DuckLake lives. The engine and the ducklake-py catalog attach
    the SAME metadata DB + data path (experiment README §4): the engine owns compute,
    the catalog owns the metadata view — both see one lake."""

    def __init__(self, meta: str, data: str, alias: str = "lake") -> None:
        self.meta = meta    # ducklake metadata DB, e.g. sqlite:/meta/catalog.db
        self.data = data    # data path for Parquet, e.g. /lake/data/ or s3://bucket/lake/
        self.alias = alias

    @classmethod
    def from_env(cls):
        """Read RAT_DUCKLAKE_META + RAT_DUCKLAKE_DATA; None if no lake is configured
        (the plain-engine mode used by engine-real conformance)."""
        meta = os.environ.get("RAT_DUCKLAKE_META")
        data = os.environ.get("RAT_DUCKLAKE_DATA")
        if not meta or not data:
            return None
        return cls(meta, data, os.environ.get("RAT_DUCKLAKE_ALIAS", "lake"))


class Engine:
    def __init__(self, ducklake: "DuckLakeConfig | None" = None, secret_sql: str = "") -> None:
        self.con = duckdb.connect()  # in-memory; the lake (if any) is attached
        self.loaded = self._load_extensions()
        self._register_embed()
        # Optional `CREATE SECRET … TYPE S3 …` run BEFORE attaching the lake — the
        # remote/scale path (step 3): the engine reads/writes Parquet on S3 with the
        # short-TTL creds a storage plugin vended. Empty locally (sqlite lake, no S3).
        if secret_sql:
            self.con.execute(secret_sql)
        self.lake = ducklake if ducklake is not None else DuckLakeConfig.from_env()
        if self.lake is not None:
            self.attach_lake(self.lake)

    def _load_extensions(self):
        loaded = set()
        for ext in _EXTENSIONS:
            try:
                self.con.execute(f"INSTALL {ext}; LOAD {ext};")
                loaded.add(ext)
            except Exception:  # offline / unavailable — feature degrades, engine still serves
                pass
        if "vss" in loaded:
            # HNSW persistence is experimental; enable so an index can live in a file db.
            try:
                self.con.execute("SET hnsw_enable_experimental_persistence=true;")
            except Exception:
                pass
        return loaded

    def _register_embed(self):
        """Register embed(text, model) -> FLOAT[]. Needs numpy (list-returning UDF)."""
        try:
            self.con.create_function(
                "embed", lambda t, m="hash-256": embed_fn(t, m),
                [duckdb.sqltype("VARCHAR"), duckdb.sqltype("VARCHAR")],
                duckdb.sqltype("FLOAT[]"),
            )
        except Exception:  # numpy missing → embed unavailable, plain SQL still works
            pass

    def attach_lake(self, cfg: "DuckLakeConfig") -> None:
        if "ducklake" not in self.loaded:
            raise EngineError(grpc.StatusCode.FAILED_PRECONDITION,
                              "ducklake extension not loaded; cannot attach lake")
        self.con.execute(
            f"ATTACH 'ducklake:{cfg.meta}' AS {cfg.alias} (DATA_PATH '{cfg.data}')"
        )

    def lake_snapshot(self) -> str:
        """Current DuckLake snapshot id (max) for the attached lake — the value the
        engine returns in WriteResult.snapshot_id so the catalog can record exactly
        what a write produced (README §10(b): the engine's write IS the snapshot the
        catalog tags). Empty string if no lake / no snapshots."""
        if self.lake is None:
            return ""
        try:
            row = self.con.execute(
                f"SELECT max(snapshot_id) FROM {self.lake.alias}.snapshots()"
            ).fetchone()
            return "" if row is None or row[0] is None else f"snap-{row[0]}"
        except Exception:
            return ""

    def execute(self, sql: str):
        """Run DDL/DML; return (rows_affected, snapshot_id). snapshot_id reflects the
        DuckLake snapshot after the statement (empty if no lake)."""
        if not sql.strip():
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, "sql is required")
        try:
            res = self.con.execute(sql)
        except Exception as e:
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        try:
            rows = res.fetchall()
        except Exception:
            rows = []
        n = int(rows[0][0]) if rows and len(rows[0]) == 1 and isinstance(rows[0][0], int) else 0
        return n, self.lake_snapshot()

    def query(self, sql: str, limit: int = 0) -> pa.Table:
        """Run a SELECT (incl. semantic search), returning a real Arrow table."""
        if not sql.strip():
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, "sql is required")
        q = sql if limit <= 0 else f"SELECT * FROM ({sql}) LIMIT {limit}"
        try:
            return self.con.execute(q).to_arrow_table()
        except Exception as e:
            raise EngineError(grpc.StatusCode.INVALID_ARGUMENT, str(e))
