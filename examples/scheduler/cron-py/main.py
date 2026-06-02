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

import os
import sys
import time

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.strategy.v1 import strategy_pb2


def main():
    gateway = os.environ["RAT_GATEWAY"]
    interval = float(os.environ.get("RAT_SCHEDULE_INTERVAL", "60"))
    target = os.environ.get("RAT_PIPELINE_TARGET", "gold_daily_revenue")
    caller = os.environ.get("RAT_PLUGIN_NAME", "rat-scheduler")

    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(traceparent="00-" + "c" * 32 + "-" + "d" * 16 + "-01", correlation_id="scheduler"),
        identity=context_pb2.Identity(caller_plugin=caller, tenant="acme"))
    md = [("rat-callmeta-bin", rc.SerializeToString())]
    stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(gateway))

    print(f"rat-scheduler: firing rat://strategy/v1/apply every {interval:g}s → {gateway}", flush=True)
    tick = 0
    while True:
        tick += 1
        req = strategy_pb2.ApplyRequest(target=data_pb2.TableRef(identifier=target), idempotency_key=f"sched-{tick}")
        try:
            r = stub.Invoke(invoke_pb2.InvokeRequest(capability="rat://strategy/v1/apply", payload=req.SerializeToString()), metadata=md)
            resp = strategy_pb2.ApplyResponse()
            resp.ParseFromString(r.result)
            print(f"rat-scheduler: tick {tick} → refreshed (rows={resp.result.rows_affected} snapshot={resp.result.snapshot_id!r})", flush=True)
        except grpc.RpcError as e:
            print(f"rat-scheduler: tick {tick} → error: {e.code()} {e.details()}", flush=True)
        time.sleep(interval)


if __name__ == "__main__":
    sys.exit(main())
