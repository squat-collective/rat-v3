"""The AuditLogService gRPC implementation (Python) — audit-log sink reference.

Append is CORE-ONLY (the capability is not grantable to ordinary plugins); the records
are core-authored + core-signed. The sink verifies + chain-links them and returns
per-record acks + the chain-head watermark. RequestContext is NOT a field (ADR-007).
"""

from rat.auditlog.v1 import auditlog_pb2, auditlog_pb2_grpc

from store import AuditSink


class AuditLogServicer(auditlog_pb2_grpc.AuditLogServiceServicer):
    def __init__(self, sink: AuditSink) -> None:
        self.sink = sink

    def Append(self, request, context):
        acks, last_id, last_hash = self.sink.append(list(request.records))
        return auditlog_pb2.AppendResponse(
            acks=acks, last_committed_id=last_id, last_committed_hash=last_hash)
