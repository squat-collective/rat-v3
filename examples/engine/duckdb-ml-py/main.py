"""Entrypoint: serve the DuckDB-ML EngineService over gRPC. Address from
$RAT_PLUGIN_ADDR (default 127.0.0.1:0 → an OS-assigned port, printed on startup).

A DuckLake is attached iff $RAT_DUCKLAKE_META + $RAT_DUCKLAKE_DATA are set (see
store.DuckLakeConfig) — otherwise the engine runs lake-less (plain SQL + embed())."""

import os
from concurrent import futures

import grpc

from rat.engine.v1 import engine_pb2_grpc

from server import EngineServicer


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    engine_pb2_grpc.add_EngineServiceServicer_to_server(EngineServicer(), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-engine-duckdb-ml-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
