"""Entrypoint: serve the dbt-runner over gRPC. Address from $RAT_PLUGIN_ADDR; the dbt
project from $RAT_DBT_PROJECT. It executes `dbt build` — dbt is the pipeline language;
rat just routes a run to it (ADR-021)."""

import os
import threading
from concurrent import futures

import grpc

from rat.contrib import contribute_ui
from rat.strategy.v1 import strategy_pb2_grpc

from server import DbtRunner, StrategyServicer


def _contribute_ui() -> None:
    """This plugin contributes its OWN UI (ADR-024) in one SDK call — a Lake Tables view +
    a Run-pipeline command. retries rides out the state plugin still wiring at boot."""
    gw = os.environ.get("RAT_GATEWAY")
    if not gw:
        return
    caller = os.environ.get("RAT_PLUGIN_NAME", "rat-pipeline")
    target = os.environ.get("RAT_PIPELINE_TARGET", "gold_daily_revenue")
    # PER-SURFACE interfaces (ADR-025): the same plugin presents a vscode interface AND a cli
    # one, from the same capabilities. Each surface consumer pulls only its own.
    components = [
        {"slot": "explorer", "surface": "vscode", "id": "lake-tables", "title": "Lake Tables", "icon": "database",
         "data": "/api/tables", "item": "/api/table/"},
        {"slot": "command", "surface": "vscode", "id": "run-pipeline", "title": "Run pipeline", "icon": "play",
         "capability": "rat://strategy/v1/apply",
         "args": {"target": {"identifier": target}, "idempotencyKey": "ui-run"}},
        {"slot": "command", "surface": "cli", "id": "build", "title": "Build the medallion",
         "capability": "rat://strategy/v1/apply",
         "args": {"target": {"identifier": target}, "idempotencyKey": "cli-build"}},
        # webapp surface: a Lake Tables view + a Run button (same capabilities, browser-flavored)
        {"slot": "explorer", "surface": "webapp", "id": "lake-tables", "title": "Lake Tables",
         "data": "/api/tables", "item": "/api/table/"},
        {"slot": "command", "surface": "webapp", "id": "run-pipeline", "title": "Run pipeline",
         "capability": "rat://strategy/v1/apply",
         "args": {"target": {"identifier": target}, "idempotencyKey": "webapp-run"}},
    ]
    try:
        contribute_ui(gw, caller, components, retries=120)
        print(f"{caller}: contributed {len(components)} UI components", flush=True)
    except Exception as e:  # never block serving on the UI contribution
        print(f"{caller}: UI contribution failed: {e}", flush=True)


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    strategy_pb2_grpc.add_StrategyServiceServicer_to_server(StrategyServicer(DbtRunner()), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-dbt-runner listening on {addr} (port {port})", flush=True)
    threading.Thread(target=_contribute_ui, daemon=True).start()  # publish UI once, in the background
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
