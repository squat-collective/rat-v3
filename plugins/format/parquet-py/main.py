"""Entrypoint: serve FormatService (Parquet-backed) over gRPC. Data root from
$RAT_FORMAT_ROOT (default ./format-root); address from $RAT_PLUGIN_ADDR
(default 127.0.0.1:0 → an OS-assigned port, printed on startup)."""

import os
from concurrent import futures

import grpc

from rat.format.v1 import format_pb2_grpc

from server import FormatServicer
from store import Store


def serve() -> None:
    root = os.environ.get("RAT_FORMAT_ROOT", "format-root")
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    format_pb2_grpc.add_FormatServiceServicer_to_server(FormatServicer(Store(root)), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-format-parquet-py listening on 127.0.0.1:{port} (root={root})", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
