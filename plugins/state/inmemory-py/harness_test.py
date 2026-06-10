"""Conformance/golden-data harness for the state/v1 axis — Python side.

The ADR-003 cross-run: loads the SAME golden vectors the Go reference loads
(contracts/conformance/state-v1.json) and drives THIS independent implementation's
StateService over real gRPC (Get/Put/List + a streaming Watch). The lifecycle is
STATEFUL; the errors array exercises the KEY GRAMMAR. Bad keys are built from
key_len / key_inject so the vector file stays pure-ASCII. Driven directly (no
gateway), like the other Python references. Runs standalone or under pytest.
"""

import json
import os
from concurrent import futures

import grpc

from rat.state.v1 import state_pb2, state_pb2_grpc

from server import StateServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "state-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
    "FAILED_PRECONDITION": grpc.StatusCode.FAILED_PRECONDITION,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
}

_OUTCOME = {
    "COMMITTED": state_pb2.PutOutcome.PUT_OUTCOME_COMMITTED,
    "CONFLICT": state_pb2.PutOutcome.PUT_OUTCOME_CONFLICT,
    "UNKNOWN": state_pb2.PutOutcome.PUT_OUTCOME_UNKNOWN,
}

_WTYPE = {
    "PUT": state_pb2.WatchResponse.TYPE_PUT,
    "DELETE": state_pb2.WatchResponse.TYPE_DELETE,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "state/v1", f'vectors axis = {v["axis"]!r}, want state/v1'
    return v


def _resolve_key(s):
    if "key_len" in s:
        return "a" * s["key_len"]
    if "key_inject" in s:
        return "a" + chr(s["key_inject"]) + "b"
    return s.get("key", "")


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        state_pb2_grpc.add_StateServiceServicer_to_server(StateServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = state_pb2_grpc.StateServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def do(self, s):
        op = s["op"]
        if op == "get":
            return self.stub.Get(state_pb2.GetRequest(key=_resolve_key(s)))
        if op == "put":
            return self.stub.Put(state_pb2.PutRequest(
                key=_resolve_key(s), value=s.get("value", "").encode("utf-8"),
                if_revision=s.get("if_revision", 0)))
        if op == "list":
            return self.stub.List(state_pb2.ListRequest(prefix=s.get("prefix", "")))
        if op == "watch":
            return list(self.stub.Watch(
                state_pb2.WatchRequest(prefix=s.get("prefix", ""), from_revision=s.get("from_revision", 0))))
        if op == "create-if-absent":
            return self.stub.CreateIfAbsent(state_pb2.CreateIfAbsentRequest(
                key=_resolve_key(s), value=s.get("value", "").encode("utf-8")))
        raise AssertionError(f'unknown op {op!r}')


def _assert_success(s, resp):
    e, op = s["expect"], s["op"]
    if op == "get":
        if "found" in e:
            assert resp.found == e["found"], f'found = {resp.found}, want {e["found"]}'
        if "value" in e:
            assert resp.value.decode("utf-8") == e["value"], f'value = {resp.value!r}, want {e["value"]!r}'
        if "revision" in e:
            assert resp.revision == e["revision"], f'revision = {resp.revision}, want {e["revision"]}'
    elif op in ("put", "create-if-absent"):
        if "outcome" in e:
            assert resp.outcome == _OUTCOME[e["outcome"]], f'outcome = {resp.outcome}, want {e["outcome"]}'
        if "revision" in e:
            assert resp.revision == e["revision"], f'revision = {resp.revision}, want {e["revision"]}'
    elif op == "list":
        assert list(resp.keys) == e["keys"], f'keys = {list(resp.keys)}, want {e["keys"]}'
    elif op == "watch":
        want = e["watch_events"]
        assert len(resp) == len(want), f'watch events = {len(resp)}, want {len(want)}'
        for ev, w in zip(resp, want):
            assert ev.type == _WTYPE[w["type"]] and ev.key == w["key"] and ev.revision == w["revision"], (
                f'event = {{{ev.type} {ev.key} rev {ev.revision}}}, want {w}')


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
    print("PASS — rat-state-inmemory-py conformed to state/v1 golden vectors")
