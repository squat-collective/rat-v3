"""The EngineService gRPC implementation over the DuckDB-ML backend.

Identical surface to every engine reference (Execute → WriteResult; Query/Preview →
a producer-hosted ArrowStream). The only delta from duckdb-py's server: Execute also
surfaces the DuckLake snapshot the statement produced in WriteResult.snapshot_id —
the value the strategy hands to catalog.CommitTable (README §10(b): the engine's
write IS the snapshot the catalog records). RequestContext is NOT a field (ADR-007).
"""

import grpc

from rat.common.v1 import data_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc

from store import Engine, EngineError
from streams import StreamRegistry


class EngineServicer(engine_pb2_grpc.EngineServiceServicer):
    def __init__(self, engine: "Engine | None" = None) -> None:
        self.engine = engine if engine is not None else Engine()
        self.streams = StreamRegistry()

    def Execute(self, request, context):
        try:
            n, snapshot = self.engine.execute(request.sql)
        except EngineError as e:
            context.abort(e.code, e.message)
        result = data_pb2.WriteResult(rows_affected=n)
        if snapshot:
            result.snapshot_id = snapshot
        return engine_pb2.ExecuteResponse(result=result)

    def Query(self, request, context):
        try:
            table = self.engine.query(request.sql)
        except EngineError as e:
            context.abort(e.code, e.message)
        return engine_pb2.QueryResponse(stream=self.streams.put(table))

    def Preview(self, request, context):
        try:
            table = self.engine.query(request.sql, request.limit)
        except EngineError as e:
            context.abort(e.code, e.message)
        return engine_pb2.PreviewResponse(stream=self.streams.put(table))
