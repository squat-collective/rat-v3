"""Conformance/golden-data harness for the engine/v1 axis — Python side.

The ADR-003 cross-run: loads the SAME golden vectors the Go reference loads
(contracts/conformance/engine-v1.json) and drives THIS independent implementation's
EngineService through them over real gRPC. Both references passing the one shared
file is what "run against each other on golden data" means literally.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`). Context
rides in the rat-callmeta-bin metadata header (ADR-007), not the request body.
"""

import json
import os
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.engine.v1 import engine_pb2, engine_pb2_grpc

from server import EngineServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "engine-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "engine/v1", f'vectors axis = {v["axis"]!r}, want engine/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.servicer = EngineServicer()
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        engine_pb2_grpc.add_EngineServiceServicer_to_server(self.servicer, self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = engine_pb2_grpc.EngineServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def _callmeta(self):
        """The rat-callmeta-bin envelope (ADR-007): context in transport metadata,
        not the request body. A well-formed traceparent + caller-supplied tenant."""
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(
                traceparent="00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
                correlation_id="corr-golden",
            ),
            identity=context_pb2.Identity(tenant="acme"),
        )
        return [("rat-callmeta-bin", rc.SerializeToString())]

    def query_rows(self, req, method):
        resp = method(req, metadata=self._callmeta())
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
        assert len(rows) == expect["row_count"], f"query = {len(rows)} rows, want {expect['row_count']}"
    for want in expect.get("rows_contain", []):
        assert _contains_row(rows, want), f"rows {rows} missing expected {want}"
    for r in rows:
        for k in expect.get("rows_exclude_keys", []):
            assert k not in r, f"row {r} should not contain projected-out key {k!r}"


def run_lifecycle(rig: Rig, v) -> None:
    for s in v["lifecycle"]:
        op, expect = s["op"], s["expect"]
        if op == "execute":
            resp = rig.stub.Execute(engine_pb2.ExecuteRequest(sql=s["sql"]), metadata=rig._callmeta())
            _assert_write(resp.result, expect)
        elif op == "query":
            rows = rig.query_rows(engine_pb2.QueryRequest(sql=s["sql"]), rig.stub.Query)
            _assert_scan(rows, expect)
        elif op == "preview":
            rows = rig.query_rows(
                engine_pb2.PreviewRequest(sql=s["sql"], limit=s.get("limit", 0)), rig.stub.Preview)
            _assert_scan(rows, expect)
        else:
            raise AssertionError(f"unknown op {op!r}")


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            if s["op"] == "execute":
                rig.stub.Execute(engine_pb2.ExecuteRequest(sql=s["sql"]), metadata=rig._callmeta())
            elif s["op"] == "query":
                rig.stub.Query(engine_pb2.QueryRequest(sql=s["sql"]), metadata=rig._callmeta())
            else:
                raise AssertionError(f"unknown error-op {s['op']!r}")
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status = {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


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
    print("PASS — rat-engine-inmemory-py conformed to engine/v1 golden vectors")
