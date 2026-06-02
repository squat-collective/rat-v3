#!/usr/bin/env python3
"""run.py — run the medallion pipeline through the rat serve gateway (ADR-020 S1).

Connects to a running `rat serve` as `platform-runner` and issues each medallion model
as `rat://engine/v1/execute` through the gateway (C5-authorized + audited), building
bronze → silver → gold in the shared DuckLake; flushes to Parquet; commits the gold
snapshot to the DuckLake **catalog** (also through the gateway); then verifies by
reading the gold mart back from the lake.

Env-driven (the compose stack sets these; local-friendly defaults):
  RAT_GATEWAY            gateway addr               (default 127.0.0.1:7777)
  RAT_PLATFORM_CALLER    caller identity            (default platform-runner)
  RAT_PLATFORM_LANDING   landing dir AS THE ENGINE SEES IT (default <here>/landing)
  RAT_DUCKLAKE_META/DATA/ALIAS              the lake (engine writes it; runner reads it)
  RAT_S3_ENDPOINT/KEY_ID/SECRET/USE_SSL/REGION   S3 creds for the lake data path
"""

import os
import pathlib
import sys

import grpc

from rat.catalog.v1 import catalog_pb2
from rat.common.v1 import context_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.engine.v1 import engine_pb2

HERE = pathlib.Path(__file__).resolve().parent
PIPELINE = ["bronze/orders.sql", "silver/orders.sql", "gold/daily_revenue.sql"]


def env(k, d=""):
    return os.environ.get(k, d)


def _callmeta(caller, tenant="acme"):
    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(traceparent="00-" + "a" * 32 + "-" + "b" * 16 + "-01", correlation_id="platform-run"),
        identity=context_pb2.Identity(caller_plugin=caller, tenant=tenant))
    return [("rat-callmeta-bin", rc.SerializeToString())]


def main():
    gateway = env("RAT_GATEWAY", "127.0.0.1:7777")
    caller = env("RAT_PLATFORM_CALLER", "platform-runner")
    landing = env("RAT_PLATFORM_LANDING", str(HERE / "landing"))
    alias = env("RAT_DUCKLAKE_ALIAS", "lake")

    _ensure_bucket()

    stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(gateway))
    md = _callmeta(caller)

    def invoke(cap, req, resp_cls):
        r = stub.Invoke(invoke_pb2.InvokeRequest(capability=cap, payload=req.SerializeToString()), metadata=md)
        out = resp_cls()
        out.ParseFromString(r.result)
        return out

    def execute(sql):
        return invoke("rat://engine/v1/execute", engine_pb2.ExecuteRequest(sql=sql), engine_pb2.ExecuteResponse).result

    print(f"🥉🥈🥇 medallion → rat serve @ {gateway} (as {caller})\n")
    for m in PIPELINE:
        sql = (HERE / "project" / m).read_text().replace("${LANDING}", landing)
        wr = execute(sql)
        print(f"  ✔ {m:<24} rows_affected={wr.rows_affected}")

    snap = execute(f"CALL ducklake_flush_inlined_data('{alias}')").snapshot_id

    # commit the gold snapshot to the DuckLake catalog — also through the gateway.
    invoke("rat://catalog/v1/register-table", catalog_pb2.RegisterTableRequest(identifier="gold_daily_revenue"), catalog_pb2.RegisterTableResponse)
    commit = invoke("rat://catalog/v1/commit-table",
                    catalog_pb2.CommitTableRequest(identifier="gold_daily_revenue", snapshot_id=snap, idempotency_key="platform-medallion-1"),
                    catalog_pb2.CommitTableResponse)
    print(f"\n  ✔ catalog.commit gold_daily_revenue  snapshot={snap!r}  already_applied={commit.already_applied}")

    print("\n🔍 gold.daily_revenue (read from the lake):")
    _verify(alias)
    print("\n✅ medallion complete — every layer built + cataloged through the real rat serve gateway")


def _ensure_bucket():
    ep = env("RAT_S3_ENDPOINT")
    if not ep:
        return
    import boto3

    bucket = env("RAT_DUCKLAKE_DATA", "s3://rat/").split("/")[2]
    scheme = "https://" if env("RAT_S3_USE_SSL", "false").lower() == "true" else "http://"
    s3 = boto3.client("s3", endpoint_url=scheme + ep,
                      aws_access_key_id=env("RAT_S3_KEY_ID"), aws_secret_access_key=env("RAT_S3_SECRET"),
                      region_name=env("RAT_S3_REGION", "us-east-1"))
    try:
        s3.create_bucket(Bucket=bucket)
    except Exception:
        pass


def _verify(alias):
    import duckdb

    meta, data = env("RAT_DUCKLAKE_META"), env("RAT_DUCKLAKE_DATA")
    remote = meta.startswith("postgres")
    con = duckdb.connect()
    for ext in (("httpfs", "postgres", "ducklake") if remote else ("ducklake",)):
        try:
            con.execute(f"INSTALL {ext}")
            con.execute(f"LOAD {ext}")
        except Exception:
            pass
    if env("RAT_S3_ENDPOINT"):
        con.execute(f"CREATE OR REPLACE SECRET s3 (TYPE S3, KEY_ID '{env('RAT_S3_KEY_ID')}', "
                    f"SECRET '{env('RAT_S3_SECRET')}', ENDPOINT '{env('RAT_S3_ENDPOINT')}', URL_STYLE 'path', "
                    f"USE_SSL {env('RAT_S3_USE_SSL', 'false')}, REGION '{env('RAT_S3_REGION', 'us-east-1')}')")
    con.execute(f"ATTACH 'ducklake:{meta}' AS {alias} (DATA_PATH '{data}')")
    cur = con.execute(f"SELECT * FROM {alias}.gold_daily_revenue ORDER BY order_date")
    cols = [d[0] for d in cur.description]
    print("   " + "  ".join(cols))
    for r in cur.fetchall():
        print("   " + "  ".join(str(x) for x in r))


if __name__ == "__main__":
    sys.exit(main())
