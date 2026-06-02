"""Entrypoint: serve the Postgres-backed StateService over gRPC. Address from
$RAT_PLUGIN_ADDR; the Postgres DSN from $RAT_STATE_PG."""

import os
from concurrent import futures

import grpc

from rat.state.v1 import state_pb2_grpc

from server import PostgresState


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    state_pb2_grpc.add_StateServiceServicer_to_server(PostgresState(), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-state-postgres-py listening on {addr} (port {port})", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
