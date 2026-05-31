"""Conformance harness for the scheduler/v1 axis.

Loads contracts/conformance/scheduler-v1.json and drives SchedulerService over real
gRPC: schedules one-shot triggers at offsets relative to now (negative = already due,
positive = future), cancels some, then drains WatchDue and asserts exactly the expected
set fired — proving Schedule/Cancel + the server-streaming WatchDue contract.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
import time
from concurrent import futures

import grpc

from rat.scheduler.v1 import scheduler_pb2, scheduler_pb2_grpc

from server import SchedulerServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "scheduler-v1.json"
)


def _now_ms() -> int:
    return int(time.time() * 1000)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "scheduler/v1", f'vectors axis = {v["axis"]!r}, want scheduler/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        scheduler_pb2_grpc.add_SchedulerServiceServicer_to_server(SchedulerServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = scheduler_pb2_grpc.SchedulerServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)


def run_scenario(rig: Rig, v):
    base = _now_ms()
    for s in v["schedule"]:
        resp = rig.stub.Schedule(scheduler_pb2.ScheduleRequest(
            trigger_id=s["trigger_id"], at_unix_ms=base + s["at_unix_ms_offset"]))
        assert resp.trigger_id == s["trigger_id"], f'Schedule echoed {resp.trigger_id!r}'
    for tid in v.get("cancel", []):
        assert rig.stub.Cancel(scheduler_pb2.CancelRequest(trigger_id=tid)).cancelled, \
            f"Cancel({tid}) reported not-cancelled"
    fired = sorted(r.trigger_id for r in rig.stub.WatchDue(scheduler_pb2.WatchDueRequest()))
    want = sorted(v["expect_due"])
    assert fired == want, f"WatchDue fired {fired}, want {want}"


def test_golden_vectors():
    rig = Rig()
    try:
        run_scenario(rig, load_vectors())
    finally:
        rig.close()


def test_cancel_unknown_is_false():
    rig = Rig()
    try:
        assert not rig.stub.Cancel(scheduler_pb2.CancelRequest(trigger_id="ghost")).cancelled
    finally:
        rig.close()


if __name__ == "__main__":
    rig = Rig()
    try:
        run_scenario(rig, load_vectors())
    finally:
        rig.close()
    test_cancel_unknown_is_false()
    print("PASS — rat-scheduler-inmemory-py conformed to scheduler/v1 golden vectors")
