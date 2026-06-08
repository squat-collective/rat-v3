"""Conformance harness for rat-storage-minio-s3 (offline — ScopeReceipt minter).

Two suites, no MinIO needed (the plugin's offline mode echoes the granted scope, so
the SAME storage-v1 golden vectors apply):

  1. storage-v1 — the shared golden vectors (contracts/conformance/storage-v1.json),
     same as the in-memory references: tenant read from the rat-callmeta-bin header
     (ADR-007, not a request field), scope = {tenant, prefix, mode}, short-TTL.

  2. the Q02 5c read/write split — the assertions NO golden file covers yet, because
     this is the first reference to implement VendReadCredentials / VendWriteCredentials:
     the method FIXES the mode (read RPC → READ scope, write RPC → WRITE scope) and both
     still scope to the metadata tenant and reject an empty prefix.

The real MinIO STS path (MinioSTSMinter) is exercised by the remote runner against the
live compose stack, not here.
"""

import json
import os
import time
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.storage.v1 import storage_pb2, storage_pb2_grpc

from creds import ScopeReceiptMinter
from server import StorageServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "storage-v1.json"
)

_CODE = {"INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT}
_MODE = {
    "READ": storage_pb2.AccessMode.ACCESS_MODE_READ,
    "WRITE": storage_pb2.AccessMode.ACCESS_MODE_WRITE,
    "READ_WRITE": storage_pb2.AccessMode.ACCESS_MODE_READ_WRITE,
    "UNSPECIFIED": storage_pb2.AccessMode.ACCESS_MODE_UNSPECIFIED,
}


def _now_ms():
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
        storage_pb2_grpc.add_StorageServiceServicer_to_server(
            StorageServicer(ScopeReceiptMinter()), self.server)
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
                correlation_id="corr-golden"),
            identity=context_pb2.Identity(tenant=self.tenant),
        )
        return [("rat-callmeta-bin", rc.SerializeToString())]

    def vend(self, prefix, mode):
        return self.stub.VendCredentials(
            storage_pb2.VendCredentialsRequest(prefix=prefix, mode=_MODE[mode]),
            metadata=self._callmeta())

    def vend_read(self, prefix):
        return self.stub.VendReadCredentials(
            storage_pb2.VendReadCredentialsRequest(prefix=prefix), metadata=self._callmeta())

    def vend_write(self, prefix):
        return self.stub.VendWriteCredentials(
            storage_pb2.VendWriteCredentialsRequest(prefix=prefix), metadata=self._callmeta())


def run_lifecycle(rig, v):
    ttl_ms = v["credentials_ttl_seconds"] * 1000
    slack = 2000
    for s in v["lifecycle"]:
        before = _now_ms()
        resp = rig.vend(s["prefix"], s["mode"])
        after = _now_ms()
        assert resp.credentials, "credentials empty"
        got = json.loads(resp.credentials.decode("utf-8"))
        sc = s["expect"]["scope"]
        assert got["tenant"] == sc["tenant"], f'tenant {got["tenant"]!r} != {sc["tenant"]!r}'
        assert got["prefix"] == sc["prefix"], f'prefix {got["prefix"]!r} != {sc["prefix"]!r}'
        assert got["mode"] == sc["mode"], f'mode {got["mode"]!r} != {sc["mode"]!r}'
        assert before + ttl_ms - slack <= resp.expires_unix_ms <= after + ttl_ms + slack


def run_errors(rig, v):
    for s in v["errors"]:
        try:
            rig.vend(s["prefix"], s["mode"])
        except grpc.RpcError as e:
            assert e.code() == _CODE[s["expect"]["code"]], f'{s["step"]}: {e.code()}'
        else:
            raise AssertionError(f'{s["step"]}: want error, got success')


def run_5c_split(rig):
    """The read/write split: the method fixes the mode regardless of the caller."""
    r = json.loads(rig.vend_read("lake").credentials.decode("utf-8"))
    assert r["mode"] == "READ" and r["tenant"] == rig.tenant, f"read vend scope wrong: {r}"
    w = json.loads(rig.vend_write("lake").credentials.decode("utf-8"))
    assert w["mode"] == "WRITE" and w["tenant"] == rig.tenant, f"write vend scope wrong: {w}"
    for fn in (rig.vend_read, rig.vend_write):
        try:
            fn("")
        except grpc.RpcError as e:
            assert e.code() == grpc.StatusCode.INVALID_ARGUMENT
        else:
            raise AssertionError("empty prefix must be INVALID_ARGUMENT")


def test_golden_vectors():
    v = load_vectors(); rig = Rig(v["tenant"])
    try:
        run_lifecycle(rig, v)
    finally:
        rig.close()


def test_error_vectors():
    v = load_vectors(); rig = Rig(v["tenant"])
    try:
        run_errors(rig, v)
    finally:
        rig.close()


def test_5c_read_write_split():
    rig = Rig("acme")
    try:
        run_5c_split(rig)
    finally:
        rig.close()


if __name__ == "__main__":
    v = load_vectors(); rig = Rig(v["tenant"])
    try:
        run_lifecycle(rig, v)
        run_errors(rig, v)
        run_5c_split(rig)
    finally:
        rig.close()
    print("PASS — rat-storage-minio-s3: storage/v1 golden vectors + Q02 5c read/write split")
