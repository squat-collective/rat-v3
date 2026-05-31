"""Conformance + round-2 harness for the Parquet format backend — with the data leg
carried by REAL Arrow Flight (not the in-process Arrow-IPC registry).

 1. ADR-003 cross-run — loads the SAME shared vectors the in-memory format refs load
    (contracts/conformance/format-v1.json). Source rows for Append/Merge/Overwrite
    are hosted on the HARNESS's real Flight server (the format plugin DoGets them);
    Resolve results are hosted on the PLUGIN's real Flight server (the harness DoGets
    them). The data crosses real TCP sockets via Flight DoGet, both directions.

 2. A backend test that real Parquet files land on disk + are readable.

Runs standalone (`python harness_test.py`) or under pytest.
"""

import glob
import json
import os
import tempfile
from concurrent import futures

import grpc
import pyarrow as pa
import pyarrow.parquet as pq

from rat.common.v1 import data_pb2
from rat.format.v1 import format_pb2, format_pb2_grpc

from flight import FlightHost, flight_pull
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
        self.source_flight = FlightHost()  # the caller hosts Append/Merge/Overwrite sources
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        format_pb2_grpc.add_FormatServiceServicer_to_server(self.servicer, self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = format_pb2_grpc.FormatServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)
        self.source_flight.stop()
        self.servicer.close()  # stops the plugin's Flight server

    def _source(self, rows):
        # Host the source on the harness's Flight server; the plugin DoGets it.
        return self.source_flight.put(pa.Table.from_pylist(rows))

    def scan(self, identifier):
        resp = self.stub.Resolve(format_pb2.ResolveRequest(table=_ref(identifier)))
        table = flight_pull(resp.stream)  # real Flight DoGet from the plugin's endpoint
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
        want = grpc.StatusCode.INVALID_ARGUMENT  # both error vectors use INVALID_ARGUMENT
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


# Round-2: real Parquet files land on disk and are readable as Parquet.
def test_real_parquet_files_on_disk():
    root = tempfile.mkdtemp(prefix="rat-format-pq-")
    store = Store(root)
    store.append("warehouse.sales.orders", [{"id": "1", "name": "alice"}])
    files = glob.glob(os.path.join(store.table_dir("warehouse.sales.orders"), "*.parquet"))
    assert files, "no parquet files written to disk"
    t = pq.read_table(files[0])
    assert t.num_rows == 1 and t.column("name")[0].as_py() == "alice", "parquet bytes not readable/correct"


if __name__ == "__main__":
    v = load_vectors()
    for runner in (run_lifecycle, run_errors):
        rig = Rig()
        try:
            runner(rig, v)
        finally:
            rig.close()
    test_real_parquet_files_on_disk()
    print(f"PASS — {FORMAT}: conformed to format/v1 over REAL Arrow Flight + real files on disk")
