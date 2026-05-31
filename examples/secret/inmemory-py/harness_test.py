"""Conformance/golden-data harness for the secret/v1 axis — Python side.

Loads the golden vectors (contracts/conformance/secret-v1.json) and drives THIS
implementation's SecretService.Resolve over real gRPC. The harness sets the
caller's tenant in the rat-callmeta-bin metadata header (ADR-007); the server
reads it to scope resolution by (tenant, secret_ref).

The load-bearing assertion is ANTI-ENUMERATION (proto FOUND SEMANTICS, reviews/06
API-1d / freeze-blocker #9): an unknown ref returns found=false, and a ref that
exists ONLY for another tenant ALSO returns found=false — never a distinguishable
response, never a PERMISSION_DENIED status. The `cross_tenant` vector proves it:
"ref://db/password" exists for acme, but a wonka caller must not be able to tell.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
import time
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.secret.v1 import secret_pb2, secret_pb2_grpc

from server import SecretServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "secret-v1.json"
)


def _now_ms() -> int:
    return int(time.time() * 1000)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "secret/v1", f'vectors axis = {v["axis"]!r}, want secret/v1'
    return v


class Rig:
    def __init__(self, tenant: str) -> None:
        self.tenant = tenant
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        secret_pb2_grpc.add_SecretServiceServicer_to_server(SecretServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = secret_pb2_grpc.SecretServiceStub(self.channel)

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

    def resolve(self, secret_ref):
        return self.stub.Resolve(
            secret_pb2.ResolveRequest(secret_ref=secret_ref),
            metadata=self._callmeta(),
        )


def run_resolve(rig: Rig, v) -> None:
    ttl_slack_ms = 2000
    for s in v["resolve"]:
        expect = s["expect"]
        before = _now_ms()
        resp = rig.resolve(s["secret_ref"])
        after = _now_ms()
        if expect["found"]:
            assert resp.found, f'{s["step"]}: found = False, want True'
            assert resp.value.decode("utf-8") == expect["value"], (
                f'{s["step"]}: value = {resp.value.decode("utf-8")!r}, want {expect["value"]!r}'
            )
            # found values carry a forward TTL hint; empty/false ones carry 0.
            assert before <= resp.expires_unix_ms <= after + 300_000 + ttl_slack_ms, (
                f'{s["step"]}: expires_unix_ms = {resp.expires_unix_ms}, want a forward TTL'
            )
        else:
            assert not resp.found, f'{s["step"]}: found = True, want False (anti-enumeration)'
            assert resp.value == b"", f'{s["step"]}: value = {resp.value!r}, want empty'
            assert resp.expires_unix_ms == 0, f'{s["step"]}: expires = {resp.expires_unix_ms}, want 0'


def run_cross_tenant(v) -> None:
    """The anti-enumeration proof: a ref that DOES exist for the vector's tenant
    (acme) must return found=false when resolved by a DIFFERENT tenant (wonka) —
    indistinguishable from a ref that does not exist at all. No PERMISSION_DENIED."""
    ct = v["cross_tenant"]
    rig = Rig(ct["tenant"])
    try:
        resp = rig.resolve(ct["secret_ref"])
        assert not resp.found, (
            f'{ct["step"]}: found = True, want False — cross-tenant ref must stay hidden'
        )
        assert resp.value == b"", f'{ct["step"]}: value = {resp.value!r}, want empty'
        assert resp.expires_unix_ms == 0, f'{ct["step"]}: expires = {resp.expires_unix_ms}, want 0'
    finally:
        rig.close()


def test_resolve_vectors():
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_resolve(rig, v)
    finally:
        rig.close()


def test_cross_tenant_hidden():
    """C7 + anti-enumeration structural check: tenant tracks the metadata envelope,
    and a cross-tenant hit is indistinguishable from a miss."""
    run_cross_tenant(load_vectors())


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_resolve(rig, v)
    finally:
        rig.close()
    run_cross_tenant(v)
    print("PASS — rat-secret-inmemory-py conformed to secret/v1 golden vectors")
