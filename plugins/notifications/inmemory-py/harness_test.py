"""Conformance/golden-data harness for the notifications/v1 axis — Python side.

Loads the golden vectors (contracts/conformance/notifications-v1.json) and
drives this independent implementation's NotificationsService.Send over real
gRPC. The notifications axis is a delivery sink, so the harness asserts the
delivery contract: each send case reports delivered + a non-empty message_id,
the shared sink captured exactly the delivered cases, and error cases abort with
the expected gRPC status.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.notifications.v1 import notifications_pb2, notifications_pb2_grpc

from server import NotificationsServicer
from store import NotificationSink

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "notifications-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}


def _severity(name: str):
    return notifications_pb2.Severity.Value(f"SEVERITY_{name}")


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "notifications/v1", f'vectors axis = {v["axis"]!r}, want notifications/v1'
    return v


class Rig:
    def __init__(self, sink: NotificationSink = None) -> None:
        self.sink = sink or NotificationSink()
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        notifications_pb2_grpc.add_NotificationsServiceServicer_to_server(
            NotificationsServicer(self.sink), self.server
        )
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = notifications_pb2_grpc.NotificationsServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def send(self, severity, title, body, target, attributes):
        return self.stub.Send(
            notifications_pb2.SendRequest(
                severity=severity,
                title=title,
                body=body,
                target=target,
                attributes=attributes,
            )
        )


def run_send(rig: Rig, v) -> None:
    for s in v["send"]:
        expect = s["expect"]
        resp = rig.send(
            severity=_severity(s["severity"]),
            title=s["title"],
            body=s.get("body", ""),
            target=s.get("target", ""),
            attributes=s.get("attributes", {}),
        )
        assert resp.delivered == expect["delivered"], (
            f'{s["step"]}: delivered = {resp.delivered}, want {expect["delivered"]}'
        )
        assert resp.message_id, f'{s["step"]}: message_id empty, want non-empty'

    delivered = [s for s in v["send"] if s["expect"]["delivered"]]
    assert len(rig.sink.captured) == len(delivered), (
        f"sink captured {len(rig.sink.captured)}, want {len(delivered)}"
    )
    titles = {c["title"] for c in rig.sink.captured}
    assert "Pipeline succeeded" in titles, "spot-check: 'Pipeline succeeded' not captured"


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            rig.send(
                severity=_severity(s["severity"]),
                title=s["title"],
                body=s.get("body", ""),
                target=s.get("target", ""),
                attributes=s.get("attributes", {}),
            )
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status = {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


def test_send_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_send(rig, v)
    finally:
        rig.close()


def test_error_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_errors(rig, v)
    finally:
        rig.close()


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig()
    try:
        run_send(rig, v)
        run_errors(rig, v)
    finally:
        rig.close()
    print("PASS — rat-notifications-inmemory-py conformed to notifications/v1 golden vectors")
