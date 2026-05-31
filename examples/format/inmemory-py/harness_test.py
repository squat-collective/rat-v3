"""Conformance/golden-data harness for the format/v1 axis — Python side.

This is the ADR-003 cross-run: it loads the SAME language-neutral golden vectors the
Go reference loads (contracts/conformance/format-v1.json) and drives THIS independent
implementation's FormatService through them over real gRPC. Both references passing
the one shared file is what "run against each other on golden data" means literally.

Runs standalone (`python harness_test.py`) or under pytest (`test_*` functions). No
third-party test dep — plain asserts, so the only installs are grpcio + protobuf.

Scope note: like the Go reference, the control plane is what's under test. The bulk
("source"/scan) leg is staged on the plugin's own in-process stream registry — the
real Arrow Flight wire is deferred to a production reference.
"""

import json
import os
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2, data_pb2
from rat.format.v1 import format_pb2, format_pb2_grpc

from server import FormatServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "format-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "format/v1", f'vectors axis = {v["axis"]!r}, want format/v1'
    return v


class Rig:
    """An in-process plugin server + client. Holds the servicer so the harness can
    stage/pull the in-process bulk leg (same process)."""

    def __init__(self) -> None:
        self.servicer = FormatServicer()
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        format_pb2_grpc.add_FormatServiceServicer_to_server(self.servicer, self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = format_pb2_grpc.FormatServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def _ref(self, identifier: str) -> data_pb2.TableRef:
        return data_pb2.TableRef(identifier=identifier)

    def _ctx(self) -> context_pb2.RequestContext:
        return context_pb2.RequestContext()

    def _source(self, rows):
        return self.servicer.streams.put([dict(r) for r in rows])

    def scan(self, table: str):
        resp = self.stub.Resolve(format_pb2.ResolveRequest(context=self._ctx(), table=self._ref(table)))
        return self.servicer.streams.pull(resp.stream)


def _assert_write(result, expect):
    if "rows_affected" in expect:
        assert result.HasField("rows_affected"), f'rows_affected absent, want {expect["rows_affected"]}'
        assert result.rows_affected == expect["rows_affected"], (
            f"rows_affected = {result.rows_affected}, want {expect['rows_affected']}"
        )
    if expect.get("rows_affected_absent"):
        assert not result.HasField("rows_affected"), f"rows_affected = {result.rows_affected}, want absent"
    if expect.get("snapshot_id_set"):
        assert result.snapshot_id != "", "snapshot_id empty, want set"


def _contains_row(rows, want):
    return any(all(r.get(k) == val for k, val in want.items()) for r in rows)


def _assert_scan(rows, expect):
    if "row_count" in expect:
        assert len(rows) == expect["row_count"], f"scan = {len(rows)} rows, want {expect['row_count']}"
    for want in expect.get("rows_contain", []):
        assert _contains_row(rows, want), f"scan rows {rows} missing expected {want}"


def run_lifecycle(rig: Rig, v) -> None:
    table = v["table"]
    for s in v["lifecycle"]:
        op, expect = s["op"], s["expect"]
        if op == "append":
            resp = rig.stub.Append(format_pb2.AppendRequest(
                context=rig._ctx(), table=rig._ref(table), source=rig._source(s["source"])))
            _assert_write(resp.result, expect)
        elif op == "merge":
            resp = rig.stub.Merge(format_pb2.MergeRequest(
                context=rig._ctx(), table=rig._ref(table), merge_keys=s.get("merge_keys", []),
                source=rig._source(s["source"])))
            _assert_write(resp.result, expect)
        elif op == "overwrite":
            resp = rig.stub.Overwrite(format_pb2.OverwriteRequest(
                context=rig._ctx(), table=rig._ref(table), source=rig._source(s["source"])))
            _assert_write(resp.result, expect)
        elif op == "maintain":
            resp = rig.stub.Maintain(format_pb2.MaintainRequest(context=rig._ctx(), table=rig._ref(table)))
            _assert_write(resp.result, expect)
        elif op == "scan":
            _assert_scan(rig.scan(table), expect)
        else:
            raise AssertionError(f"unknown op {op!r}")


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        table = s["table_override"] if "table_override" in s else v["table"]
        want = _CODE[s["expect"]["code"]]
        try:
            if s["op"] == "scan":
                rig.scan(table)
            elif s["op"] == "merge":
                rig.stub.Merge(format_pb2.MergeRequest(
                    context=rig._ctx(), table=rig._ref(table), merge_keys=s.get("merge_keys", []),
                    source=rig._source(s.get("source", []))))
            else:
                raise AssertionError(f"unknown error-op {s['op']!r}")
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status = {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


# pytest entrypoints
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
        run_errors(rig, v)
    finally:
        rig.close()
    print("PASS — rat-format-inmemory-py conformed to format/v1 golden vectors")
