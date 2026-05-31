"""Conformance/golden-data harness for the tenancy/v1 axis — Python side.

Loads the golden vectors (contracts/conformance/tenancy-v1.json) and drives this
implementation's TenancyService.Decide over real gRPC. The harness sets the
caller's tenant in the rat-callmeta-bin metadata header (ADR-007); the server
reads it to scope the decision. Cases are evaluated IN ORDER because the QUOTA
decision is stateful (a per-tenant counter on the policy instance).

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.tenancy.v1 import tenancy_pb2, tenancy_pb2_grpc

from server import TenancyServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "tenancy-v1.json"
)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "tenancy/v1", f'vectors axis = {v["axis"]!r}, want tenancy/v1'
    return v


def _kind(name: str):
    """Map a golden-vector kind string (e.g. "QUOTA") to the proto enum value."""
    return tenancy_pb2.DecisionKind.Value(f"DECISION_KIND_{name}")


def _deny_code(name: str):
    """Map a golden-vector deny_code string (e.g. "QUOTA_EXCEEDED") to the enum value."""
    return tenancy_pb2.DenyCode.Value(f"DENY_CODE_{name}")


class Rig:
    def __init__(self, tenant: str) -> None:
        self.tenant = tenant
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        tenancy_pb2_grpc.add_TenancyServiceServicer_to_server(TenancyServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = tenancy_pb2_grpc.TenancyServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def _callmeta(self):
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(
                traceparent="00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
                correlation_id="corr-golden",
            ),
            identity=context_pb2.Identity(tenant=self.tenant),
        )
        return [("rat-callmeta-bin", rc.SerializeToString())]

    def decide(self, kind, subject_action, counterparty_tenant=""):
        return self.stub.Decide(
            tenancy_pb2.DecideRequest(
                kind=kind,
                subject_action=subject_action,
                counterparty_tenant=counterparty_tenant,
            ),
            metadata=self._callmeta(),
        )


def run_decide(rig: Rig, v) -> None:
    # IN ORDER — quota is stateful.
    for s in v["decide"]:
        expect = s["expect"]
        resp = rig.decide(
            kind=_kind(s["kind"]),
            subject_action=s["subject_action"],
            counterparty_tenant=s.get("counterparty_tenant", ""),
        )
        assert resp.allowed == expect["allowed"], (
            f'{s["step"]}: allowed = {resp.allowed}, want {expect["allowed"]}'
        )
        if not expect["allowed"]:
            want = _deny_code(expect["deny_code"])
            assert resp.deny_code == want, (
                f'{s["step"]}: deny_code = {resp.deny_code}, want {want} ({expect["deny_code"]})'
            )


def test_golden_vectors():
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_decide(rig, v)
    finally:
        rig.close()


def test_tenant_from_metadata_not_request():
    """C7 structural check: the decision is posed for the metadata tenant; there is
    no request field that could override it. A tenant with no allowlist entry is
    denied any cross-tenant share."""
    rig = Rig("globex")  # not in SHARING_ALLOWLIST
    try:
        resp = rig.decide(
            kind=tenancy_pb2.DecisionKind.DECISION_KIND_SHARING,
            subject_action="share:dataset/x",
            counterparty_tenant="partner",
        )
        assert not resp.allowed, "globex has no allowlist; share must be denied"
        assert resp.deny_code == tenancy_pb2.DenyCode.DENY_CODE_CROSS_TENANT_DENIED
    finally:
        rig.close()


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_decide(rig, v)
    finally:
        rig.close()
    test_tenant_from_metadata_not_request()
    print("PASS — rat-tenancy-inmemory-py conformed to tenancy/v1 golden vectors")
