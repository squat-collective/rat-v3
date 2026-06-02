#!/usr/bin/env python3
"""run.py — trigger the medallion pipeline ONCE, through the rat serve gateway (ADR-020 S2).

Invokes `rat://strategy/v1/apply` on the sql-pipeline strategy plugin — which runs
bronze → silver → gold through the gateway and commits the gold snapshot to the DuckLake
catalog — then verifies by reading the gold mart from the lake. This is the *same*
capability the scheduler invokes on a cron; `run.py` is just the manual trigger.

Env-driven (the compose stack sets these; local-friendly defaults):
  RAT_GATEWAY            gateway addr               (default 127.0.0.1:7777)
  RAT_PLATFORM_CALLER    caller identity            (default platform-runner)
  RAT_PIPELINE_TARGET    the table the pipeline commits (default gold_daily_revenue)
  RAT_DUCKLAKE_META/DATA/ALIAS              the lake (the runner reads it back to verify)
  RAT_S3_ENDPOINT/KEY_ID/SECRET/USE_SSL/REGION   S3 creds for the lake data path
"""

import os
import sys

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.strategy.v1 import strategy_pb2


def env(k, d=""):
    return os.environ.get(k, d)


def main():
    gateway = env("RAT_GATEWAY", "127.0.0.1:7777")
    caller = env("RAT_PLATFORM_CALLER", "platform-runner")
    target = env("RAT_PIPELINE_TARGET", "gold_daily_revenue")
    alias = env("RAT_DUCKLAKE_ALIAS", "lake")

    _ensure_bucket()

    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(traceparent="00-" + "a" * 32 + "-" + "b" * 16 + "-01", correlation_id="platform-run"),
        identity=context_pb2.Identity(caller_plugin=caller, tenant="acme"))
    md = [("rat-callmeta-bin", rc.SerializeToString())]
    stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(gateway))

    print(f"🥉🥈🥇 trigger medallion → rat serve @ {gateway}  (strategy.apply, as {caller})\n")
    req = strategy_pb2.ApplyRequest(target=data_pb2.TableRef(identifier=target), idempotency_key="platform-medallion-1")
    r = stub.Invoke(invoke_pb2.InvokeRequest(capability="rat://strategy/v1/apply", payload=req.SerializeToString()), metadata=md)
    resp = strategy_pb2.ApplyResponse()
    resp.ParseFromString(r.result)
    wr = resp.result
    print(f"  ✔ strategy.apply → rows_affected={wr.rows_affected}  snapshot={wr.snapshot_id!r}  already_applied={wr.already_applied}")

    print("\n🔍 gold.daily_revenue (read from the lake):")
    _verify(alias)
    print("\n✅ medallion triggered through the real rat serve gateway (orchestrator → strategy → engine + catalog)")


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
    for row in cur.fetchall():
        print("   " + "  ".join(str(x) for x in row))


if __name__ == "__main__":
    sys.exit(main())
