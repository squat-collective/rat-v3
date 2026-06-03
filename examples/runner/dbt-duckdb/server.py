"""The dbt-runner backend — a `pipeline-runner` that executes a dbt project (ADR-021).

EXPLORATORY (the data platform bundle). It reuses the frozen strategy/v1 axis for the
experiment (provides `rat://strategy/v1/apply`); a dedicated `rat://pipeline/v1/run`
axis is the formalization. On Apply it runs `dbt build` on a dbt project — dbt owns the
DAG, ref(), Jinja, materializations AND tests, so rat reinvents none of it. A failed dbt
build (a model error OR a failing test) surfaces as FAILED_PRECONDITION — the quality
gate, native to dbt.

PROJECT DELIVERY (ADR-021 "your pipeline is code you submit"): the dbt project is not
baked — it is whatever was `rat apply`'d. When $RAT_PROJECT_KEY is set, on each run this
fetches the project tarball from the state-backend (rat://state/v1/get, via the gateway —
C5-authorized + audited), extracts it (re-extracting only when the stored revision
changed), and runs YOUR submitted code. A baked sample project is the fallback when
nothing has been applied yet.
"""

import io
import os
import shutil
import subprocess
import tarfile
import threading
import time

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.core.v1 import invoke_pb2, invoke_pb2_grpc
from rat.secret.v1 import secret_pb2
from rat.state.v1 import state_pb2
from rat.strategy.v1 import strategy_pb2, strategy_pb2_grpc


class DbtRunner:
    def __init__(self) -> None:
        # the baked sample project — the fallback before anything is applied
        self.baked_project = os.environ["RAT_DBT_PROJECT"]
        self.baked_profiles = os.environ.get("RAT_DBT_PROFILES", self.baked_project)
        # applied-project delivery (rat apply -> state-backend)
        self.project_key = os.environ.get("RAT_PROJECT_KEY")  # e.g. projects/medallion
        self.gateway = os.environ.get("RAT_GATEWAY", "127.0.0.1:7777")
        self.caller = os.environ.get("RAT_PLUGIN_NAME", "rat-pipeline")
        self.tenant = os.environ.get("RAT_TENANT", "acme")
        self.extract_root = "/tmp/rat-applied-project"
        self._lock = threading.Lock()
        self._applied_rev = None  # the state revision currently extracted (re-extract on change)
        self._secret_env = None   # resolved *_REF env (cached after the first apply)

    def _md(self):
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(traceparent="00-" + "a" * 32 + "-" + "c" * 16 + "-01", correlation_id="dbt-runner"),
            identity=context_pb2.Identity(caller_plugin=self.caller, tenant=self.tenant))
        return [("rat-callmeta-bin", rc.SerializeToString())]

    def _resolve(self, ref):
        """Resolve a secret ref (ref://…) to its value via the gateway's secret-backend
        (rat://secret/v1/resolve — C5-authorized + audited). Retries while the secret plugin
        finishes wiring at boot."""
        stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(self.gateway))
        last = "unknown"
        for _ in range(60):
            try:
                r = stub.Invoke(invoke_pb2.InvokeRequest(
                    capability="rat://secret/v1/resolve",
                    payload=secret_pb2.ResolveRequest(secret_ref=ref).SerializeToString()), metadata=self._md())
                resp = secret_pb2.ResolveResponse()
                resp.ParseFromString(r.result)
                if resp.found:
                    return resp.value.decode("utf-8")
                last = f"secret {ref!r} not found (absent or not authorized)"
            except grpc.RpcError as e:
                last = f"{e.code()}: {e.details()}"
            time.sleep(1)
        raise RuntimeError(f"could not resolve {ref!r}: {last}")

    def _resolved_secret_env(self):
        """Every `<NAME>_REF=ref://…` env var becomes `<NAME>=<resolved value>` for the dbt
        subprocess — so the dbt profile reads plain env_var('<NAME>') while the credential
        lives only on the secret plugin (no creds in plugins.yaml). Resolved once, cached."""
        if self._secret_env is None:
            extra = {}
            for k, v in os.environ.items():
                if k.endswith("_REF") and v.startswith("ref://"):
                    extra[k[:-4]] = self._resolve(v)  # strip the _REF suffix
            self._secret_env = extra
        return self._secret_env

    def _applied_project(self):
        """The project dir to run: the `rat apply`'d project from the state-backend, or None
        to use the baked fallback. Re-extracts only when the stored revision changed."""
        if not self.project_key:
            return None
        stub = invoke_pb2_grpc.CapabilityInvokeServiceStub(grpc.insecure_channel(self.gateway))
        try:
            r = stub.Invoke(invoke_pb2.InvokeRequest(
                capability="rat://state/v1/get",
                payload=state_pb2.GetRequest(key=self.project_key).SerializeToString()), metadata=self._md())
        except grpc.RpcError as e:
            print(f"rat-pipeline: could not fetch {self.project_key!r}: {e.code()} — using baked project", flush=True)
            return None
        gr = state_pb2.GetResponse()
        gr.ParseFromString(r.result)
        if not gr.found:
            return None  # nothing applied yet → baked fallback
        with self._lock:
            if self._applied_rev == gr.revision and os.path.isdir(self.extract_root):
                return self.extract_root  # unchanged since last extract — reuse
            shutil.rmtree(self.extract_root, ignore_errors=True)
            os.makedirs(self.extract_root, exist_ok=True)
            with tarfile.open(fileobj=io.BytesIO(gr.value), mode="r:gz") as tar:
                tar.extractall(self.extract_root, filter="data")  # py3.12 safe extraction
            self._applied_rev = gr.revision
            print(f"rat-pipeline: extracted applied project {self.project_key!r} rev {gr.revision} → {self.extract_root}", flush=True)
            return self.extract_root

    def apply(self, idem: str) -> data_pb2.WriteResult:
        applied = self._applied_project()
        if applied:
            project, profiles = applied, applied
        else:
            project, profiles = self.baked_project, self.baked_profiles
        # dbt runs as a subprocess from its OWN venv ($RAT_DBT_BIN): dbt-core pins an
        # older protobuf than the RAT gRPC SDK (7.35), so the two can't share one env —
        # isolating dbt behind a binary boundary is the clean fix.
        dbt = os.environ.get("RAT_DBT_BIN", "dbt")
        # the dbt profile reads the lake creds via env_var(); resolve any *_REF from the
        # secret plugin into the subprocess env so no credential lives in plugins.yaml.
        env = {**os.environ, **self._resolved_secret_env()}
        proc = subprocess.run(
            [dbt, "build",
             "--project-dir", project,
             "--profiles-dir", profiles,
             "--target-path", "/tmp/dbt-target",
             "--log-path", "/tmp/dbt-logs"],
            capture_output=True, text=True, env=env)
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
