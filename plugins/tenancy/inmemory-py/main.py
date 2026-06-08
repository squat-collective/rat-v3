"""Entrypoint: serve TenancyService over gRPC. Address from $RAT_PLUGIN_ADDR
(default 127.0.0.1:0 → an OS-assigned port, printed on startup)."""

import os
from concurrent import futures

import grpc

from rat.tenancy.v1 import tenancy_pb2_grpc

from server import TenancyServicer


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    tenancy_pb2_grpc.add_TenancyServiceServicer_to_server(TenancyServicer(), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-tenancy-inmemory-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
