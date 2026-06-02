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
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.state.v1 import state_pb2
from rat.strategy.v1 import strategy_pb2

GATEWAY = os.environ.get("RAT_GATEWAY", "127.0.0.1:7777")
CALLER = os.environ.get("RAT_PLUGIN_NAME", "platform-bff")
TARGET = os.environ.get("RAT_PIPELINE_TARGET", "gold_daily_revenue")


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
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        try:
            if self.path == "/api/health":
                self._send(200, {"ok": True, "gateway": GATEWAY})
            elif self.path == "/api/runs":
                self._send(200, {"runs": _runs()})
            else:
                self._send(404, {"error": "not found"})
        except grpc.RpcError as e:
            self._send(502, {"error": f"{e.code()}: {e.details()}"})

    def do_POST(self):
        try:
            if self.path == "/api/run":
                self._send(200, _trigger())
            else:
                self._send(404, {"error": "not found"})
        except grpc.RpcError as e:
            self._send(502, {"error": f"{e.code()}: {e.details()}"})


def main():
    host, port = os.environ.get("BFF_ADDR", "0.0.0.0:8080").split(":")
    print(f"platform-bff on {host}:{port} → gateway {GATEWAY}", flush=True)
    ThreadingHTTPServer((host, int(port)), Handler).serve_forever()


if __name__ == "__main__":
    main()
