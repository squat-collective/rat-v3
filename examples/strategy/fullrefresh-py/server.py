"""StrategyService gRPC implementation over the full-refresh backend.

Apply(source, target, options) -> WriteResult. The servicer is constructed with an
`invoke` seam (the core capability-invoke gateway, ADR-005); it does NOT dial any
provider directly. RequestContext is NOT a field (ADR-007).
"""

import grpc

from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc

from store import FullRefreshStrategy


class StrategyServicer(strategy_pb2_grpc.StrategyServiceServicer):
    def __init__(self, invoke) -> None:
        self.strategy = FullRefreshStrategy(invoke)

    def Apply(self, request, context):
        try:
            result = self.strategy.apply(
                request.source.identifier, request.target.identifier, request.options
            )
        except ValueError as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        return strategy_pb2.ApplyResponse(result=result)
