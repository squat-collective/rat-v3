"""StrategyService gRPC implementation over the incremental-embed backend.

Apply(source, target, options) -> WriteResult. Constructed with the `invoke` seam (the
core capability-invoke gateway, ADR-005); it dials no provider directly. The run's
idempotency_key (ApplyRequest.idempotency_key) threads into the catalog commit so a
re-applied run is a no-op end to end (C1). RequestContext is NOT a field (ADR-007).
"""

import grpc

from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc

from store import IncrementalEmbedStrategy


class StrategyServicer(strategy_pb2_grpc.StrategyServiceServicer):
    def __init__(self, invoke) -> None:
        self.strategy = IncrementalEmbedStrategy(invoke)

    def Apply(self, request, context):
        try:
            result = self.strategy.apply(
                request.source.identifier, request.target.identifier,
                request.options, run_id=request.idempotency_key)
        except ValueError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        return strategy_pb2.ApplyResponse(result=result)
