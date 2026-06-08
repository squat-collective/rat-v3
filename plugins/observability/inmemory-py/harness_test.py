"""Conformance harness for the observability/v1 axis.

Loads contracts/conformance/observability-v1.json and drives the BIDI Ingest stream:
sends each batch of telemetry points and asserts the per-batch IngestResponse carries
the expected CUMULATIVE accepted/rejected counts (unnamed points are rejected). Proves
the bidi shape gives incremental, cumulative acks rather than one terminal ack.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.observability.v1 import observability_pb2, observability_pb2_grpc

from server import ObservabilityServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "observability-v1.json"
)

_SIGNAL = {
    "METRIC": observability_pb2.SignalType.SIGNAL_TYPE_METRIC,
    "SPAN": observability_pb2.SignalType.SIGNAL_TYPE_SPAN,
    "LOG": observability_pb2.SignalType.SIGNAL_TYPE_LOG,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "observability/v1", f'vectors axis = {v["axis"]!r}, want observability/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        observability_pb2_grpc.add_ObservabilityServiceServicer_to_server(ObservabilityServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = observability_pb2_grpc.ObservabilityServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)


def _request(batch):
    points = [observability_pb2.TelemetryPoint(
        type=_SIGNAL[p["type"]], name=p["name"], value=p.get("value", 0.0)) for p in batch["points"]]
    return observability_pb2.IngestRequest(points=points)


def run_scenario(rig: Rig, v):
    batches = v["batches"]
    responses = list(rig.stub.Ingest(iter([_request(b) for b in batches])))
    assert len(responses) == len(batches), f"got {len(responses)} acks, want {len(batches)}"
    for i, (b, resp) in enumerate(zip(batches, responses)):
        e = b["expect"]
        assert resp.accepted == e["accepted"], f"batch {i}: accepted={resp.accepted}, want {e['accepted']}"
        assert resp.rejected == e["rejected"], f"batch {i}: rejected={resp.rejected}, want {e['rejected']}"


def test_golden_vectors():
    rig = Rig()
    try:
        run_scenario(rig, load_vectors())
    finally:
        rig.close()


if __name__ == "__main__":
    rig = Rig()
    try:
        run_scenario(rig, load_vectors())
    finally:
        rig.close()
    print("PASS — rat-observability-inmemory-py conformed to observability/v1 golden vectors")
