"""The incremental-embed strategy backend — a REAL ELT `kind: strategy` reference.

> 🛰️ EXPLORATORY (experiments/data-dev-plane). Additive; the frozen strategy/v1 surface
> is unchanged. Not a toy full-refresh — a genuine incremental embedding ELT.

Like every strategy, it couples to NO concrete plugin: its only dependency is an
`invoke(capability_uri, request) -> response` seam (the core capability-invoke gateway,
ADR-005), and it composes capabilities by URI. What's different from the generic
full-refresh/scd2 references: this stack has **no format plugin** (DuckLake subsumes it),
so the strategy writes through **`rat://engine/v1/execute`** (the engine writes the lake
directly) rather than `format.overwrite`, and records the snapshot via
`rat://catalog/v1/commit-table`. It is therefore DuckLake-aware in ONE respect — it
addresses tables as `<alias>.<identifier>` in SQL (the engine attaches the lake) — but
still names no specific engine/catalog plugin. (Finding F8, README §10.)

Per run (idempotent via run_id; the §5.4 pattern):
  1. register the target (own the output) + ensure its schema (CTAS-from-source, empty);
  2. WATERMARK — stage only source rows newer than the target's high-water mark, entirely
     server-side (a subquery over the target's max watermark — the strategy never reads
     the value back, so no Arrow round-trip);
  3. MERGE — upsert the staged rows on the business key (changed rows get embedding=NULL
     so they re-embed);
  4. EMBED — embed() ONLY the rows that need it (embedding IS NULL) — the incremental win;
  5. FLUSH + SNAPSHOT — force inlined data out to Parquet, then commit the snapshot.

A re-applied run with the same run_id stages 0 rows (watermark), embeds 0, and the
commit is a no-op (already_applied) — idempotent end to end (C1, ADR-012).
"""

import json

from rat.catalog.v1 import catalog_pb2
from rat.common.v1 import data_pb2
from rat.engine.v1 import engine_pb2

CAP_GET_TABLE = "rat://catalog/v1/get-table"
CAP_REGISTER = "rat://catalog/v1/register-table"
CAP_COMMIT = "rat://catalog/v1/commit-table"
CAP_EXECUTE = "rat://engine/v1/execute"

# What this strategy declares it `requires` (manifest). The gateway denies anything
# outside this set (C5). Notably NO format capability — the engine writes the lake.
REQUIRES = (CAP_GET_TABLE, CAP_REGISTER, CAP_EXECUTE, CAP_COMMIT)

WATERMARK_FLOOR = "TIMESTAMP '1970-01-01'"


class IncrementalEmbedStrategy:
    def __init__(self, invoke) -> None:
        self._invoke = invoke  # (capability_uri, request) -> response — the only seam

    def _execute(self, sql: str) -> data_pb2.WriteResult:
        return self._invoke(CAP_EXECUTE, engine_pb2.ExecuteRequest(sql=sql)).result

    def apply(self, source_id: str, target_id: str, options: bytes, run_id: str = ""):
        spec = json.loads(options.decode("utf-8")) if options else {}
        key = spec.get("key", "id")
        text_col = spec.get("text_column", "text")
        model = spec.get("embed_model", "hash-256")
        wm = spec.get("watermark_column", "_ingested_at")
        cols = spec.get("columns")
        alias = spec.get("alias", "lake")
        if not cols:
            raise ValueError("strategy options must carry a non-empty 'columns' list")
        for required in (key, text_col, wm):
            if required not in cols:
                raise ValueError(f"'columns' must include {required!r}")

        idem = run_id or f"{source_id}->{target_id}"
        src = f"{alias}.{source_id}"
        tgt = f"{alias}.{target_id}"
        collist = ", ".join(cols)
        s_collist = ", ".join(f"s.{c}" for c in cols)
        set_clause = ", ".join(f"{c} = s.{c}" for c in cols if c != key) + ", embedding = NULL"

        # 0. resolve/verify the source on the catalog axis (it must already be landed).
        self._invoke(CAP_GET_TABLE, catalog_pb2.GetTableRequest(identifier=source_id))
        # 1. own the target + ensure its schema (source columns + an embedding FLOAT[]).
        self._invoke(CAP_REGISTER, catalog_pb2.RegisterTableRequest(identifier=target_id))
        self._execute(
            f"CREATE TABLE IF NOT EXISTS {tgt} AS "
            f"SELECT {collist}, CAST(NULL AS FLOAT[]) AS embedding FROM {src} WHERE false")

        # 2. WATERMARK — stage only rows newer than the target's high-water mark.
        self._execute(
            f"CREATE OR REPLACE TEMP TABLE _rat_stg AS "
            f"SELECT {collist} FROM {src} s "
            f"WHERE s.{wm} > (SELECT coalesce(max({wm}), {WATERMARK_FLOOR}) FROM {tgt})")

        # 3. MERGE — upsert on the business key (changed rows → embedding=NULL → re-embed).
        self._execute(
            f"MERGE INTO {tgt} t USING _rat_stg s ON t.{key} = s.{key} "
            f"WHEN MATCHED THEN UPDATE SET {set_clause} "
            f"WHEN NOT MATCHED THEN INSERT ({collist}, embedding) VALUES ({s_collist}, NULL)")

        # 4. EMBED — only the rows that need it (the incremental win). rows_affected here
        #    is the count embedded THIS run — the headline incrementality signal (full on
        #    the first run, just the delta on the next, 0 on an idempotent replay).
        embedded = self._execute(
            f"UPDATE {tgt} SET embedding = embed({text_col}, '{model}') WHERE embedding IS NULL"
        ).rows_affected

        # 5. FLUSH inlined data out to Parquet, then read the resulting snapshot.
        snapshot = self._execute(f"CALL ducklake_flush_inlined_data('{alias}')").snapshot_id

        # 6. commit the snapshot under the run's idempotency key (C1).
        commit = self._invoke(CAP_COMMIT, catalog_pb2.CommitTableRequest(
            identifier=target_id, snapshot_id=snapshot, idempotency_key=idem))

        return data_pb2.WriteResult(
            rows_affected=embedded, snapshot_id=snapshot,
            already_applied=commit.already_applied)
