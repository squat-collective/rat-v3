"""The EngineService gRPC implementation over a real SQL engine. Identical for both
real-engine references; only store.py (the Engine backend) differs.

Execute (DDL/DML) → WriteResult; Query/Preview (SELECT) → a producer-hosted
ArrowStream carrying the real Arrow result. RequestContext is NOT a field (ADR-007).
"""

import grpc

from rat.common.v1 import data_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc

from store import Engine, EngineError
from streams import StreamRegistry


class EngineServicer(engine_pb2_grpc.EngineServiceServicer):
    def __init__(self) -> None:
        self.engine = Engine()
        self.streams = StreamRegistry()

    def Execute(self, request, context):
        try:
            n = self.engine.execute(request.sql)
        except EngineError as e:
            context.abort(e.code, e.message)
        return engine_pb2.ExecuteResponse(result=data_pb2.WriteResult(rows_affected=n))

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
