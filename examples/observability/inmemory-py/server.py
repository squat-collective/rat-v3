"""The ObservabilityService gRPC implementation (Python) — observability reference.

Ingest is BIDI-streaming (observability.proto API-4 / freeze-blocker #9): for every
inbound batch the sink emits one IngestResponse with the CUMULATIVE accepted/rejected
counts for the stream, so a process-lifetime telemetry stream gets incremental
backpressure instead of a single terminal ack. RequestContext is NOT a field (ADR-007).
"""

from rat.observability.v1 import observability_pb2, observability_pb2_grpc

from store import TelemetrySink


class ObservabilityServicer(observability_pb2_grpc.ObservabilityServiceServicer):
    def __init__(self, sink: TelemetrySink = None) -> None:
        self.sink = sink or TelemetrySink()

    def Ingest(self, request_iterator, context):
        accepted = rejected = 0
        for req in request_iterator:
            acc, rej = self.sink.ingest(req.points)
            accepted += acc
            rejected += rej
            yield observability_pb2.IngestResponse(accepted=accepted, rejected=rejected)
