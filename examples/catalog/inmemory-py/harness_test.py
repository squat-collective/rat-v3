"""Conformance/golden-data harness for the catalog/v1 axis — Python side.

The ADR-003 cross-run: loads the SAME golden vectors the Go reference loads
(contracts/conformance/catalog-v1.json) and drives THIS independent implementation's
CatalogService over real gRPC. The lifecycle is STATEFUL (steps share one catalog,
in order); a step with expect.code asserts a gRPC error mid-sequence (the
optimistic-concurrency reject). Driven directly (no gateway), like the other Python
references. Runs standalone or under pytest.
"""

import json
import os
from concurrent import futures

import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from rat.common.v1 import context_pb2

from server import CatalogServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "catalog-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
    "FAILED_PRECONDITION": grpc.StatusCode.FAILED_PRECONDITION,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "catalog/v1", f'vectors axis = {v["axis"]!r}, want catalog/v1'
    return v


def _callmeta():
    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(
            traceparent="00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
            correlation_id="corr-golden",
        ),
        identity=context_pb2.Identity(tenant="acme"),
    )
    return [("rat-callmeta-bin", rc.SerializeToString())]


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        catalog_pb2_grpc.add_CatalogServiceServicer_to_server(CatalogServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = catalog_pb2_grpc.CatalogServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def do(self, s):
        op = s["op"]
        if op == "get_table":
            return self.stub.GetTable(
                catalog_pb2.GetTableRequest(identifier=s.get("identifier", ""), branch=s.get("branch", "")),
                metadata=_callmeta())
        if op == "create_branch":
            return self.stub.CreateBranch(
                catalog_pb2.CreateBranchRequest(branch=s.get("branch", ""), from_branch=s.get("from_branch", "")),
                metadata=_callmeta())
        if op == "merge_branch":
            return self.stub.MergeBranch(
                catalog_pb2.MergeBranchRequest(
                    branch=s.get("branch", ""), into_branch=s.get("into_branch", ""),
                    expected_into_snapshot=s.get("expected_into_snapshot", ""),
                    idempotency_key=s.get("idempotency_key", "")),
                metadata=_callmeta())
        raise AssertionError(f'unknown op {op!r}')


def _assert_success(s, resp):
    e, op = s["expect"], s["op"]
    if op == "get_table" and "table" in e:
        assert resp.table.identifier == e["table"]["identifier"], (
            f'table.identifier = {resp.table.identifier!r}, want {e["table"]["identifier"]!r}')
        assert resp.table.branch == e["table"]["branch"], (
            f'table.branch = {resp.table.branch!r}, want {e["table"]["branch"]!r}')
    elif op == "create_branch" and "branch" in e:
        assert resp.branch == e["branch"], f'branch = {resp.branch!r}, want {e["branch"]!r}'
    elif op == "merge_branch":
        if "already_applied" in e:
            assert resp.already_applied == e["already_applied"], (
                f'already_applied = {resp.already_applied}, want {e["already_applied"]}')
        if e.get("snapshot_id_set"):
            assert resp.snapshot_id != "", "snapshot_id empty, want set"


def run_step(rig: Rig, s):
    code = s["expect"].get("code")
    try:
        resp = rig.do(s)
    except grpc.RpcError as err:
        assert code, f'{s["step"]}: unexpected error {err.code()}'
        assert err.code() == _CODE[code], f'{s["step"]}: status {err.code()}, want {code}'
        return
    assert not code, f'{s["step"]}: want error {code}, got success'
    _assert_success(s, resp)


def run_lifecycle(rig: Rig, v) -> None:
    for s in v["lifecycle"]:
        run_step(rig, s)


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        run_step(rig, s)


def test_golden_vectors():
    rig = Rig()
    try:
        run_lifecycle(rig, load_vectors())
    finally:
        rig.close()


def test_error_vectors():
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
    finally:
        rig.close()
    rig = Rig()
    try:
        run_errors(rig, v)
    finally:
        rig.close()
    print("PASS — rat-catalog-inmemory-py conformed to catalog/v1 golden vectors")
