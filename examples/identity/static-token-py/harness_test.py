"""Conformance/golden-data harness for the identity/v1 axis — Python side.

The ADR-003 cross-run: loads the golden vectors (contracts/conformance/identity-v1.json)
and drives THIS implementation's IdentityService over real gRPC.

  - authenticate cases call Authenticate(credential) and assert (authenticated,
    subject, tenant).
  - authorize cases set the end-user subject in the rat-callmeta-bin metadata
    header (ADR-007) — the server reads it there, never as a request field — then
    call Authorize(action, resource) and assert (allowed, deny_code). A deny is a
    SUCCESSFUL rpc carrying a machine-readable deny_code (the ERROR MODEL in
    identity.proto), so the harness branches on deny_code, never a free-text reason.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc

from rat.common.v1 import context_pb2
from rat.identity.v1 import identity_pb2, identity_pb2_grpc

from server import IdentityServicer

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "identity-v1.json"
)


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "identity/v1", f'vectors axis = {v["axis"]!r}, want identity/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        identity_pb2_grpc.add_IdentityServiceServicer_to_server(IdentityServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = identity_pb2_grpc.IdentityServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def _callmeta(self, subject: str):
        rc = context_pb2.RequestContext(
            trace=context_pb2.TraceContext(
                traceparent="00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
                correlation_id="corr-golden",
            ),
            identity=context_pb2.Identity(
                subject=context_pb2.SubjectAssertion(principal=subject),
            ),
        )
        return [("rat-callmeta-bin", rc.SerializeToString())]

    def authenticate(self, credential: str):
        return self.stub.Authenticate(
            identity_pb2.AuthenticateRequest(credential=credential.encode("utf-8")),
        )

    def authorize(self, subject: str, action: str, resource: str):
        return self.stub.Authorize(
            identity_pb2.AuthorizeRequest(action=action, resource=resource),
            metadata=self._callmeta(subject),
        )


def run_authenticate(rig: Rig, v) -> None:
    for s in v["authenticate"]:
        expect = s["expect"]
        resp = rig.authenticate(s["credential"])
        assert resp.authenticated == expect["authenticated"], (
            f'{s["step"]}: authenticated = {resp.authenticated}, want {expect["authenticated"]}'
        )
        if expect["authenticated"]:
            assert resp.subject == expect["subject"], (
                f'{s["step"]}: subject = {resp.subject!r}, want {expect["subject"]!r}'
            )
            assert resp.tenant == expect["tenant"], (
                f'{s["step"]}: tenant = {resp.tenant!r}, want {expect["tenant"]!r}'
            )


def run_authorize(rig: Rig, v) -> None:
    for s in v["authorize"]:
        expect = s["expect"]
        resp = rig.authorize(s["subject"], s["action"], s.get("resource", ""))
        assert resp.allowed == expect["allowed"], (
            f'{s["step"]}: allowed = {resp.allowed}, want {expect["allowed"]}'
        )
        if not expect["allowed"]:
            want_code = identity_pb2.DenyCode.Value("DENY_CODE_" + expect["deny_code"])
            assert resp.deny_code == want_code, (
                f'{s["step"]}: deny_code = '
                f'{identity_pb2.DenyCode.Name(resp.deny_code)!r}, '
                f'want DENY_CODE_{expect["deny_code"]}'
            )


def test_authenticate_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_authenticate(rig, v)
    finally:
        rig.close()


def test_authorize_vectors():
    v = load_vectors()
    rig = Rig()
    try:
        run_authorize(rig, v)
    finally:
        rig.close()


def test_subject_from_metadata_not_request():
    """Structural check: the authorize decision tracks the subject in the metadata
    envelope; there is no request field that could override it. An empty subject
    (no metadata principal) is treated as unauthenticated."""
    rig = Rig()
    try:
        resp = rig.authorize("", "pipeline.run", "")
        assert not resp.allowed, "empty-subject authorize must deny"
        assert resp.deny_code == identity_pb2.DenyCode.DENY_CODE_NOT_AUTHENTICATED, (
            f"empty subject deny_code = {identity_pb2.DenyCode.Name(resp.deny_code)!r}, "
            f"want DENY_CODE_NOT_AUTHENTICATED"
        )
    finally:
        rig.close()


if __name__ == "__main__":
    v = load_vectors()
    rig = Rig()
    try:
        run_authenticate(rig, v)
        run_authorize(rig, v)
    finally:
        rig.close()
    test_subject_from_metadata_not_request()
    print("PASS — rat-identity-static-token-py conformed to identity/v1 golden vectors")
