"""Entrypoint: serve AuditLogService over gRPC. Address from $RAT_PLUGIN_ADDR.

NOTE: in a real deployment the sink is constructed with the CORE's published Ed25519
verification key. Here a fresh key is generated so the binary boots standalone; a real
plugin would load the core's key from its manifest/config.
"""

import os
from concurrent import futures

import grpc
from cryptography.hazmat.primitives.asymmetric import ed25519

from rat.auditlog.v1 import auditlog_pb2_grpc

from server import AuditLogServicer
from store import AuditSink


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    sink = AuditSink(ed25519.Ed25519PrivateKey.generate().public_key())
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    auditlog_pb2_grpc.add_AuditLogServiceServicer_to_server(AuditLogServicer(sink), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-auditlog-inmemory-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
