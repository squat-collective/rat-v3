"""Conformance + round-2 harness for rat-runtime-subprocess-py.

 1. ADR-003 cross-run — loads the SAME shared golden vectors the in-memory runtime
    references load (contracts/conformance/runtime-v1.json) and drives this
    subprocess-backed RuntimeService.Execute over real streaming gRPC. The toy
    work_spec ({steps, rows, indeterminate, fail}) is abstract enough that a real
    child-process runtime interprets it identically — so the same vectors pass.

 2. Round-2 properties the in-thread runtime CANNOT show — REAL process isolation:
    - test_work_runs_in_a_separate_process: the work unit reports a PID != the
      server's own.
    - test_each_work_unit_gets_its_own_process: two Execute calls run in two
      DISTINCT child PIDs (each unit isolated).

Runs standalone (`python harness_test.py`) or under pytest.
"""

import json
import os
from concurrent import futures

import grpc

from rat.runtime.v1 import runtime_pb2, runtime_pb2_grpc

from server import RuntimeServicer, run_worker

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "runtime-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "runtime/v1", f'vectors axis = {v["axis"]!r}, want runtime/v1'
    return v


def _work_spec_bytes(work):
    if work is None:
        return b""
    return json.dumps(work).encode("utf-8")


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        runtime_pb2_grpc.add_RuntimeServiceServicer_to_server(RuntimeServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = runtime_pb2_grpc.RuntimeServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def execute(self, work_spec):
        progs, done = [], None
        for msg in self.stub.Execute(runtime_pb2.ExecuteRequest(work_spec=work_spec)):
            which = msg.WhichOneof("event")
            if which == "progress":
                progs.append(msg.progress)
            elif which == "completed":
                done = msg.completed
        return progs, done


def _assert_execute(progs, done, e):
    if "progress_count" in e:
        assert len(progs) == e["progress_count"], f"progress count = {len(progs)}, want {e['progress_count']}"
    if e.get("progress_has_fraction"):
        for i, p in enumerate(progs):
            assert p.HasField("fraction"), f"progress[{i}] fraction absent, want present"
    if e.get("progress_fraction_absent"):
        for i, p in enumerate(progs):
            assert not p.HasField("fraction"), f"progress[{i}] fraction = {p.fraction}, want absent"
    if "final_fraction" in e:
        assert progs and progs[-1].HasField("fraction") and progs[-1].fraction == e["final_fraction"], (
            f"final fraction mismatch, want {e['final_fraction']}")
    if "completed" in e:
        assert done is not None, "no terminal completed message"
        ce = e["completed"]
        assert done.success == ce["success"], f"completed.success = {done.success}, want {ce['success']}"
        if ce.get("error"):
            assert done.error == ce["error"], f"completed.error = {done.error!r}, want {ce['error']!r}"
        if "rows_affected" in ce:
            assert done.result.rows_affected == ce["rows_affected"], (
                f"rows_affected = {done.result.rows_affected}, want {ce['rows_affected']}")


# 1. ADR-003 cross-run.
def test_golden_vectors():
    rig = Rig()
    try:
        for s in load_vectors()["lifecycle"]:
            progs, done = rig.execute(_work_spec_bytes(s["work"]))
            _assert_execute(progs, done, s["expect"])
    finally:
        rig.close()


def test_error_vectors():
    rig = Rig()
    try:
        for s in load_vectors()["errors"]:
            want = _CODE[s["expect"]["code"]]
            try:
                rig.execute(_work_spec_bytes(s["work"]))
            except grpc.RpcError as e:
                assert e.code() == want, f'{s["step"]}: status {e.code()}, want {want}'
            else:
                raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')
    finally:
        rig.close()


def _completed_pid(spec):
    events, _ = run_worker(json.dumps(spec))
    completed = [e for e in events if e["t"] == "completed"][0]
    return completed["pid"]


# 2. Round-2: real process isolation.
def test_work_runs_in_a_separate_process():
    pid = _completed_pid({"steps": 1, "rows": 5})
    assert pid > 0 and pid != os.getpid(), "work did not run in a separate process"


def test_each_work_unit_gets_its_own_process():
    pid1 = _completed_pid({"steps": 1, "rows": 1})
    pid2 = _completed_pid({"steps": 1, "rows": 1})
    assert pid1 != os.getpid() and pid2 != os.getpid()
    assert pid1 != pid2, "two work units shared a process — no per-unit isolation"


if __name__ == "__main__":
    test_golden_vectors()
    test_error_vectors()
    test_work_runs_in_a_separate_process()
    test_each_work_unit_gets_its_own_process()
    print("PASS — rat-runtime-subprocess-py conformed to runtime/v1 golden vectors + process isolation")
