"""Conformance harness for the deployment-runtime/v1 axis (shared by both ADR-003
references — local-process-py + k8s-dryrun-py; each imports its own `server`).

Loads contracts/conformance/deploymentruntime-v1.json and drives DeploymentRuntime
over real gRPC: Launch (full I9 profile) → assert an instance_id + endpoint come back;
Healthcheck → HEALTHY + the isolation-honored receipt matches the requested profile
(the I9 obligation, observable); Terminate → terminated; Healthcheck → no longer
HEALTHY. The error vectors assert the trust gate: a below-I9-minimum isolation is
refused (FAILED_PRECONDITION) and an empty image is INVALID_ARGUMENT.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.deploymentruntime.v1 import deployment_runtime_pb2 as pb
from rat.deploymentruntime.v1 import deployment_runtime_pb2_grpc as pb_grpc

from server import DeploymentRuntimeServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "deploymentruntime-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "FAILED_PRECONDITION": grpc.StatusCode.FAILED_PRECONDITION,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "deployment-runtime/v1", f'vectors axis = {v["axis"]!r}'
    return v


def _profile(d):
    return pb.IsolationProfile(
        run_as_non_root=d.get("run_as_non_root", False),
        drop_all_capabilities=d.get("drop_all_capabilities", False),
        no_new_privileges=d.get("no_new_privileges", False),
        read_only_root_fs=d.get("read_only_root_fs", False),
        block_metadata_egress=d.get("block_metadata_egress", False),
        seccomp_profile=d.get("seccomp_profile", ""),
    )


def _spec(image, iso):
    return pb.LaunchSpec(image=image, isolation=_profile(iso))


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        pb_grpc.add_DeploymentRuntimeServiceServicer_to_server(DeploymentRuntimeServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = pb_grpc.DeploymentRuntimeServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)


def run_lifecycle(rig: Rig, v):
    lc = v["lifecycle"]
    launched = rig.stub.Launch(pb.LaunchRequest(
        plugin_id=lc["plugin_id"], spec=_spec(lc["image"], v["full_profile"])))
    assert launched.instance_id, "Launch returned empty instance_id"
    assert launched.endpoint, "Launch returned empty endpoint"
    iid = launched.instance_id

    hc = rig.stub.Healthcheck(pb.HealthcheckRequest(instance_id=iid))
    assert hc.status == pb.HealthStatus.HEALTH_STATUS_HEALTHY, f"running status = {hc.status}"
    honored = json.loads(hc.detail).get("isolation_honored", {})
    for k, want in v["expect_isolation_honored"].items():
        assert honored.get(k) == want, f"isolation_honored[{k}] = {honored.get(k)}, want {want}"

    assert rig.stub.Terminate(pb.TerminateRequest(instance_id=iid)).terminated, "Terminate not terminated"

    gone = rig.stub.Healthcheck(pb.HealthcheckRequest(instance_id=iid))
    assert gone.status != pb.HealthStatus.HEALTH_STATUS_HEALTHY, "still HEALTHY after terminate"


def run_errors(rig: Rig, v):
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            rig.stub.Launch(pb.LaunchRequest(plugin_id="p", spec=_spec(s["image"], s["isolation"])))
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


def test_lifecycle():
    rig = Rig()
    try:
        run_lifecycle(rig, load_vectors())
    finally:
        rig.close()


def test_errors():
    rig = Rig()
    try:
        run_errors(rig, load_vectors())
    finally:
        rig.close()


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig()
    try:
        run_lifecycle(rig, v)
        run_errors(rig, v)
    finally:
        rig.close()
    print("PASS — deployment-runtime reference conformed to deployment-runtime/v1 golden vectors")
