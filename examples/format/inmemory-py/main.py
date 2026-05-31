"""Entrypoint: serve FormatService over gRPC.

A real `kind: format` plugin is a gRPC server the core (or a test harness) dials.
This wires the in-memory implementation onto a listener. Address comes from
$RAT_PLUGIN_ADDR (default 127.0.0.1:0 -> an OS-assigned port, printed on startup so a
harness can read it); a real deployment-runtime would inject the address.
"""

import os
from concurrent import futures

import grpc

from rat.format.v1 import format_pb2_grpc

from server import FormatServicer


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    format_pb2_grpc.add_FormatServiceServicer_to_server(FormatServicer(), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-format-inmemory-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
