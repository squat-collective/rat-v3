"""The dbt-runner backend — a `pipeline-runner` that executes a dbt project (ADR-021).

EXPLORATORY (the data platform bundle). It reuses the frozen strategy/v1 axis for the
experiment (provides `rat://strategy/v1/apply`); a dedicated `rat://pipeline/v1/run`
axis is the formalization. On Apply it runs `dbt build` on a dbt project — dbt owns the
DAG, ref(), Jinja, materializations AND tests, so rat reinvents none of it. A failed dbt
build (a model error OR a failing test) surfaces as FAILED_PRECONDITION — the quality
gate, native to dbt.
"""

import os
import subprocess

import grpc

from rat.common.v1 import data_pb2
from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc


class DbtRunner:
    def __init__(self) -> None:
        self.project = os.environ["RAT_DBT_PROJECT"]
        self.profiles = os.environ.get("RAT_DBT_PROFILES", self.project)

    def apply(self, idem: str) -> data_pb2.WriteResult:
        # dbt runs as a subprocess from its OWN venv ($RAT_DBT_BIN): dbt-core pins an
        # older protobuf than the RAT gRPC SDK (7.35), so the two can't share one env —
        # isolating dbt behind a binary boundary is the clean fix.
        dbt = os.environ.get("RAT_DBT_BIN", "dbt")
        proc = subprocess.run(
            [dbt, "build",
             "--project-dir", self.project,
             "--profiles-dir", self.profiles,
             "--target-path", "/tmp/dbt-target",
             "--log-path", "/tmp/dbt-logs"],
            capture_output=True, text=True)
        print((proc.stdout or "")[-4000:], flush=True)
        if proc.stderr:
            print((proc.stderr)[-1000:], flush=True)
        if proc.returncode != 0:
            raise RuntimeError(f"dbt build exited {proc.returncode}")
        return data_pb2.WriteResult(rows_affected=0, snapshot_id=idem)


class StrategyServicer(strategy_pb2_grpc.StrategyServiceServicer):
    def __init__(self, runner: DbtRunner) -> None:
        self._runner = runner

    def Apply(self, request, context):
        try:
            result = self._runner.apply(request.idempotency_key or "dbt-run")
        except RuntimeError as e:  # a model error or a failing dbt test → quality gate
            context.abort(grpc.StatusCode.FAILED_PRECONDITION, f"dbt build failed: {e}")
        return strategy_pb2.ApplyResponse(result=result)
