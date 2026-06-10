"""Conformance + round-2 semantic harness for rat-state-sqlite-py.

THREE things run here:

 1. The ADR-003 cross-run — loads the SAME shared golden vectors the in-memory
    references load (contracts/conformance/state-v1.json) and drives this
    sqlite-backed StateService through them over real gRPC. A REAL backend passing
    the identical vectors is the actual round-2 ADR-003 evidence (a
    technologically-divergent impl, not another in-memory twin).

 2. test_durability_survives_reopen — write, close the store, reopen the SAME db
    file, read it back. Demonstrates PERSISTENCE, which the in-memory twins cannot.

 3. test_linearizable_cas_one_winner — N threads race a compare-and-set from the
    same expected revision; exactly one COMMITs. The serialization is enforced by
    sqlite (BEGIN IMMEDIATE), not an in-process mutex — the lease primitive the
    in-memory twin could only fake (reviews/06 C-4).

Runs standalone (`python harness_test.py`) or under pytest.
"""

import json
import os
import shutil
import tempfile
import threading
from concurrent import futures

import grpc

from rat.state.v1 import state_pb2, state_pb2_grpc

from server import StateServicer
from store import Store

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
_WTYPE = {"PUT": state_pb2.WatchResponse.TYPE_PUT, "DELETE": state_pb2.WatchResponse.TYPE_DELETE}


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
    """An in-process StateService backed by a fresh sqlite file."""

    def __init__(self) -> None:
        self._dir = tempfile.mkdtemp(prefix="rat-state-sqlite-")
        self.store = Store(os.path.join(self._dir, "state.db"))
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        state_pb2_grpc.add_StateServiceServicer_to_server(StateServicer(self.store), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = state_pb2_grpc.StateServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)
        self.store.close()
        shutil.rmtree(self._dir, ignore_errors=True)

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


def _run(vectors_key):
    rig = Rig()
    try:
        for s in load_vectors()[vectors_key]:
            run_step(rig, s)
    finally:
        rig.close()


# 1. ADR-003 cross-run: the real backend passes the SAME shared vectors.
def test_golden_vectors():
    _run("lifecycle")


def test_error_vectors():
    _run("errors")


# 2. Round-2 property the in-memory twins cannot show: DURABILITY.
def test_durability_survives_reopen():
    d = tempfile.mkdtemp(prefix="rat-state-dur-")
    try:
        path = os.path.join(d, "dur.db")
        s1 = Store(path)
        committed, rev = s1.put("persisted/key", b"hello", 0)
        assert committed and rev == 1
        s1.close()

        s2 = Store(path)  # reopen the SAME file in a fresh store
        found, value, rev = s2.get("persisted/key")
        assert found and value == b"hello" and rev == 1, "state did not survive reopen"
        s2.close()
    finally:
        shutil.rmtree(d, ignore_errors=True)


# 3. Round-2 property: LINEARIZABLE CAS enforced by the backend (not a mutex).
def test_linearizable_cas_one_winner():
    d = tempfile.mkdtemp(prefix="rat-state-cas-")
    try:
        store = Store(os.path.join(d, "cas.db"))
        committed, rev = store.put("lease", b"init", 0)  # seed at revision 1
        assert committed and rev == 1

        n = 16
        barrier = threading.Barrier(n)
        results = [None] * n

        def attempt(i):
            barrier.wait()  # release all threads at once to maximize contention
            try:
                ok, _ = store.put("lease", f"node-{i}".encode(), 1)  # CAS from revision 1
                results[i] = ok
            except Exception:
                results[i] = False

        threads = [threading.Thread(target=attempt, args=(i,)) for i in range(n)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        winners = sum(1 for r in results if r)
        assert winners == 1, f"linearizable CAS: {winners} winners among {n} racers, want exactly 1"
        store.close()
    finally:
        shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    _run("lifecycle")
    _run("errors")
    test_durability_survives_reopen()
    test_linearizable_cas_one_winner()
    print("PASS — rat-state-sqlite-py conformed to state/v1 golden vectors + durability + linearizable CAS")
