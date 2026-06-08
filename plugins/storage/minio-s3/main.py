"""Entrypoint: serve rat-storage-minio-s3 over gRPC. Address from $RAT_PLUGIN_ADDR
(default 127.0.0.1:0).

Minter selection (creds.py): if $MINIO_ENDPOINT is set, vend REAL MinIO STS creds;
otherwise vend offline scope receipts (the conformance / no-MinIO mode).

env (real mode):
  MINIO_ENDPOINT      host:port of the MinIO S3 API (e.g. rat-minio:9000)
  MINIO_ROOT_USER     admin key the plugin mints scoped creds from
  MINIO_ROOT_PASSWORD admin secret
  RAT_S3_BUCKET       bucket the lake lives in (default 'rat')
  MINIO_USE_SSL       'true' for https (default false)
  MINIO_REGION        S3 region (default us-east-1)
  RAT_CRED_TTL_SECONDS short-TTL for vended creds (default 900)
"""

import os
from concurrent import futures

import grpc

from rat.storage.v1 import storage_pb2_grpc

from creds import DEFAULT_TTL_SECONDS, MinioSTSMinter, ScopeReceiptMinter
from server import StorageServicer


def _build_minter():
    ttl = int(os.environ.get("RAT_CRED_TTL_SECONDS", DEFAULT_TTL_SECONDS))
    endpoint = os.environ.get("MINIO_ENDPOINT")
    if not endpoint:
        return ScopeReceiptMinter(ttl_seconds=ttl)
    return MinioSTSMinter(
        endpoint=endpoint,
        bucket=os.environ.get("RAT_S3_BUCKET", "rat"),
        admin_key=os.environ["MINIO_ROOT_USER"],
        admin_secret=os.environ["MINIO_ROOT_PASSWORD"],
        region=os.environ.get("MINIO_REGION", "us-east-1"),
        use_ssl=os.environ.get("MINIO_USE_SSL", "false").lower() == "true",
        ttl_seconds=ttl,
    )


def serve() -> None:
    addr = os.environ.get("RAT_PLUGIN_ADDR", "127.0.0.1:0")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    storage_pb2_grpc.add_StorageServiceServicer_to_server(StorageServicer(_build_minter()), server)
    port = server.add_insecure_port(addr)
    server.start()
    print(f"rat-storage-minio-s3 listening on 127.0.0.1:{port}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
