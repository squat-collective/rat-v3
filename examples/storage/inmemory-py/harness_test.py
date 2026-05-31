"""Conformance/golden-data harness for the storage/v1 axis — Python side.

The ADR-003 cross-run: loads the SAME golden vectors the Go reference loads
(contracts/conformance/storage-v1.json) and drives THIS independent implementation's
StorageService.VendCredentials over real gRPC. The harness sets the caller's tenant
in the rat-callmeta-bin metadata header (ADR-007); the server reads it to scope the
vended credentials; the harness decodes the (conformance) scope receipt and asserts
the C7 obligation (scope.tenant == caller tenant, + prefix + mode + short TTL).

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
import time
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.storage.v1 import storage_pb2, storage_pb2_grpc

from server import StorageServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "storage-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}

_MODE = {
    "READ": storage_pb2.AccessMode.ACCESS_MODE_READ,
    "WRITE": storage_pb2.AccessMode.ACCESS_MODE_WRITE,
    "READ_WRITE": storage_pb2.AccessMode.ACCESS_MODE_READ_WRITE,
    "UNSPECIFIED": storage_pb2.AccessMode.ACCESS_MODE_UNSPECIFIED,
}


def _now_ms() -> int:
    return int(time.time() * 1000)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "storage/v1", f'vectors axis = {v["axis"]!r}, want storage/v1'
    return v


class Rig:
    def __init__(self, tenant: str) -> None:
        self.tenant = tenant
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        storage_pb2_grpc.add_StorageServiceServicer_to_server(StorageServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = storage_pb2_grpc.StorageServiceStub(self.channel)

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

    def vend(self, prefix, mode):
        return self.stub.VendCredentials(
            storage_pb2.VendCredentialsRequest(prefix=prefix, mode=_MODE[mode]),
            metadata=self._callmeta(),
        )


def run_lifecycle(rig: Rig, v) -> None:
    ttl_ms = v["credentials_ttl_seconds"] * 1000
    slack_ms = 2000
    for s in v["lifecycle"]:
        expect = s["expect"]
        before = _now_ms()
        resp = rig.vend(s["prefix"], s["mode"])
        after = _now_ms()
        if expect.get("credentials_present"):
            assert resp.credentials, "credentials empty, want present"
        got = json.loads(resp.credentials.decode("utf-8"))
        sc = expect.get("scope")
        if sc:
            assert got["tenant"] == sc["tenant"], f'scope.tenant = {got["tenant"]!r}, want {sc["tenant"]!r}'
            assert got["prefix"] == sc["prefix"], f'scope.prefix = {got["prefix"]!r}, want {sc["prefix"]!r}'
            assert got["mode"] == sc["mode"], f'scope.mode = {got["mode"]!r}, want {sc["mode"]!r}'
        exp = resp.expires_unix_ms
        assert before + ttl_ms - slack_ms <= exp <= after + ttl_ms + slack_ms, (
            f"expires_unix_ms = {exp}, want ~ now + {ttl_ms}ms"
        )
        assert exp == got["expires_unix_ms"], "response expires != receipt expires"


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            rig.vend(s["prefix"], s["mode"])
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status = {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


def test_golden_vectors():
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_lifecycle(rig, v)
    finally:
        rig.close()


def test_error_vectors():
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_errors(rig, v)
    finally:
        rig.close()


def test_tenant_from_metadata_not_request():
    """C7 structural check: the vended tenant tracks the metadata envelope; there is
    no request field that could override it."""
    rig = Rig("globex")
    try:
        resp = rig.vend("s3://bucket/x", "READ")
        got = json.loads(resp.credentials.decode("utf-8"))
        assert got["tenant"] == "globex", f'scope.tenant = {got["tenant"]!r}, want globex (the metadata tenant)'
    finally:
        rig.close()


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig(v["tenant"])
    try:
        run_lifecycle(rig, v)
        run_errors(rig, v)
    finally:
        rig.close()
    test_tenant_from_metadata_not_request()
    print("PASS — rat-storage-inmemory-py conformed to storage/v1 golden vectors")
