"""Entrypoint: serve the DuckLake-backed CatalogService over gRPC. Address from
$RAT_PLUGIN_ADDR (default 127.0.0.1:0). The shared DuckLake comes from
$RAT_DUCKLAKE_META + $RAT_DUCKLAKE_DATA (the same lake the engine attaches); the
catalog's own RAT bookkeeping lives at $RAT_CATALOG_TRACKING (default ./tracking.db)."""

import os
from concurrent import futures

import grpc

from rat.catalog.v1 import catalog_pb2_grpc

from server import CatalogServicer
from store import Catalog


def serve() -> None:
    meta = os.environ["RAT_DUCKLAKE_META"]
    data = os.environ["RAT_DUCKLAKE_DATA"]
    tracking = os.environ.get("RAT_CATALOG_TRACKING", "tracking.db")
    alias = os.environ.get("RAT_DUCKLAKE_ALIAS", "lake")

    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    catalog_pb2_grpc.add_CatalogServiceServicer_to_server(
        CatalogServicer(Catalog(tracking, meta, data, alias)), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-catalog-ducklake-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
