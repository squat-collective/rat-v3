"""The full-refresh strategy backend — the FIRST `kind: strategy` reference (ADR-003).

A strategy is "the cleanest expression of the capability model" (strategy.proto): it
`requires` capabilities and works across EVERY provider that offers them, naming none.
This one does a full refresh: read a source, transform it with SQL, OVERWRITE a target.

What makes it a faithful reference is what it does NOT know: it holds no stub, port,
or class of any engine/format/catalog. It has exactly one dependency — an `invoke`
function `(capability_uri, request) -> response` — the core capability-invoke gateway
(ADR-005). It composes five capabilities by URI:

  rat://catalog/v1/get-table     — resolve the source logical name to a TableRef
  rat://catalog/v1/register-table— create the pipeline's OWN output table (idempotent)
  rat://engine/v1/query          — run the transform SQL, binding the source ref; the
                                    ENGINE itself resolves that ref via a format `scan`
                                    capability (a second cross-axis hop) and streams the
                                    result back as Arrow
  rat://format/v1/overwrite      — write the result Arrow stream into the target
  rat://catalog/v1/commit-table  — record WHICH snapshot the write produced (commit-
                                    linkage), closing the create→write→register loop
                                    on the wire (ADR-010)

Swap parquet->delta, duckdb->datafusion, sqlite->in-memory catalog underneath: this
code does not change, because it couples to none of them. That invariance, proven on
golden data across the substitutions, is the ADR-003 cross-combination gate.
"""

import json

from rat.catalog.v1 import catalog_pb2
from rat.engine.v1 import engine_pb2
from rat.format.v1 import format_pb2

CAP_GET_TABLE = "rat://catalog/v1/get-table"
CAP_REGISTER = "rat://catalog/v1/register-table"
CAP_COMMIT = "rat://catalog/v1/commit-table"
CAP_QUERY = "rat://engine/v1/query"
CAP_OVERWRITE = "rat://format/v1/overwrite"

# What this strategy declares it `requires` (manifest). The gateway enforces that a
# capability not in this set is denied (C5) — the strategy can reach ONLY these.
REQUIRES = (CAP_GET_TABLE, CAP_REGISTER, CAP_COMMIT, CAP_QUERY, CAP_OVERWRITE)


class FullRefreshStrategy:
    def __init__(self, invoke) -> None:
        # invoke(capability_uri, request_message) -> response_message — the only seam.
        self._invoke = invoke

    def _get_table(self, identifier: str):
        resp = self._invoke(CAP_GET_TABLE, catalog_pb2.GetTableRequest(identifier=identifier))
        return resp.table

    def apply(self, source_id: str, target_id: str, options: bytes):
        # options is UTF-8 JSON validated against the strategy's metadata_schema
        # (manifest) — here just {"sql": "..."} (strategy.proto API-12 encoding pin).
        spec = json.loads(options.decode("utf-8")) if options else {}
        sql = spec.get("sql")
        if not sql:
            raise ValueError("strategy options must carry a non-empty 'sql'")

        source_ref = self._get_table(source_id)
        # The pipeline OWNS its output: register the target table in the catalog
        # (idempotent) rather than assuming an admin pre-created it. This is the
        # "register" leg of create→write→register→merge (ADR-010 / reviews/08 B1).
        target_ref = self._invoke(
            CAP_REGISTER, catalog_pb2.RegisterTableRequest(identifier=target_id)
        ).table

        # engine.Query binds the source TableRef (it resolves it via format `scan`)
        # and streams the transformed result back out-of-band as Arrow.
        q = self._invoke(CAP_QUERY, engine_pb2.QueryRequest(sql=sql, tables=[source_ref]))

        # format.Overwrite pulls that result stream and replaces the target.
        w = self._invoke(
            CAP_OVERWRITE, format_pb2.OverwriteRequest(table=target_ref, source=q.stream)
        )

        # Commit-linkage: record WHICH snapshot the write produced, so the catalog
        # learns what format.Write landed (ADR-010). idempotency_key makes a reconciler
        # retry of the same logical run a no-op.
        self._invoke(CAP_COMMIT, catalog_pb2.CommitTableRequest(
            identifier=target_id, branch=target_ref.branch,
            snapshot_id=w.result.snapshot_id, idempotency_key=f"{source_id}->{target_id}"))
        return w.result
