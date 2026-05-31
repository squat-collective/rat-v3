"""Conformance/golden-data harness for the ui/v1 axis — Python side.

Loads the golden vectors (contracts/conformance/ui-v1.json) and drives this
UiService over real gRPC: Describe (display name + the SET of hosted slot ids),
RenderSlot (asset ref + props schema present for a known component), and the
error path (unknown component → NOT_FOUND).

The ui axis carries no tenant/context, so — unlike the storage harness — the Rig
makes plain RPCs with no rat-callmeta-bin metadata envelope.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.ui.v1 import ui_pb2, ui_pb2_grpc

from server import UiServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "ui-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "ui/v1", f'vectors axis = {v["axis"]!r}, want ui/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        ui_pb2_grpc.add_UiServiceServicer_to_server(UiServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = ui_pb2_grpc.UiServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def describe(self):
        return self.stub.Describe(ui_pb2.DescribeRequest())

    def render_slot(self, slot, component):
        return self.stub.RenderSlot(
            ui_pb2.RenderSlotRequest(slot=slot, component=component)
        )


def run_describe(rig: Rig, v) -> None:
    expect = v["describe"]["expect"]
    resp = rig.describe()
    assert resp.display_name == expect["display_name"], (
        f'display_name = {resp.display_name!r}, want {expect["display_name"]!r}'
    )
    got_slots = {s.slot for s in resp.slots}
    want_slots = set(expect["slots"])
    assert got_slots == want_slots, f"slots = {got_slots!r}, want {want_slots!r}"


def run_render(rig: Rig, v) -> None:
    for s in v["render"]:
        expect = s["expect"]
        resp = rig.render_slot(s["slot"], s["component"])
        if expect.get("asset_ref_present"):
            assert resp.asset_ref, f'{s["step"]}: asset_ref empty, want present'
        if expect.get("props_schema_present"):
            assert resp.props_schema, f'{s["step"]}: props_schema empty, want present'
            schema = json.loads(resp.props_schema.decode("utf-8"))
            assert isinstance(schema, dict), f'{s["step"]}: props_schema is not a JSON object'


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            rig.render_slot(s["slot"], s["component"])
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status = {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


def test_describe_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_describe(rig, v)
    finally:
        rig.close()


def test_render_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_render(rig, v)
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
        run_describe(rig, v)
        run_render(rig, v)
        run_errors(rig, v)
    finally:
        rig.close()
    print("PASS — rat-ui-web-portal-py conformed to ui/v1 golden vectors")
