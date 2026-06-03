#!/usr/bin/env python3
"""bff.py — the platform UI's backend, routed through the REAL rat serve gateway (ADR-020 S4b).

A thin JSON-over-HTTP adapter: a VS Code extension (or any web UI) talks JSON here, and
every CONTROL call is issued to `rat serve` as a capability invocation (C5-authorized +
audited) — the portal-replacement's backend, on the real orchestrator. It is the honest
minimum of the F9 split: control through the gateway; the bulk data-leg (table/row
preview) would attach its own engine to the lake (out of scope for this slice).

  GET  /api/health → { ok, gateway }
  GET  /api/runs   → the run history          (rat://state/v1/list + get)
  POST /api/run    → trigger a medallion refresh (rat://strategy/v1/apply)
"""

import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.secret.v1 import secret_pb2
from rat.state.v1 import state_pb2
from rat.strategy.v1 import strategy_pb2

GATEWAY = os.environ.get("RAT_GATEWAY", "127.0.0.1:7777")
CALLER = os.environ.get("RAT_PLUGIN_NAME", "platform-bff")
TARGET = os.environ.get("RAT_PIPELINE_TARGET", "gold_daily_revenue")


# --- the bulk DATA leg (Q2): read the medallion tables straight from the shared DuckLake ---
# Control (trigger / run history) goes through the gateway above; the bulk data leg attaches
# the same lake read-only and returns rows. This is the honest F9 split — control mediated,
# data direct — and it is what lets the UI actually SEE the pipeline's output.
_lake_lock = threading.Lock()
_lake_con = None


def _resolve(ref):
    """Resolve a secret ref via the gateway's secret-backend (C5-authorized + audited)."""
    stub, md = _stub(), _md()
    resp = secret_pb2.ResolveResponse()
    resp.ParseFromString(stub.Invoke(invoke_pb2.InvokeRequest(
        capability="rat://secret/v1/resolve",
        payload=secret_pb2.ResolveRequest(secret_ref=ref).SerializeToString()), metadata=md).result)
    if not resp.found:
        raise RuntimeError(f"secret {ref!r} not found (absent or not authorized)")
    return resp.value.decode("utf-8")


def _cfg(name, default=None):
    """A config value: the literal env var, or — if only <name>_REF is set — the secret it
    resolves to (so lake creds live on the secret plugin, not in plugins.yaml)."""
    if name in os.environ:
        return os.environ[name]
    ref = os.environ.get(name + "_REF")
    return _resolve(ref) if ref else default


def _lake():
    global _lake_con
    if _lake_con is None:
        import duckdb
        c = duckdb.connect()
        for e in ("ducklake", "httpfs", "postgres"):
            c.execute(f"INSTALL {e}; LOAD {e};")
        c.execute("CREATE SECRET s3sec (TYPE S3, KEY_ID '%s', SECRET '%s', ENDPOINT '%s', URL_STYLE 'path', USE_SSL false)" % (
            _cfg("RAT_S3_KEY", "minioadmin"), _cfg("RAT_S3_SECRET", "minioadmin"),
            _cfg("RAT_S3_ENDPOINT", "host.containers.internal:59010")))
        c.execute("ATTACH 'ducklake:postgres:%s' AS lake (DATA_PATH '%s', READ_ONLY)" % (
            _cfg("RAT_LAKE_PG"), _cfg("RAT_LAKE_DATA", "s3://rat/lake/")))
        _lake_con = c
    return _lake_con


def _lake_query(sql):
    with _lake_lock:
        cur = _lake().execute(sql)
        cols = [d[0] for d in cur.description]
        return cols, cur.fetchall()


def _tables():
    _, rows = _lake_query("SELECT table_name FROM information_schema.tables WHERE table_catalog='lake' ORDER BY 1")
    return [r[0] for r in rows]


def _table(name, limit):
    if not name.replace("_", "").isalnum():
        raise ValueError("invalid table name")
    cols, rows = _lake_query(f"SELECT * FROM lake.main.{name} LIMIT {int(limit)}")
    return {"columns": cols, "rows": rows}


def _md():
    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(traceparent="00-" + "a" * 32 + "-" + "b" * 16 + "-01", correlation_id="bff"),
        identity=context_pb2.Identity(caller_plugin=CALLER, tenant="acme"))
    return [("rat-callmeta-bin", rc.SerializeToString())]


def _stub():
    return invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(GATEWAY))


def _runs():
    stub, md = _stub(), _md()
    lr = state_pb2.ListResponse()
    lr.ParseFromString(stub.Invoke(invoke_pb2.InvokeRequest(
        capability="rat://state/v1/list", payload=state_pb2.ListRequest(prefix="runs/").SerializeToString()), metadata=md).result)
    out = []
    for k in lr.keys:
        gr = state_pb2.GetResponse()
        gr.ParseFromString(stub.Invoke(invoke_pb2.InvokeRequest(
            capability="rat://state/v1/get", payload=state_pb2.GetRequest(key=k).SerializeToString()), metadata=md).result)
        out.append({"key": k, **json.loads(gr.value.decode() or "{}")})
    return out


def _trigger():
    stub, md = _stub(), _md()
    req = strategy_pb2.ApplyRequest(target=data_pb2.TableRef(identifier=TARGET), idempotency_key="bff-trigger")
    r = stub.Invoke(invoke_pb2.InvokeRequest(capability="rat://strategy/v1/apply", payload=req.SerializeToString()), metadata=md)
    resp = strategy_pb2.ApplyResponse()
    resp.ParseFromString(r.result)
    return {"rows_affected": resp.result.rows_affected, "snapshot": resp.result.snapshot_id, "already_applied": resp.result.already_applied}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):  # quiet
        pass

    def _send(self, code, payload):
        body = json.dumps(payload, default=str).encode()  # default=str: dates/decimals
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        u = urlparse(self.path)
        try:
            if u.path == "/api/health":
                self._send(200, {"ok": True, "gateway": GATEWAY})
            elif u.path == "/api/runs":
                self._send(200, {"runs": _runs()})
            elif u.path == "/api/tables":  # the medallion tables in the shared lake (Q2)
                self._send(200, {"tables": _tables()})
            elif u.path.startswith("/api/table/"):  # rows of one table (the data leg)
                name = u.path[len("/api/table/"):]
                limit = int(parse_qs(u.query).get("limit", ["100"])[0])
                self._send(200, _table(name, limit))
            else:
                self._send(404, {"error": "not found"})
        except grpc.RpcError as e:
            self._send(502, {"error": f"{e.code()}: {e.details()}"})
        except Exception as e:  # lake read errors (bad table, lake down)
            self._send(500, {"error": f"{type(e).__name__}: {e}"})

    def do_POST(self):
        try:
            if self.path == "/api/run":
                self._send(200, _trigger())
            else:
                self._send(404, {"error": "not found"})
        except grpc.RpcError as e:
            self._send(502, {"error": f"{e.code()}: {e.details()}"})


def main():
    # When rat LAUNCHES the bff (ADR-022) it sets RAT_PLUGIN_ADDR; serving the HTTP API on
    # that port lets the deployment-runtime's readiness check pass (a TCP connect succeeds).
    addr = os.environ.get("RAT_PLUGIN_ADDR") or os.environ.get("BFF_ADDR", "0.0.0.0:8080")
    host, _, port = addr.rpartition(":")
    print(f"platform-bff on {addr} → gateway {GATEWAY}", flush=True)
    ThreadingHTTPServer((host, int(port)), Handler).serve_forever()


if __name__ == "__main__":
    main()
