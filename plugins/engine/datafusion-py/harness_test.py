"""Round-2 conformance harness for a REAL SQL engine (shared by duckdb-py +
datafusion-py — identical; only the imported Engine backend differs).

Loads contracts/conformance/engine-real-v1.json and drives EngineService over real
gRPC: `setup` runs typed DDL/INSERT (asserting rows_affected), `queries` run
Query/Preview and assert the REAL typed-Arrow result (row_count + projected column
set + rows_contain with typed values, read back from the Arrow IPC leg with
pyarrow), `errors` assert gRPC codes. Two independent real engines passing this one
file is ADR-003's two-real-engine cross-run.
"""

import json
import os
from concurrent import futures

import grpc

from rat.engine.v1 import engine_pb2, engine_pb2_grpc

from server import EngineServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "engine-real-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
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

    def query_table(self, req, method):
        """Call Query/Preview, pull the real Arrow result from the IPC leg."""
        resp = method(req)
        return self.servicer.streams.pull(resp.stream)  # pyarrow.Table


def _contains_row(rows, want):
    return any(all(r.get(k) == val for k, val in want.items()) for r in rows)


def _assert_query(table, expect):
    if "row_count" in expect:
        assert table.num_rows == expect["row_count"], f"rows = {table.num_rows}, want {expect['row_count']}"
    if "columns" in expect:
        assert list(table.schema.names) == expect["columns"], (
            f"columns = {list(table.schema.names)}, want {expect['columns']}")
    rows = table.to_pylist()
    for want in expect.get("rows_contain", []):
        assert _contains_row(rows, want), f"rows {rows} missing expected {want}"


def run_conformance(rig: Rig, v) -> None:
    for s in v["setup"]:
        resp = rig.stub.Execute(engine_pb2.ExecuteRequest(sql=s["sql"]))
        assert resp.result.rows_affected == s["rows_affected"], (
            f'{s["sql"]!r}: rows_affected = {resp.result.rows_affected}, want {s["rows_affected"]}')
    for s in v["queries"]:
        if s["op"] == "query":
            tbl = rig.query_table(engine_pb2.QueryRequest(sql=s["sql"]), rig.stub.Query)
        elif s["op"] == "preview":
            tbl = rig.query_table(engine_pb2.PreviewRequest(sql=s["sql"], limit=s.get("limit", 0)), rig.stub.Preview)
        else:
            raise AssertionError(f'unknown op {s["op"]!r}')
        _assert_query(tbl, s["expect"])


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            if s["op"] == "execute":
                rig.stub.Execute(engine_pb2.ExecuteRequest(sql=s["sql"]))
            elif s["op"] == "query":
                rig.stub.Query(engine_pb2.QueryRequest(sql=s["sql"]))
            else:
                raise AssertionError(f'unknown error-op {s["op"]!r}')
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


def test_golden_vectors():
    rig = Rig()
    try:
        run_conformance(rig, load_vectors())
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
        run_conformance(rig, v)
    finally:
        rig.close()
    rig = Rig()
    try:
        run_errors(rig, v)
    finally:
        rig.close()
    from store import ENGINE
    print(f"PASS — {ENGINE}: real engine conformed to engine/v1 real-SQL golden vectors (typed Arrow)")
