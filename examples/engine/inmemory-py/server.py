"""The EngineService gRPC implementation (Python) — second `engine` reference.

Implements the three engine/v1 RPCs against the in-memory mini-SQL store:
  - Execute (rat://engine/v1/execute): CREATE / INSERT for effect → WriteResult.
  - Query   (rat://engine/v1/query):   SELECT → producer-hosted ArrowStream.
  - Preview (rat://engine/v1/preview): SELECT, bounded by `limit` → ArrowStream.

RequestContext is NOT a field (ADR-007): call context rides in the rat-callmeta-bin
metadata header. This reference ignores identity (a conformant choice for a plugin
that does no per-caller authorization of its own).
"""

import grpc

from rat.common.v1 import data_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc

from sql import SQLError, parse_sql
from store import Store
from streams import StreamRegistry


def _snapshot_id(n: int) -> str:
    return f"snap-{n}"


class EngineServicer(engine_pb2_grpc.EngineServiceServicer):
    def __init__(self) -> None:
        self.store = Store()
        self.streams = StreamRegistry()

    def Execute(self, request, context):
        try:
            st = parse_sql(request.sql)
            if st.kind == "create":
                snap = self.store.create(st.table, st.cols)
                return engine_pb2.ExecuteResponse(result=self._wr(0, snap))
            if st.kind == "insert":
                n, snap = self.store.insert(st.table, st.vals)
                return engine_pb2.ExecuteResponse(result=self._wr(n, snap))
            raise SQLError("Execute requires a CREATE or INSERT statement")
        except SQLError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))

    def Query(self, request, context):
        rows = self._run_select(request.sql, 0, context)
        return engine_pb2.QueryResponse(stream=self.streams.put(rows))

    def Preview(self, request, context):
        rows = self._run_select(request.sql, request.limit, context)
        return engine_pb2.PreviewResponse(stream=self.streams.put(rows))

    def _run_select(self, sql, preview_limit, context):
        try:
            st = parse_sql(sql)
            if st.kind != "select":
                raise SQLError("Query/Preview requires a SELECT statement")
            rows = self.store.select_rows(st)
        except SQLError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        limit = st.limit if st.has_limit else -1
        if preview_limit > 0 and (limit < 0 or preview_limit < limit):
            limit = preview_limit
        if limit >= 0 and len(rows) > limit:
            rows = rows[:limit]
        return rows

    @staticmethod
    def _wr(rows: int, snapshot: int) -> data_pb2.WriteResult:
        return data_pb2.WriteResult(rows_affected=rows, snapshot_id=_snapshot_id(snapshot))
