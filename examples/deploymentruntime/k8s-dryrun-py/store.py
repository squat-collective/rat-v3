"""K8s (dry-run) deployment-runtime for rat-deploymentruntime-k8s-dryrun-py.

The divergent second ADR-003 reference (paired with local-process-py): instead of
forking a local process, it models a MANAGED/declarative runtime — it translates the
LaunchSpec (and crucially the I9 IsolationProfile) into a Kubernetes Pod manifest's
`securityContext` and records it. "Launch" is producing + admitting that manifest, not
a local spawn (dry-run: no cluster needed). This is where the IsolationProfile gets a
real, inspectable enforcement target — the proto's "honor the profile" obligation maps
1:1 to securityContext fields a container runtime actually enforces.

It shares the exact I9 trust gate with the local-process reference (check_spec) and the
same isolation-honored receipt shape, so both pass the identical golden vectors —
proving the contract holds across genuinely different runtime technologies (local fork
vs managed/container), which is the ADR-003 cross-implementation point.
"""

import itertools
import json
import threading

import grpc


class DeploymentError(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code
        self.message = message


def honored(iso):
    return {
        "run_as_non_root": iso.run_as_non_root,
        "drop_all_capabilities": iso.drop_all_capabilities,
        "no_new_privileges": iso.no_new_privileges,
        "read_only_root_fs": iso.read_only_root_fs,
        "block_metadata_egress": iso.block_metadata_egress,
    }


def check_spec(spec):
    """The SHARED I9 gate (identical to local-process): image required; isolation MUST
    meet the I9 minimum."""
    if not spec.image:
        raise DeploymentError(grpc.StatusCode.INVALID_ARGUMENT, "spec.image is required")
    iso = spec.isolation
    if not (iso.run_as_non_root and iso.drop_all_capabilities and iso.no_new_privileges):
        raise DeploymentError(
            grpc.StatusCode.FAILED_PRECONDITION,
            "isolation below the I9 minimum (run_as_non_root + drop_all_capabilities + no_new_privileges)",
        )


def _security_context(iso):
    """Map the RAT IsolationProfile onto a k8s Pod securityContext — the real
    enforcement surface a container runtime applies."""
    return {
        "runAsNonRoot": iso.run_as_non_root,
        "allowPrivilegeEscalation": not iso.no_new_privileges,
        "readOnlyRootFilesystem": iso.read_only_root_fs,
        "capabilities": {"drop": ["ALL"] if iso.drop_all_capabilities else []},
        "seccompProfile": {"type": iso.seccomp_profile or "RuntimeDefault"},
    }


class K8sDryRunRuntime:
    KIND = "k8s-dryrun"

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._pods = {}  # instance_id -> {"manifest", "endpoint", "iso"}
        self._ids = itertools.count(1)

    def _manifest(self, plugin_id, spec):
        return {
            "apiVersion": "v1",
            "kind": "Pod",
            "metadata": {"name": f"{plugin_id}-{next(self._ids)}", "labels": {"rat.dev/plugin": plugin_id}},
            "spec": {
                "automountServiceAccountToken": False,
                "securityContext": _security_context(spec.isolation),
                "containers": [{
                    "name": "plugin",
                    "image": spec.image,
                    "env": [{"name": k, "value": v} for k, v in spec.env.items()],
                    "resources": {
                        "requests": {"cpu": spec.requests.cpu, "memory": spec.requests.memory},
                        "limits": {"cpu": spec.limits.cpu, "memory": spec.limits.memory},
                    },
                }],
            },
        }

    def launch(self, plugin_id, spec):
        check_spec(spec)
        with self._lock:
            manifest = self._manifest(plugin_id, spec)
            name = manifest["metadata"]["name"]
            iid = f"pod/{name}"
            endpoint = f"{name}.rat.svc.cluster.local:50051"
            self._pods[iid] = {"manifest": manifest, "endpoint": endpoint, "iso": honored(spec.isolation)}
        return iid, endpoint

    def healthcheck(self, instance_id):
        with self._lock:
            pod = self._pods.get(instance_id)
        if pod is None:
            return "UNKNOWN", json.dumps({"kind": self.KIND, "found": False})
        # Dry-run: an admitted Pod is reported Running; a real runtime would query the
        # API for the Pod phase. The securityContext-derived receipt is exposed for C9.
        detail = json.dumps({"kind": self.KIND, "isolation_honored": pod["iso"],
                             "security_context": pod["manifest"]["spec"]["securityContext"]})
        return "HEALTHY", detail

    def terminate(self, instance_id):
        with self._lock:
            return self._pods.pop(instance_id, None) is not None
