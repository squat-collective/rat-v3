"""StrategyService gRPC implementation over the SCD2 backend.

Apply(source, target, options) -> WriteResult. Built with the `invoke` gateway seam
(ADR-005) plus a Flight host/pull (SCD2 reads + writes bulk data). RequestContext is
NOT a field (ADR-007).
"""

import grpc

from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc

from flight import FlightHost, pull
from store import SCD2Strategy


class StrategyServicer(strategy_pb2_grpc.StrategyServiceServicer):
    def __init__(self, invoke) -> None:
        self._flight = FlightHost()
        self.strategy = SCD2Strategy(invoke, self._flight.host_rows, pull)

    def close(self) -> None:
        self._flight.stop()

    def Apply(self, request, context):
        try:
            result = self.strategy.apply(
                request.source.identifier, request.target.identifier, request.options
            )
        except (ValueError, KeyError) as e:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, f"invalid SCD2 options: {e}")
        return strategy_pb2.ApplyResponse(result=result)
