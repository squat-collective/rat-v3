"""Entrypoint: serve the dbt-runner over gRPC. Address from $RAT_PLUGIN_ADDR; the dbt
project from $RAT_DBT_PROJECT. It executes `dbt build` — dbt is the pipeline language;
rat just routes a run to it (ADR-021)."""

import os
from concurrent import futures

import grpc

from rat.strategy.v1 import strategy_pb2_grpc

from server import DbtRunner, StrategyServicer


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    strategy_pb2_grpc.add_StrategyServiceServicer_to_server(StrategyServicer(DbtRunner()), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-dbt-runner listening on {addr} (port {port})", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
