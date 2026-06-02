"""Entrypoint: serve the sql-pipeline StrategyService over gRPC. Address from
$RAT_PLUGIN_ADDR. The pipeline reaches engine + catalog ONLY through the core
capability-invoke gateway ($RAT_GATEWAY) — it names no concrete plugin (ADR-005)."""

import os
from concurrent import futures

import grpc

from rat.strategy.v1 import strategy_pb2_grpc

from server import PipelineStrategy, StrategyServicer


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    strategy_pb2_grpc.add_StrategyServiceServicer_to_server(StrategyServicer(PipelineStrategy()), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-sql-pipeline-py listening on {addr} (port {port})", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
