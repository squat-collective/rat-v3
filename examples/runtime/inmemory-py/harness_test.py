"""Conformance/golden-data harness for the runtime/v1 axis — Python side.

The ADR-003 cross-run: loads the SAME golden vectors the Go reference loads
(contracts/conformance/runtime-v1.json) and drives THIS independent implementation's
RuntimeService.Execute over real server-streaming gRPC, collecting progress + the
terminal completion.

DIRECT, NOT GATEWAY-MEDIATED: the core gateway's Invoke is unary (invoke.proto) and
cannot mediate a server-streaming capability, so runtime is driven directly (a
contract finding — see ideas/inbox.md). Runs standalone or under pytest.
"""

import json
import os
from concurrent import futures

import grpc

from rat.runtime.v1 import runtime_pb2, runtime_pb2_grpc

from server import RuntimeServicer

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
    """A JSON null/None → empty bytes (the INVALID_ARGUMENT case); otherwise the
    work object serialized to JSON bytes."""
    if work is None:
        return b""
    return json.dumps(work).encode("utf-8")


def _callmeta():
    # Attached for faithfulness (a real call carries the ADR-007 envelope); nothing
    # validates it on this direct path. Built inline to avoid importing context here.
    from rat.common.v1 import context_pb2

    rc = context_pb2.RequestContext(
        trace=context_pb2.TraceContext(
            traceparent="00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
            correlation_id="corr-golden",
        ),
        identity=context_pb2.Identity(tenant="acme"),
    )
    return [("rat-callmeta-bin", rc.SerializeToString())]


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
        """Drive one streaming Execute; return (progress_list, completed_or_None).
        Raises grpc.RpcError if the stream errors."""
        progs, done = [], None
        req = runtime_pb2.ExecuteRequest(work_spec=work_spec)
        for msg in self.stub.Execute(req, metadata=_callmeta()):
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
        assert progs, "final_fraction expected but no progress messages"
        last = progs[-1]
        assert last.HasField("fraction") and last.fraction == e["final_fraction"], (
            f"final fraction = {last.fraction if last.HasField('fraction') else None}, want {e['final_fraction']}"
        )
    if "completed" in e:
        assert done is not None, "no terminal completed message"
        ce = e["completed"]
        assert done.success == ce["success"], f"completed.success = {done.success}, want {ce['success']}"
        if ce.get("error"):
            assert done.error == ce["error"], f"completed.error = {done.error!r}, want {ce['error']!r}"
        if "rows_affected" in ce:
            assert done.result.rows_affected == ce["rows_affected"], (
                f"completed rows_affected = {done.result.rows_affected}, want {ce['rows_affected']}"
            )


def run_lifecycle(rig: Rig, v) -> None:
    for s in v["lifecycle"]:
        progs, done = rig.execute(_work_spec_bytes(s["work"]))
        _assert_execute(progs, done, s["expect"])


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            rig.execute(_work_spec_bytes(s["work"]))
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
    print("PASS — rat-runtime-inmemory-py conformed to runtime/v1 golden vectors")
