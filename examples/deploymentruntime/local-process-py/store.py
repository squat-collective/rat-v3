"""Local-process deployment-runtime for rat-deploymentruntime-local-process-py.

WHERE PLUGINS RUN (deployment_runtime.proto) — the simplest runtime: each plugin
instance is a child OS process on this host. It is one of two divergent ADR-003
references (the other, k8s-dryrun-py, maps the same spec to a Pod securityContext).

The load-bearing obligation is the I9 isolation profile: a conformant runtime MUST
honor it, so this runtime REFUSES to launch a plugin whose isolation is below the I9
minimum (run_as_non_root + drop_all_capabilities + no_new_privileges). A local process
can genuinely apply only a subset of the kernel-level controls (it asserts the gate +
records the committed profile; full cap-drop / read-only-fs enforcement needs a
container runtime — that's what the k8s reference demonstrates). The trust GATE,
however, is real and identical across runtimes.
"""

import itertools
import json
import subprocess
import sys
import threading

import grpc


class DeploymentError(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code
        self.message = message


def honored(iso):
    """The I9 isolation receipt the runtime commits to honoring (conformance
    stand-in, like storage's scope receipt)."""
    return {
        "run_as_non_root": iso.run_as_non_root,
        "drop_all_capabilities": iso.drop_all_capabilities,
        "no_new_privileges": iso.no_new_privileges,
        "read_only_root_fs": iso.read_only_root_fs,
        "block_metadata_egress": iso.block_metadata_egress,
    }


def check_spec(spec):
    """Shared validation: image required; isolation MUST meet the I9 minimum."""
    if not spec.image:
        raise DeploymentError(grpc.StatusCode.INVALID_ARGUMENT, "spec.image is required")
    iso = spec.isolation
    if not (iso.run_as_non_root and iso.drop_all_capabilities and iso.no_new_privileges):
        raise DeploymentError(
            grpc.StatusCode.FAILED_PRECONDITION,
            "isolation below the I9 minimum (run_as_non_root + drop_all_capabilities + no_new_privileges)",
        )


class LocalProcessRuntime:
    KIND = "local-process"

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._instances = {}  # instance_id -> {"proc", "endpoint", "iso"}
        self._ids = itertools.count(1)
        self._ports = itertools.count(40000)

    def launch(self, plugin_id, spec):
        check_spec(spec)
        # A real plugin process would be the gRPC server from `spec.image`; here a
        # placeholder child stands in so the lifecycle (alive → terminate → gone) is real.
        proc = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(300)"])
        with self._lock:
            iid = f"proc-{next(self._ids)}"
            endpoint = f"127.0.0.1:{next(self._ports)}"
            self._instances[iid] = {"proc": proc, "endpoint": endpoint, "iso": honored(spec.isolation)}
        return iid, endpoint

    def healthcheck(self, instance_id):
        with self._lock:
            inst = self._instances.get(instance_id)
        if inst is None:
            return "UNKNOWN", json.dumps({"kind": self.KIND, "found": False})
        alive = inst["proc"].poll() is None
        detail = json.dumps({"kind": self.KIND, "isolation_honored": inst["iso"]})
        return ("HEALTHY" if alive else "UNHEALTHY"), detail

    def terminate(self, instance_id):
        with self._lock:
            inst = self._instances.pop(instance_id, None)
        if inst is None:
            return False
        inst["proc"].terminate()
        try:
            inst["proc"].wait(timeout=5)
        except subprocess.TimeoutExpired:
            inst["proc"].kill()
        return True
