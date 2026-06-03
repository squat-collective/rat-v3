"""rat-scheduler — the always-on platform's self-driving clock.

Every $RAT_SCHEDULE_INTERVAL seconds it invokes the pipeline (`rat://strategy/v1/apply`)
through the core gateway — so the platform refreshes on its own, with nobody running a
command. The v2 `ratd` scheduler tick, decoupled into its own plugin that drives the
gateway.

> 🛰️ EXPLORATORY (the data platform bundle, ADR-020 S2b). This is a MINIMAL active
> trigger (a clock that fires a capability on an interval). The full scheduler-backend
> axis — `rat://scheduler/v1/{schedule,cancel,watch-due}`, a clock the orchestrator
> WATCHES — is the richer form (a clock separated from the actor); this driver folds
> clock + actor for the solo platform.

Env:
  RAT_GATEWAY            the core gateway to fire the pipeline through
  RAT_SCHEDULE_INTERVAL  seconds between fires (default 60; the platform demo uses less)
  RAT_PIPELINE_TARGET    the pipeline's target table (default gold_daily_revenue)
  RAT_PLUGIN_NAME        the caller identity (default rat-scheduler; must `requires` apply)
"""

import json
import os
import socket
import sys
import threading
import time

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.state.v1 import state_pb2
from rat.strategy.v1 import strategy_pb2


def _serve_health(addr):
    """A trivial TCP listener on $RAT_PLUGIN_ADDR so the deployment-runtime's readiness
    check passes — the scheduler is a driver (it calls the gateway; it serves no capability),
    but rat launches+supervises it like any plugin (ADR-022), and that needs a port to poke."""
    host, _, port = addr.rpartition(":")
    s = socket.socket()
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("" if host in ("0.0.0.0", "") else host, int(port)))
    s.listen(16)
    while True:
        try:
            conn, _ = s.accept()
            conn.close()  # drain each readiness probe
        except OSError:
            return


def _record_run(stub, md, tick, status, snapshot, error):
    """Record a run record to the state-backend (rat://state/v1/put) — the platform's
    run history, exactly v2's `runs` table, now a state-backend plugin behind the gateway."""
    rec = json.dumps({"tick": tick, "status": status, "snapshot": snapshot, "error": error}).encode()
    try:
        stub.Invoke(invoke_pb2.InvokeRequest(capability="rat://state/v1/put",
                    payload=state_pb2.PutRequest(key=f"runs/{tick:06d}", value=rec).SerializeToString()), metadata=md)
    except grpc.RpcError as e:
        print(f"rat-scheduler: tick {tick} → could not record run: {e.code()}", flush=True)


def main():
    gateway = os.environ["RAT_GATEWAY"]
    interval = float(os.environ.get("RAT_SCHEDULE_INTERVAL", "60"))
    target = os.environ.get("RAT_PIPELINE_TARGET", "gold_daily_revenue")
    caller = os.environ.get("RAT_PLUGIN_NAME", "rat-scheduler")

    # readiness port so rat can launch + supervise this driver (ADR-022)
    threading.Thread(target=_serve_health, args=(os.environ.get("RAT_PLUGIN_ADDR", "0.0.0.0:50051"),), daemon=True).start()

    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(traceparent="00-" + "c" * 32 + "-" + "d" * 16 + "-01", correlation_id="scheduler"),
        identity=context_pb2.Identity(caller_plugin=caller, tenant="acme"))
    md = [("rat-callmeta-bin", rc.SerializeToString())]
    stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(gateway))

    print(f"rat-scheduler: firing rat://strategy/v1/apply every {interval:g}s → {gateway}", flush=True)
    tick = 0
    while True:
        tick += 1
        status, snapshot, error = "success", "", ""
        req = strategy_pb2.ApplyRequest(target=data_pb2.TableRef(identifier=target), idempotency_key=f"sched-{tick}")
        try:
            r = stub.Invoke(invoke_pb2.InvokeRequest(capability="rat://strategy/v1/apply", payload=req.SerializeToString()), metadata=md)
            resp = strategy_pb2.ApplyResponse()
            resp.ParseFromString(r.result)
            snapshot = resp.result.snapshot_id
            print(f"rat-scheduler: tick {tick} → refreshed (rows={resp.result.rows_affected} snapshot={snapshot!r})", flush=True)
        except grpc.RpcError as e:
            status, error = "failed", f"{e.code()}: {e.details()}"
            print(f"rat-scheduler: tick {tick} → error: {error}", flush=True)
        _record_run(stub, md, tick, status, snapshot, error)  # the run history → state-backend
        time.sleep(interval)


if __name__ == "__main__":
    sys.exit(main())
