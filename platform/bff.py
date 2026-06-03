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
from google.protobuf import json_format, symbol_database

from rat.common.v1 import annotations_pb2, context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.secret.v1 import secret_pb2
from rat.state.v1 import state_pb2
from rat.strategy.v1 import strategy_pb2

# Import a broad axis set so ANY contributed command's capability resolves generically
# (ADR-024 /api/invoke). New axes here = the bff can drive their capabilities too.
_AXIS_MODULES = []
for _m in ("strategy", "state", "secret", "engine", "catalog", "storage", "format"):
    try:
        _AXIS_MODULES.append(__import__(f"rat.{_m}.v1.{_m}_pb2", fromlist=[""]))
    except Exception:
        pass

# capability URI -> (input message descriptor, output message descriptor)
_CAP_INDEX = {}
for _mod in _AXIS_MODULES:
    for _svc in _mod.DESCRIPTOR.services_by_name.values():
        for _meth in _svc.methods:
            _cap = _meth.GetOptions().Extensions[annotations_pb2.capability]
            if _cap:
                _CAP_INDEX[_cap] = (_meth.input_type, _meth.output_type)

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
    return _invoke("rat://strategy/v1/apply", {"target": {"identifier": TARGET}, "idempotencyKey": "bff-trigger"})


# --- generic capability invoke (ADR-024): any contributed command routes through here ---
def _invoke(capability, data):
    """Invoke any capability through the gateway (C5-authorized + audited), building the
    request from JSON via the resolved input type and returning the response as a dict.
    This is the one path every contributed command fires."""
    if capability not in _CAP_INDEX:
        raise ValueError(f"unknown capability {capability!r}")
    in_desc, out_desc = _CAP_INDEX[capability]
    sym = symbol_database.Default()
    req = sym.GetSymbol(in_desc.full_name)()
    json_format.ParseDict(data or {}, req)
    stub, md = _stub(), _md()
    r = stub.Invoke(invoke_pb2.InvokeRequest(capability=capability, payload=req.SerializeToString()), metadata=md)
    resp = sym.GetSymbol(out_desc.full_name)()
    resp.ParseFromString(r.result)
    return json_format.MessageToDict(resp)


# --- the UI is ASSEMBLED from plugin contributions (ADR-024) ---
# A contribution is a JSON component spec at state key ui/components/<plugin>/<id>. The bff
# aggregates them by slot; the generic shell renders. Adding UI = publishing a contribution
# (any plugin via state/put); the bff + shell never change. The platform seeds its own.
PLATFORM_COMPONENTS = [
    {"slot": "explorer", "id": "lake-tables", "title": "Lake Tables", "icon": "database",
     "data": "/api/tables", "item": "/api/table/"},
    {"slot": "explorer", "id": "run-history", "title": "Run History", "icon": "history", "data": "/api/runs"},
    {"slot": "command", "id": "run-pipeline", "title": "Run pipeline", "icon": "play",
     "capability": "rat://strategy/v1/apply",
     "args": {"target": {"identifier": TARGET}, "idempotencyKey": "ui-run"}},
]


def _state_list(prefix):
    stub, md = _stub(), _md()
    lr = state_pb2.ListResponse()
    lr.ParseFromString(stub.Invoke(invoke_pb2.InvokeRequest(
        capability="rat://state/v1/list", payload=state_pb2.ListRequest(prefix=prefix).SerializeToString()), metadata=md).result)
    return list(lr.keys)


def _state_get(key):
    stub, md = _stub(), _md()
    gr = state_pb2.GetResponse()
    gr.ParseFromString(stub.Invoke(invoke_pb2.InvokeRequest(
        capability="rat://state/v1/get", payload=state_pb2.GetRequest(key=key).SerializeToString()), metadata=md).result)
    return gr.value if gr.found else None


def _state_put(key, value):
    stub, md = _stub(), _md()
    stub.Invoke(invoke_pb2.InvokeRequest(
        capability="rat://state/v1/put", payload=state_pb2.PutRequest(key=key, value=value).SerializeToString()), metadata=md)


def _seed_platform_ui():
    for c in PLATFORM_COMPONENTS:
        _state_put(f"ui/components/platform-bff/{c['id']}", json.dumps(c).encode())


def _ui():
    """Aggregate every published contribution into a slot-grouped UI descriptor (ADR-024).
    The bff hardcodes no view — it seeds the PLATFORM's own components once, then renders
    whatever any plugin has published under ui/components/."""
    if not _state_list("ui/components/platform-bff/"):
        _seed_platform_ui()
    slots = {}
    for key in _state_list("ui/components/"):
        raw = _state_get(key)
        if not raw:
            continue
        comp = json.loads(raw.decode())
        comp["_source"] = key.split("/")[2] if key.count("/") >= 2 else "?"  # the contributing plugin
        slots.setdefault(comp.get("slot", "explorer"), []).append(comp)
    return {"slots": slots}


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
            elif u.path == "/api/ui":  # the assembled UI: every plugin's contributions (ADR-024)
                self._send(200, _ui())
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
            elif self.path == "/api/invoke":  # the generic action path for contributed commands
                n = int(self.headers.get("Content-Length", 0))
                body = json.loads(self.rfile.read(n) or b"{}")
                self._send(200, _invoke(body["capability"], body.get("data", {})))
            else:
                self._send(404, {"error": "not found"})
        except grpc.RpcError as e:
            self._send(502, {"error": f"{e.code()}: {e.details()}"})
        except Exception as e:
            self._send(500, {"error": f"{type(e).__name__}: {e}"})


def main():
    # When rat LAUNCHES the bff (ADR-022) it sets RAT_PLUGIN_ADDR; serving the HTTP API on
    # that port lets the deployment-runtime's readiness check pass (a TCP connect succeeds).
    addr = os.environ.get("RAT_PLUGIN_ADDR") or os.environ.get("BFF_ADDR", "0.0.0.0:8080")
    host, _, port = addr.rpartition(":")
    print(f"platform-bff on {addr} → gateway {GATEWAY}", flush=True)
    ThreadingHTTPServer((host, int(port)), Handler).serve_forever()


if __name__ == "__main__":
    main()
