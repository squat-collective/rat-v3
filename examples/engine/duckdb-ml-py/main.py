"""Entrypoint: serve the DuckDB-ML EngineService over gRPC. Address from
$RAT_PLUGIN_ADDR (default 127.0.0.1:0 → an OS-assigned port, printed on startup).

A DuckLake is attached iff $RAT_DUCKLAKE_META + $RAT_DUCKLAKE_DATA are set (see
store.DuckLakeConfig) — otherwise the engine runs lake-less (plain SQL + embed()).
When the lake data path is on S3 ($RAT_S3_ENDPOINT set), an S3 SECRET is created from
$RAT_S3_* BEFORE the lake attaches, so the engine can read/write Parquet on S3/MinIO."""

import os
from concurrent import futures

import grpc

from rat.engine.v1 import engine_pb2_grpc

from server import EngineServicer
from store import DuckLakeConfig, Engine


def _s3_secret_sql() -> str:
    """A `CREATE SECRET … TYPE S3` from $RAT_S3_* — empty when no S3 endpoint is set
    (the lake-less / local-fs case). Mirrors the remote pipeline's secret shape."""
    endpoint = os.environ.get("RAT_S3_ENDPOINT")
    if not endpoint:
        return ""

    def q(s: str) -> str:
        return "'" + s.replace("'", "''") + "'"

    use_ssl = os.environ.get("RAT_S3_USE_SSL", "false").lower() == "true"
    return (
        f"CREATE OR REPLACE SECRET s3 (TYPE S3, "
        f"KEY_ID {q(os.environ['RAT_S3_KEY_ID'])}, SECRET {q(os.environ['RAT_S3_SECRET'])}, "
        f"ENDPOINT {q(endpoint)}, URL_STYLE 'path', "
        f"USE_SSL {'true' if use_ssl else 'false'}, REGION {q(os.environ.get('RAT_S3_REGION', 'us-east-1'))})"
    )


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    secret_sql = _s3_secret_sql()
    # When an S3 secret is needed, construct the Engine explicitly so the secret is
    # created before the lake attaches; otherwise EngineServicer builds the default.
    engine = Engine(DuckLakeConfig.from_env(), secret_sql=secret_sql) if secret_sql else None
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    engine_pb2_grpc.add_EngineServiceServicer_to_server(EngineServicer(engine), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-engine-duckdb-ml-py listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
