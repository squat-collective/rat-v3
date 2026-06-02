"""Conformance harness for rat-engine-duckdb-ml-py.

Two suites, both offline-capable (no extension install needed):

  1. engine-real-v1 — the SAME real-SQL golden vectors duckdb-py/datafusion-py pass
     (contracts/conformance/engine-real-v1.json). Proves the ML extension did NOT
     regress the engine: duckdb-ml is still a conformant `rat://engine/v1` engine
     returning typed Arrow.

  2. engine-embed-v1 — deterministic golden vectors for the embed() UDF
     (contracts/conformance/engine-embed-v1.json, README §10 Q7). For each case it
     runs `SELECT embed(text, model) AS v` over real gRPC, pulls the FLOAT[] back from
     the Arrow leg, and asserts: dim == 256, the nonzero buckets match the frozen
     golden exactly (float32 tolerance), and a non-empty input is L2-normalized. This
     makes the ML UDF as deterministically tested as every other capability.

The embed() UDF is a pure Python feature-hash (embed.py) — no model weights, no
network — so the golden is reproducible anywhere.
"""

import json
import math
import os
from concurrent import futures

import grpc

from rat.engine.v1 import engine_pb2, engine_pb2_grpc

from server import EngineServicer

_HERE = os.path.dirname(__file__)
_CONF = os.path.join(_HERE, "..", "..", "..", "contracts", "conformance")
REAL_VECTORS = os.path.join(_CONF, "engine-real-v1.json")
EMBED_VECTORS = os.path.join(_CONF, "engine-embed-v1.json")

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
}


def _load(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


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
        resp = method(req)
        return self.servicer.streams.pull(resp.stream)  # pyarrow.Table


# ---- suite 1: engine-real-v1 (unchanged real-SQL conformance) --------------------

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


def run_real(rig, v):
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


def run_errors(rig, v):
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


# ---- suite 2: engine-embed-v1 (embed() UDF determinism) --------------------------

def _sql_str(s):
    return "'" + s.replace("'", "''") + "'"


def run_embed(rig, v):
    dim = v["dim"]
    for case in v["cases"]:
        sql = f"SELECT embed({_sql_str(case['text'])}, {_sql_str(case['model'])}) AS v"
        tbl = rig.query_table(engine_pb2.QueryRequest(sql=sql), rig.stub.Query)
        got = tbl.to_pylist()[0]["v"]
        assert got is not None and len(got) == dim, (
            f"embed({case['text']!r}) dim = {None if got is None else len(got)}, want {dim}")
        expected = [0.0] * dim
        for idx, val in case["nonzero"].items():
            expected[int(idx)] = val
        for i, (g, e) in enumerate(zip(got, expected)):
            assert math.isclose(g, e, rel_tol=1e-5, abs_tol=1e-6), (
                f"embed({case['text']!r})[{i}] = {g}, want {e}")
        norm = math.sqrt(sum(x * x for x in got))
        if case["nonzero"]:  # non-empty input is L2-normalized
            assert math.isclose(norm, 1.0, rel_tol=1e-5, abs_tol=1e-6), (
                f"embed({case['text']!r}) norm = {norm}, want ~1.0")
        else:                # empty input -> zero vector
            assert norm == 0.0, f"empty embed norm = {norm}, want 0"


# ---- pytest entrypoints ----------------------------------------------------------

def test_engine_real_golden_vectors():
    rig = Rig()
    try:
        run_real(rig, _load(REAL_VECTORS))
    finally:
        rig.close()


def test_engine_real_error_vectors():
    rig = Rig()
    try:
        run_errors(rig, _load(REAL_VECTORS))
    finally:
        rig.close()


def test_embed_golden_vectors():
    rig = Rig()
    try:
        run_embed(rig, _load(EMBED_VECTORS))
    finally:
        rig.close()


if __name__ == "__main__":
    for fn in (run_real, run_errors):
        rig = Rig()
        try:
            fn(rig, _load(REAL_VECTORS))
        finally:
            rig.close()
    rig = Rig()
    try:
        run_embed(rig, _load(EMBED_VECTORS))
    finally:
        rig.close()
    from store import ENGINE
    print(f"PASS — {ENGINE}: engine-real-v1 (typed Arrow) + engine-embed-v1 (embed() golden) conformant")
