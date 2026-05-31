"""Entrypoint: serve CatalogService over gRPC, backed by a sqlite file. DB path from
$RAT_CATALOG_DB (default ./catalog.db); address from $RAT_PLUGIN_ADDR
(default 127.0.0.1:0 → an OS-assigned port, printed on startup)."""

import os
from concurrent import futures

import grpc

from rat.catalog.v1 import catalog_pb2_grpc

from server import CatalogServicer
from store import Catalog


def serve() -> None:
    db_path = os.environ.get("RAT_CATALOG_DB", "catalog.db")
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    catalog_pb2_grpc.add_CatalogServiceServicer_to_server(CatalogServicer(Catalog(db_path)), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-catalog-sqlite-py listening on 127.0.0.1:{port} (db={db_path})", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
