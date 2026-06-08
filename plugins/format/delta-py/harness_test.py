"""Conformance + round-2 harness for rat-format-delta-py.

 1. ADR-003 cross-run on the SAME shared vectors (contracts/conformance/format-v1.json)
    the in-memory + parquet refs use — source rows staged as real Arrow, scan results
    pulled back as real Arrow (the typed-Arrow data leg, both directions).

 2. test_delta_time_travel — a property neither the in-memory dict nor plain Parquet
    files can show: read a PRIOR table version via Delta's versioned snapshots.

Note: deltalake's Rust runtime aborts at interpreter teardown (a known quirk) AFTER
all logic completes; the standalone runner calls os._exit(0) after PASS to exit
cleanly. Runs standalone (`python harness_test.py`) or under pytest.
"""

import json
import os
import tempfile
from concurrent import futures

import grpc
import pyarrow as pa
from deltalake import DeltaTable

from rat.common.v1 import data_pb2
from rat.format.v1 import format_pb2, format_pb2_grpc

from server import FormatServicer
from store import FORMAT, Store

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "format-v1.json"
)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "format/v1", f'vectors axis = {v["axis"]!r}, want format/v1'
    return v


def _ref(identifier):
    return data_pb2.TableRef(identifier=identifier)


class Rig:
    def __init__(self) -> None:
        self._dir = tempfile.mkdtemp(prefix="rat-format-")
        self.servicer = FormatServicer(Store(self._dir))
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        format_pb2_grpc.add_FormatServiceServicer_to_server(self.servicer, self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = format_pb2_grpc.FormatServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def _source(self, rows):
        return self.servicer.streams.put(pa.Table.from_pylist(rows))

    def scan(self, identifier):
        resp = self.stub.Resolve(format_pb2.ResolveRequest(table=_ref(identifier)))
        table = self.servicer.streams.pull(resp.stream)
        return table.to_pylist() if table is not None and table.num_columns else []


def _assert_write(result, e):
    if "rows_affected" in e:
        assert result.HasField("rows_affected") and result.rows_affected == e["rows_affected"], (
            f'rows_affected = {result.rows_affected if result.HasField("rows_affected") else None}, want {e["rows_affected"]}')
    if e.get("rows_affected_absent"):
        assert not result.HasField("rows_affected"), "rows_affected present, want absent"
    if e.get("snapshot_id_set"):
        assert result.snapshot_id != "", "snapshot_id empty, want set"


def _contains_row(rows, want):
    return any(all(r.get(k) == val for k, val in want.items()) for r in rows)


def _assert_scan(rows, e):
    if "row_count" in e:
        assert len(rows) == e["row_count"], f"scan = {len(rows)} rows, want {e['row_count']}"
    for want in e.get("rows_contain", []):
        assert _contains_row(rows, want), f"scan rows {rows} missing {want}"


def run_lifecycle(rig: Rig, v):
    table = v["table"]
    for s in v["lifecycle"]:
        op, e = s["op"], s["expect"]
        if op == "append":
            _assert_write(rig.stub.Append(format_pb2.AppendRequest(
                table=_ref(table), source=rig._source(s["source"]))).result, e)
        elif op == "merge":
            _assert_write(rig.stub.Merge(format_pb2.MergeRequest(
                table=_ref(table), merge_keys=s.get("merge_keys", []),
                source=rig._source(s["source"]))).result, e)
        elif op == "overwrite":
            _assert_write(rig.stub.Overwrite(format_pb2.OverwriteRequest(
                table=_ref(table), source=rig._source(s["source"]))).result, e)
        elif op == "maintain":
            _assert_write(rig.stub.Maintain(format_pb2.MaintainRequest(table=_ref(table))).result, e)
        elif op == "scan":
            _assert_scan(rig.scan(table), e)
        else:
            raise AssertionError(f"unknown op {op!r}")


def run_errors(rig: Rig, v):
    for s in v["errors"]:
        table = s["table_override"] if "table_override" in s else v["table"]
        want = grpc.StatusCode.INVALID_ARGUMENT
        try:
            if s["op"] == "scan":
                rig.scan(table)
            elif s["op"] == "merge":
                rig.stub.Merge(format_pb2.MergeRequest(
                    table=_ref(table), merge_keys=s.get("merge_keys", []),
                    source=rig._source(s.get("source", []))))
            else:
                raise AssertionError(f"unknown error-op {s['op']!r}")
        except grpc.RpcError as ex:
            assert ex.code() == want, f'{s["step"]}: status {ex.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error, got success')


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


# Round-2: Delta versioning / TIME TRAVEL — read a prior table version.
def test_delta_time_travel():
    root = tempfile.mkdtemp(prefix="rat-format-delta-")
    store = Store(root)
    store.append("warehouse.sales.orders", [{"id": "1", "name": "alice"}])  # version 0
    store.append("warehouse.sales.orders", [{"id": "2", "name": "bob"}])    # version 1
    path = store.table_path("warehouse.sales.orders")
    assert DeltaTable(path).version() == 1
    assert DeltaTable(path).to_pyarrow_table().num_rows == 2, "current version should have 2 rows"
    assert DeltaTable(path, version=0).to_pyarrow_table().num_rows == 1, "time-travel to v0 should show 1 row"


if __name__ == "__main__":
    v = load_vectors()
    for runner in (run_lifecycle, run_errors):
        rig = Rig()
        try:
            runner(rig, v)
        finally:
            rig.close()
    test_delta_time_travel()
    # flush BEFORE os._exit (which skips buffer flushing). deltalake's Rust runtime
    # aborts on interpreter teardown (after all logic ran); exit cleanly past it.
    print(f"PASS — {FORMAT}: real format backend conformed to format/v1 golden vectors + time travel", flush=True)
    os._exit(0)
