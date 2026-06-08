"""Entrypoint: serve BillingService over gRPC. Address from $RAT_PLUGIN_ADDR
(default 127.0.0.1:0 → an OS-assigned port, printed on startup)."""

import os
from concurrent import futures

import grpc

from rat.billing.v1 import billing_pb2_grpc

from server import BillingServicer


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    billing_pb2_grpc.add_BillingServiceServicer_to_server(BillingServicer(), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-billing-inmemory-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
