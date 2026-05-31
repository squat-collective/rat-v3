"""Conformance/golden-data harness for the billing/v1 axis — Python side.

Loads the golden vectors (contracts/conformance/billing-v1.json) and drives this
implementation's BillingService.Record over real gRPC. The harness sets the caller's
tenant in the rat-callmeta-bin metadata header (ADR-007); the server reads it to scope
the recorded usage; the harness then asserts the per-(tenant, meter) AGGREGATION and
a per-tenant ISOLATION property (C7): recording under one tenant never moves another
tenant's totals.

To assert aggregation, the Rig shares a single BillingLedger with the test (the same
instance the servicer writes into), so after the Record calls the test can read
ledger.aggregate(tenant, meter) directly.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.billing.v1 import billing_pb2, billing_pb2_grpc
from rat.common.v1 import context_pb2

from server import BillingServicer
from store import BillingLedger

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "billing-v1.json"
)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "billing/v1", f'vectors axis = {v["axis"]!r}, want billing/v1'
    return v


class Rig:
    def __init__(self, tenant: str, ledger: BillingLedger = None) -> None:
        self.tenant = tenant
        self.ledger = ledger if ledger is not None else BillingLedger()
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        billing_pb2_grpc.add_BillingServiceServicer_to_server(
            BillingServicer(self.ledger), self.server
        )
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = billing_pb2_grpc.BillingServiceStub(self.channel)

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

    def record(self, events):
        return self.stub.Record(
            billing_pb2.RecordRequest(events=events),
            metadata=self._callmeta(),
        )


def _events(spec):
    return [
        billing_pb2.UsageEvent(
            meter=e["meter"],
            quantity=e["quantity"],
            timestamp_unix_ms=0,
            dimensions={},
        )
        for e in spec
    ]


def run_record(rig: Rig, v) -> None:
    for s in v["record"]:
        resp = rig.record(_events(s["events"]))
        want = s["expect"]["recorded"]
        assert resp.recorded == want, f'{s["step"]}: recorded = {resp.recorded}, want {want}'
    # after all record cases, the per-(tenant, meter) aggregate must match
    for agg in v["expect_aggregate"]:
        got = rig.ledger.aggregate(v["tenant"], agg["meter"])
        assert got == agg["quantity"], (
            f'aggregate({v["tenant"]!r}, {agg["meter"]!r}) = {got}, want {agg["quantity"]}'
        )


def test_golden_vectors():
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_record(rig, v)
    finally:
        rig.close()


def test_per_tenant_isolation():
    """C7 structural check: recording under another tenant must NOT move the
    target tenant's aggregate. A shared ledger; two tenants; only the writer's
    totals change."""
    ledger = BillingLedger()
    # record some usage as "acme" first
    acme = Rig("acme", ledger)
    try:
        acme.record(_events([{"meter": "pipeline.run", "quantity": 2}]))
    finally:
        acme.close()
    acme_before = ledger.aggregate("acme", "pipeline.run")
    assert acme_before == 2.0, f"acme pipeline.run = {acme_before}, want 2.0"

    # now record as "globex" against the SAME ledger
    globex = Rig("globex", ledger)
    try:
        globex.record(_events([{"meter": "pipeline.run", "quantity": 5}]))
    finally:
        globex.close()

    # acme's aggregate is unchanged; globex is metered independently
    assert ledger.aggregate("acme", "pipeline.run") == acme_before, (
        "acme aggregate changed after globex recorded — tenant isolation broken (C7)"
    )
    assert ledger.aggregate("globex", "pipeline.run") == 5.0, (
        f'globex pipeline.run = {ledger.aggregate("globex", "pipeline.run")}, want 5.0'
    )


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_record(rig, v)
    finally:
        rig.close()
    test_per_tenant_isolation()
    print("PASS — rat-billing-inmemory-py conformed to billing/v1 golden vectors")
