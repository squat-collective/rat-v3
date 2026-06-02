"""Self-test for rat-catalog-ducklake-py — drives the CatalogService over real gRPC
against a real DuckLake.

Deliberately NOT named harness_test.py: it is NOT wired into the frozen catalog/v1
golden-vector conformance suite yet (the branch model is the open §10 Q2 spike). It
proves the DuckLake-backed behavior that IS settled:

  1. a separate "engine" connection creates + writes lake.reviews (producing real
     DuckLake snapshots);
  2. GetTable on a non-existent table → NOT_FOUND (error-model M2);
  3. RegisterTable is idempotent;
  4. GetTable resolves the table's REAL current snapshot from the lake;
  5. CommitTable records the engine-reported snapshot, and is idempotent under retry
     (same key → already_applied, original value) and optimistic-concurrency-guarded;
  6. CreateBranch / MergeBranch (thin tracker) round-trip + merge idempotency.

Run (containerized):
  podman run --rm -v "$PWD":/work:Z -e PYTHONPATH=/work/contracts/sdks/python \
    -w /work/examples/catalog/ducklake-py python:3.12 \
    bash -c 'pip install -q -r requirements.txt && python selftest.py'
"""

import os
import tempfile
from concurrent import futures

import duckdb
import grpc

from rat.catalog.v1 import catalog_pb2, catalog_pb2_grpc

from server import CatalogServicer
from store import Catalog


def _seed_lake(meta: str, data: str):
    """Stand in for the engine: create + populate lake.reviews, producing snapshots."""
    con = duckdb.connect()
    con.execute("INSTALL ducklake; LOAD ducklake;")
    con.execute(f"ATTACH 'ducklake:{meta}' AS lake (DATA_PATH '{data}')")
    con.execute("CREATE TABLE lake.reviews(id INTEGER, text VARCHAR, embedding FLOAT[])")
    con.execute("INSERT INTO lake.reviews VALUES (1,'a',NULL),(2,'b',NULL)")
    snap = con.execute("SELECT max(snapshot_id) FROM lake.snapshots()").fetchone()[0]
    con.close()
    return f"snap-{snap}"


class Rig:
    def __init__(self, cat: Catalog) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        catalog_pb2_grpc.add_CatalogServiceServicer_to_server(CatalogServicer(cat), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = catalog_pb2_grpc.CatalogServiceStub(self.channel)

    def close(self):
        self.channel.close()
        self.server.stop(None)


def main():
    with tempfile.TemporaryDirectory() as tmp:
        meta = f"sqlite:{os.path.join(tmp, 'lakemeta.db')}"
        data = os.path.join(tmp, "lakedata") + "/"
        tracking = os.path.join(tmp, "tracking.db")

        engine_snap = _seed_lake(meta, data)
        rig = Rig(Catalog(tracking, meta, data))
        try:
            # 2. GetTable on a missing table -> NOT_FOUND
            try:
                rig.stub.GetTable(catalog_pb2.GetTableRequest(identifier="nope"))
                raise AssertionError("GetTable(nope) should be NOT_FOUND")
            except grpc.RpcError as e:
                assert e.code() == grpc.StatusCode.NOT_FOUND, e.code()

            # 3. RegisterTable idempotent
            r1 = rig.stub.RegisterTable(catalog_pb2.RegisterTableRequest(identifier="reviews"))
            r2 = rig.stub.RegisterTable(catalog_pb2.RegisterTableRequest(identifier="reviews"))
            assert r1.table.identifier == "reviews" and r2.table.identifier == "reviews"

            # 4. GetTable resolves the REAL current lake snapshot
            g = rig.stub.GetTable(catalog_pb2.GetTableRequest(identifier="reviews"))
            assert g.table.branch == "main", g.table.branch
            assert engine_snap in g.table.uri, f"uri {g.table.uri!r} lacks real snapshot {engine_snap}"

            # 5. CommitTable records the engine snapshot; idempotent on retry
            c1 = rig.stub.CommitTable(catalog_pb2.CommitTableRequest(
                identifier="reviews", snapshot_id=engine_snap, idempotency_key="run-1"))
            assert c1.snapshot_id == engine_snap and not c1.already_applied
            c2 = rig.stub.CommitTable(catalog_pb2.CommitTableRequest(
                identifier="reviews", snapshot_id=engine_snap, idempotency_key="run-1"))
            assert c2.already_applied and c2.snapshot_id == engine_snap, "retry must be already_applied"
            # optimistic-concurrency guard: wrong expected_snapshot -> FAILED_PRECONDITION
            try:
                rig.stub.CommitTable(catalog_pb2.CommitTableRequest(
                    identifier="reviews", snapshot_id="snap-99",
                    expected_snapshot="snap-wrong", idempotency_key="run-2"))
                raise AssertionError("stale expected_snapshot should be FAILED_PRECONDITION")
            except grpc.RpcError as e:
                assert e.code() == grpc.StatusCode.FAILED_PRECONDITION, e.code()

            # 6. branches (thin tracker)
            rig.stub.CreateBranch(catalog_pb2.CreateBranchRequest(branch="exp-1", from_branch="main"))
            m1 = rig.stub.MergeBranch(catalog_pb2.MergeBranchRequest(
                branch="exp-1", into_branch="main", idempotency_key="merge-1"))
            m2 = rig.stub.MergeBranch(catalog_pb2.MergeBranchRequest(
                branch="exp-1", into_branch="main", idempotency_key="merge-1"))
            assert not m1.already_applied and m2.already_applied, "merge retry must be already_applied"

            print("PASS — rat-catalog-ducklake-py: DuckLake-backed get/register/commit "
                  "(real snapshot, idempotent, optimistic) + branch tracker conformant")
        finally:
            rig.close()


if __name__ == "__main__":
    main()
