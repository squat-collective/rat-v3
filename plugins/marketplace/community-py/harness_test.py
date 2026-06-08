"""Conformance/golden-data harness for the marketplace/v1 axis — Python side.

Loads the golden vectors (contracts/conformance/marketplace-v1.json) and drives
THIS implementation's MarketplaceService over real gRPC. The load-bearing test is
the capability-aware "works on my deployment?" compatibility filter on Search:
the same query+kind returns a listing only once the deployment advertises every
required capability.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.marketplace.v1 import marketplace_pb2, marketplace_pb2_grpc

from server import MarketplaceServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "marketplace-v1.json"
)

_CODE = {
    "INVALID_ARGUMENT": grpc.StatusCode.INVALID_ARGUMENT,
    "PERMISSION_DENIED": grpc.StatusCode.PERMISSION_DENIED,
    "NOT_FOUND": grpc.StatusCode.NOT_FOUND,
}


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "marketplace/v1", f'vectors axis = {v["axis"]!r}, want marketplace/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        marketplace_pb2_grpc.add_MarketplaceServiceServicer_to_server(
            MarketplaceServicer(), self.server
        )
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = marketplace_pb2_grpc.MarketplaceServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def search(self, query, kind, deployment_capabilities):
        return self.stub.Search(
            marketplace_pb2.SearchRequest(
                query=query, kind=kind, deployment_capabilities=deployment_capabilities
            )
        )

    def get(self, plugin_id):
        return self.stub.Get(marketplace_pb2.GetRequest(plugin_id=plugin_id))


def run_search(rig: Rig, v) -> None:
    for s in v["search"]:
        resp = rig.search(
            s.get("query", ""),
            s.get("kind", ""),
            s.get("deployment_capabilities", []),
        )
        got = {l.plugin_id for l in resp.listings}
        want = set(s["expect"]["plugin_ids"])
        assert got == want, f'{s["step"]}: plugin_ids = {sorted(got)}, want {sorted(want)}'


def run_get(rig: Rig, v) -> None:
    for s in v["get"]:
        listing = rig.get(s["plugin_id"]).listing
        expect = s["expect"]
        assert listing.kind == expect["kind"], (
            f'{s["step"]}: kind = {listing.kind!r}, want {expect["kind"]!r}'
        )
        assert listing.signed == expect["signed"], (
            f'{s["step"]}: signed = {listing.signed}, want {expect["signed"]}'
        )
        # The MANDATORY capability fields (proto N2) must be populated — a listing
        # with no provided capabilities cannot answer "works on your deployment?".
        assert len(listing.provided_capabilities) > 0, (
            f'{s["step"]}: provided_capabilities empty — mandatory field unpopulated'
        )


def run_errors(rig: Rig, v) -> None:
    for s in v["errors"]:
        want = _CODE[s["expect"]["code"]]
        try:
            rig.get(s["plugin_id"])
        except grpc.RpcError as e:
            assert e.code() == want, f'{s["step"]}: status = {e.code()}, want {want}'
        else:
            raise AssertionError(f'{s["step"]}: want error {s["expect"]["code"]}, got success')


def test_search_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_search(rig, v)
    finally:
        rig.close()


def test_get_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_get(rig, v)
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
        run_search(rig, v)
        run_get(rig, v)
        run_errors(rig, v)
    finally:
        rig.close()
    print("PASS — rat-marketplace-community-py conformed to marketplace/v1 golden vectors")
