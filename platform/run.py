#!/usr/bin/env python3
"""run.py — drive the medallion pipeline through the rat serve gateway.

The platform's orchestrator. For each model in the pipeline (in order) it issues
`rat://engine/v1/execute` through the REAL core gateway — so every layer build is
C5-authorized + audited like any other command — building bronze → silver → gold in
the shared DuckLake. It then flushes inlined data to Parquet and verifies by reading
the gold mart straight from the lake (a co-located read — sidesteps the F9 data-leg).

Connects to `rat serve` as the `platform-runner` identity; the daemon launches the
engine + catalog plugins (see plane.yaml). Run via `make platform-demo`.
"""

import os
import pathlib
import sys

import grpc
import yaml

from rat.common.v1 import context_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.engine.v1 import engine_pb2

HERE = pathlib.Path(__file__).resolve().parent


def _yaml(path):
    with open(path) as f:
        return yaml.safe_load(f)


def _callmeta(caller, tenant=""):
    """The call-context envelope the gateway reads: a well-formed traceparent (C1) +
    the caller identity it authorizes on (C5)."""
    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(
            traceparent="00-" + "a" * 32 + "-" + "b" * 16 + "-01",
            correlation_id="platform-run"),
        identity=context_pb2.Identity(caller_plugin=caller, tenant=tenant))
    return [("rat-callmeta-bin", rc.SerializeToString())]


def main():
    cfg = _yaml(HERE / "rat.yaml")
    gateway = os.environ.get("RAT_GATEWAY", cfg["gateway"])
    caller = cfg["caller"]
    project = HERE / cfg["project"]
    landing = HERE / "landing"
    models = _yaml(HERE / cfg["pipeline"])["models"]

    channel = grpc.insecure_channel(gateway)
    stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(channel)
    md = _callmeta(caller)

    def execute(sql):
        req = engine_pb2.ExecuteRequest(sql=sql)
        resp = stub.Invoke(invoke_pb2.InvokeRequest(
            capability="rat://engine/v1/execute", payload=req.SerializeToString()), metadata=md)
        out = engine_pb2.ExecuteResponse()
        out.ParseFromString(resp.result)
        return out.result  # WriteResult

    print(f"🥉🥈🥇 medallion pipeline → rat serve @ {gateway} (as {caller})\n")
    for m in models:
        sql = (project / m).read_text().replace("${LANDING}", str(landing))
        wr = execute(sql)
        print(f"  ✔ {m:<24} rows_affected={wr.rows_affected}")

    # Force inlined data out to Parquet so a separate connection sees committed data.
    execute(f"CALL ducklake_flush_inlined_data('{cfg['lake']['alias']}')")

    print("\n🔍 gold.daily_revenue (read straight from the lake):")
    _verify(cfg["lake"])
    print("\n✅ medallion pipeline complete — every layer built through the real rat serve gateway")


def _verify(lake):
    """Co-located read of the gold mart from the shared DuckLake (M1 verification)."""
    import duckdb

    con = duckdb.connect()
    try:
        con.execute("INSTALL ducklake")
        con.execute("LOAD ducklake")
    except Exception:
        pass
    con.execute(f"ATTACH 'ducklake:{lake['meta']}' AS {lake['alias']} (DATA_PATH '{lake['data']}')")
    cur = con.execute(f"SELECT * FROM {lake['alias']}.gold_daily_revenue ORDER BY order_date")
    cols = [d[0] for d in cur.description]
    rows = cur.fetchall()
    print("   " + "  ".join(cols))
    for r in rows:
        print("   " + "  ".join(str(x) for x in r))


if __name__ == "__main__":
    sys.exit(main())
