"""Conformance + round-2 semantic harness for rat-catalog-sqlite-py.

 1. ADR-003 cross-run — loads the SAME shared golden vectors the in-memory catalog
    references load (contracts/conformance/catalog-v1.json) and drives this
    sqlite-backed CatalogService through the STATEFUL lifecycle + errors over real
    gRPC. A real backend passing the identical vectors is the round-2 evidence.

 2. test_durability_branches_and_ledger_survive_reopen — create a branch, merge it,
    close the catalog, reopen the SAME db file: the branch, the moved snapshot, AND
    the idempotency ledger all persist (a re-merge with the same key is still a
    no-op returning already_applied). The in-memory catalog loses all of it.

 3. test_concurrent_merge_one_winner — N threads race a MergeBranch into main from
    the same expected snapshot; exactly one COMMITs (the rest FAILED_PRECONDITION).
    Lost-update prevention enforced by sqlite BEGIN IMMEDIATE — the publish gate of
    the pipeline model (reviews/06 #8), not an in-process mutex.

Runs standalone (`python harness_test.py`) or under pytest.
"""

import json
import os
import shutil
import tempfile
import threading
from concurrent import futures

import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc

from server import CatalogServicer
from store import Catalog, CatalogError

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


class Rig:
    def __init__(self) -> None:
        self._dir = tempfile.mkdtemp(prefix="rat-catalog-sqlite-")
        self.catalog = Catalog(os.path.join(self._dir, "catalog.db"))
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        catalog_pb2_grpc.add_CatalogServiceServicer_to_server(CatalogServicer(self.catalog), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = catalog_pb2_grpc.CatalogServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)
        self.catalog.close()
        shutil.rmtree(self._dir, ignore_errors=True)

    def do(self, s):
        op = s["op"]
        if op == "get_table":
            return self.stub.GetTable(
                catalog_pb2.GetTableRequest(identifier=s.get("identifier", ""), branch=s.get("branch", "")))
        if op == "create_branch":
            return self.stub.CreateBranch(
                catalog_pb2.CreateBranchRequest(branch=s.get("branch", ""), from_branch=s.get("from_branch", "")))
        if op == "merge_branch":
            return self.stub.MergeBranch(catalog_pb2.MergeBranchRequest(
                branch=s.get("branch", ""), into_branch=s.get("into_branch", ""),
                expected_into_snapshot=s.get("expected_into_snapshot", ""),
                idempotency_key=s.get("idempotency_key", "")))
        if op == "register_table":
            return self.stub.RegisterTable(catalog_pb2.RegisterTableRequest(
                identifier=s.get("identifier", ""), uri=s.get("uri", ""), branch=s.get("branch", "")))
        if op == "commit_table":
            return self.stub.CommitTable(catalog_pb2.CommitTableRequest(
                identifier=s.get("identifier", ""), branch=s.get("branch", ""),
                snapshot_id=s.get("snapshot_id", ""), expected_snapshot=s.get("expected_snapshot", ""),
                idempotency_key=s.get("idempotency_key", "")))
        raise AssertionError(f'unknown op {op!r}')


def _assert_success(s, resp):
    e, op = s["expect"], s["op"]
    if op == "get_table" and "table" in e:
        assert resp.table.identifier == e["table"]["identifier"]
        assert resp.table.branch == e["table"]["branch"]
    elif op == "create_branch" and "branch" in e:
        assert resp.branch == e["branch"]
    elif op == "merge_branch":
        if "already_applied" in e:
            assert resp.already_applied == e["already_applied"], (
                f'already_applied = {resp.already_applied}, want {e["already_applied"]}')
        if e.get("snapshot_id_set"):
            assert resp.snapshot_id != "", "snapshot_id empty, want set"
    elif op == "register_table" and "table" in e:
        assert resp.table.identifier == e["table"]["identifier"]
        assert resp.table.branch == e["table"]["branch"]
    elif op == "commit_table":
        if "already_applied" in e:
            assert resp.already_applied == e["already_applied"], (
                f'already_applied = {resp.already_applied}, want {e["already_applied"]}')
        if "snapshot_id" in e:
            assert resp.snapshot_id == e["snapshot_id"], (
                f'snapshot_id = {resp.snapshot_id!r}, want {e["snapshot_id"]!r}')
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


# 1. ADR-003 cross-run.
def test_golden_vectors():
    rig = Rig()
    try:
        for s in load_vectors()["lifecycle"]:
            run_step(rig, s)
    finally:
        rig.close()


def test_error_vectors():
    rig = Rig()
    try:
        for s in load_vectors()["errors"]:
            run_step(rig, s)
    finally:
        rig.close()


# 2. Round-2: durability of branches + snapshots + the idempotency ledger.
def test_durability_branches_and_ledger_survive_reopen():
    d = tempfile.mkdtemp(prefix="rat-catalog-dur-")
    try:
        path = os.path.join(d, "cat.db")
        c1 = Catalog(path)
        c1.create_branch("run-7", "main")
        snap, already = c1.merge_branch("run-7", "main", "snap-0", "merge-7")
        assert not already and snap == "snap-1"
        c1.close()

        c2 = Catalog(path)  # reopen the SAME file
        branch, _ = c2.get_table("warehouse.sales.orders", "run-7")  # branch persisted
        assert branch == "run-7", "branch did not survive reopen"
        # idempotency ledger persisted: a re-merge with the same key is still a no-op.
        snap2, already2 = c2.merge_branch("run-7", "main", "", "merge-7")
        assert already2 and snap2 == "snap-1", "idempotency ledger did not survive reopen"
        c2.close()
    finally:
        shutil.rmtree(d, ignore_errors=True)


# 3. Round-2: concurrent merges into one target — exactly one wins (no lost update).
def test_concurrent_merge_one_winner():
    d = tempfile.mkdtemp(prefix="rat-catalog-cas-")
    try:
        store = Catalog(os.path.join(d, "cas.db"))
        store.create_branch("run", "main")  # source branch off main@snap-0

        n = 16
        barrier = threading.Barrier(n)
        outcomes = [None] * n

        def attempt(i):
            barrier.wait()  # all threads merge into main expecting snap-0 at once
            try:
                _, already = store.merge_branch("run", "main", "snap-0", f"k{i}")
                outcomes[i] = ("ok", already)
            except CatalogError as e:
                outcomes[i] = ("err", e.code)

        threads = [threading.Thread(target=attempt, args=(i,)) for i in range(n)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        winners = sum(1 for o in outcomes if o[0] == "ok")
        conflicts = sum(1 for o in outcomes if o[0] == "err" and o[1] == grpc.StatusCode.FAILED_PRECONDITION)
        assert winners == 1, f"concurrent merge: {winners} winners among {n}, want exactly 1 (no lost update)"
        assert conflicts == n - 1, f"expected {n - 1} FAILED_PRECONDITION conflicts, got {conflicts}"
        store.close()
    finally:
        shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    test_golden_vectors()
    test_error_vectors()
    test_durability_branches_and_ledger_survive_reopen()
    test_concurrent_merge_one_winner()
    print("PASS — rat-catalog-sqlite-py conformed to catalog/v1 golden vectors + durability + concurrent-merge safety")
