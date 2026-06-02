"""The sql-pipeline strategy backend — a REAL multi-model pipeline `kind: strategy`.

> 🛰️ EXPLORATORY (the data platform bundle, ADR-020 S2). Additive; the frozen
> strategy/v1 surface is unchanged.

Like every strategy it couples to NO concrete plugin: its only dependency is an
`invoke(capability, request, response_cls)` seam (the core capability-invoke gateway,
ADR-005). On Apply it runs an ORDERED LIST of SQL models (a medallion: bronze → silver
→ gold) by issuing `rat://engine/v1/execute` for each through the gateway, flushes the
lake, and commits the target table's snapshot via `rat://catalog/v1/commit-table` — the
v2 "runner" decoupled into a capability the scheduler (or any caller) can invoke.

Config from env (the platform sets these):
  RAT_GATEWAY        the core gateway addr to call engine/catalog back through
  RAT_PROJECT        the model dir (default /work/platform/project)
  RAT_LANDING        landing dir AS THE ENGINE SEES IT (substituted for ${LANDING})
  RAT_MODELS         comma-separated model paths, in order (relative to RAT_PROJECT)
  RAT_PIPELINE_TARGET the table to register+commit (default gold_daily_revenue)
  RAT_DUCKLAKE_ALIAS the lake alias (default lake)
"""

import os
import pathlib

import grpc

from rat.catalog.v1 import catalog_pb2
from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.engine.v1 import engine_pb2
from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc

CALLER = os.environ.get("RAT_PLUGIN_NAME", "rat-sql-pipeline")


class QualityFailure(Exception):
    """A data-quality test returned violation rows — the run is gated (not committed)."""


class GatewayInvoke:
    """invoke(capability, request, response_cls) -> response — over the core gateway,
    carrying this plugin's identity (C5) + a traceparent (C1)."""

    def __init__(self, gateway_addr, caller=CALLER, tenant="acme"):
        self._stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(gateway_addr))
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(traceparent="00-" + "a" * 32 + "-" + "b" * 16 + "-01", correlation_id="pipeline"),
            identity=context_pb2.Identity(caller_plugin=caller, tenant=tenant))
        self._md = [("rat-callmeta-bin", rc.SerializeToString())]

    def __call__(self, cap, req, resp_cls):
        r = self._stub.Invoke(invoke_pb2.InvokeRequest(capability=cap, payload=req.SerializeToString()), metadata=self._md)
        out = resp_cls()
        out.ParseFromString(r.result)
        return out


class PipelineStrategy:
    def __init__(self):
        self._gateway = os.environ["RAT_GATEWAY"]
        self._project = pathlib.Path(os.environ.get("RAT_PROJECT", "/work/platform/project"))
        self._landing = os.environ.get("RAT_LANDING", "/work/platform/landing")
        self._models = [m.strip() for m in os.environ.get("RAT_MODELS", "").split(",") if m.strip()]
        self._target = os.environ.get("RAT_PIPELINE_TARGET", "gold_daily_revenue")
        self._alias = os.environ.get("RAT_DUCKLAKE_ALIAS", "lake")

    def apply(self, target_id: str, idem: str) -> data_pb2.WriteResult:
        target = target_id or self._target
        invoke = GatewayInvoke(self._gateway)  # dial lazily — the gateway is up by Apply time

        def execute(sql):
            return invoke("rat://engine/v1/execute", engine_pb2.ExecuteRequest(sql=sql), engine_pb2.ExecuteResponse).result

        rows = 0
        for m in self._models:
            sql = (self._project / m).read_text().replace("${LANDING}", self._landing)
            rows = execute(sql).rows_affected  # the last model's rows (the target build)

        snap = execute(f"CALL ducklake_flush_inlined_data('{self._alias}')").snapshot_id

        # QUALITY GATE (v2's "tests block the merge", on DuckLake): run project/tests/*.sql;
        # a test that returns rows is a violation. On any failure the snapshot is NOT
        # committed to the catalog — the published pointer stays at the last good snapshot.
        failures = self._run_quality(execute)
        if failures:
            raise QualityFailure("; ".join(failures))

        invoke("rat://catalog/v1/register-table", catalog_pb2.RegisterTableRequest(identifier=target), catalog_pb2.RegisterTableResponse)
        commit = invoke("rat://catalog/v1/commit-table",
                        catalog_pb2.CommitTableRequest(identifier=target, snapshot_id=snap, idempotency_key=idem),
                        catalog_pb2.CommitTableResponse)
        return data_pb2.WriteResult(rows_affected=rows, snapshot_id=snap, already_applied=commit.already_applied)

    def _run_quality(self, execute) -> list:
        """Run each project/tests/*.sql as a CTAS — rows_affected IS the violation count
        (so no Arrow row-pull is needed, sidestepping the in-proc data leg, F9). Returns
        the list of failures ("<test>: N violation(s)"); empty == all passed."""
        tests_dir = self._project / "tests"
        if not tests_dir.is_dir():
            return []
        failures = []
        for t in sorted(tests_dir.glob("*.sql")):
            body = "\n".join(l for l in t.read_text().splitlines() if not l.strip().startswith("--")).strip().rstrip(";")
            if not body:
                continue
            violations = execute(f"CREATE OR REPLACE TEMP TABLE _rat_qt AS\n{body}").rows_affected
            status = "FAIL" if violations else "pass"
            print(f"rat-pipeline: quality {t.name}: {status} ({violations} violation(s))", flush=True)
            if violations > 0:
                failures.append(f"{t.name}: {violations} violation(s)")
        return failures


class StrategyServicer(strategy_pb2_grpc.StrategyServiceServicer):
    def __init__(self, strategy: PipelineStrategy) -> None:
        self._strategy = strategy

    def Apply(self, request, context):
        try:
            result = self._strategy.apply(request.target.identifier, request.idempotency_key or "pipeline-run")
        except QualityFailure as e:  # the quality gate blocked the commit (FAILED_PRECONDITION)
            context.abort(grpc.StatusCode.FAILED_PRECONDITION, f"quality gate failed: {e}")
        except Exception as e:  # any other pipeline error
            context.abort(grpc.StatusCode.INTERNAL, f"pipeline failed: {e}")
        return strategy_pb2.ApplyResponse(result=result)
