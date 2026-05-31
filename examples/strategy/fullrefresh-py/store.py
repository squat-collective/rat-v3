"""The full-refresh strategy backend — the FIRST `kind: strategy` reference (ADR-003).

A strategy is "the cleanest expression of the capability model" (strategy.proto): it
`requires` capabilities and works across EVERY provider that offers them, naming none.
This one does a full refresh: read a source, transform it with SQL, OVERWRITE a target.

What makes it a faithful reference is what it does NOT know: it holds no stub, port,
or class of any engine/format/catalog. It has exactly one dependency — an `invoke`
function `(capability_uri, request) -> response` — the core capability-invoke gateway
(ADR-005). It composes three capabilities by URI:

  rat://catalog/v1/get-table  — resolve the source + target logical names to TableRefs
  rat://engine/v1/query       — run the transform SQL, binding the source ref; the
                                 ENGINE itself resolves that ref via a format `scan`
                                 capability (a second cross-axis hop) and streams the
                                 result back as Arrow
  rat://format/v1/overwrite   — write the result Arrow stream into the target

Swap parquet->delta, duckdb->datafusion, sqlite->in-memory catalog underneath: this
code does not change, because it couples to none of them. That invariance, proven on
golden data across the substitutions, is the ADR-003 cross-combination gate.
"""

import json

from rat.catalog.v1 import catalog_pb2
from rat.engine.v1 import engine_pb2
from rat.format.v1 import format_pb2

CAP_GET_TABLE = "rat://catalog/v1/get-table"
CAP_QUERY = "rat://engine/v1/query"
CAP_OVERWRITE = "rat://format/v1/overwrite"

# What this strategy declares it `requires` (manifest). The gateway enforces that a
# capability not in this set is denied (C5) — the strategy can reach ONLY these.
REQUIRES = (CAP_GET_TABLE, CAP_QUERY, CAP_OVERWRITE)


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
        target_ref = self._get_table(target_id)

        # engine.Query binds the source TableRef (it resolves it via format `scan`)
        # and streams the transformed result back out-of-band as Arrow.
        q = self._invoke(CAP_QUERY, engine_pb2.QueryRequest(sql=sql, tables=[source_ref]))

        # format.Overwrite pulls that result stream and replaces the target.
        w = self._invoke(
            CAP_OVERWRITE, format_pb2.OverwriteRequest(table=target_ref, source=q.stream)
        )
        return w.result
