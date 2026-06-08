"""Conformance harness for the auditlog/v1 axis.

This harness plays the CORE: it generates an Ed25519 keypair, builds chain-linked,
core-signed AuditRecords (signature over the pinned canonical serialization), and drives
the sink's Append over real gRPC — asserting the four freeze-blocker-#4 properties:
COMMITTED for valid chained records, DUPLICATE for an idempotent retry, REJECTED+
BAD_SIGNATURE for a forged record, REJECTED+CHAIN_BREAK for a gap, and prefix-only commit
(nothing after a rejection commits). The sink holds only the PUBLIC key — it verifies,
never forges.

The scenario is crypto-intrinsic (records must be signed at run time), so it's built in
code; the golden file pins the axis + documents the expected acks.

Runs standalone (`python harness_test.py`) or under pytest (`test_*`).
"""

import json
import os
from concurrent import futures

import grpc
from cryptography.hazmat.primitives.asymmetric import ed25519

from rat.auditlog.v1 import auditlog_pb2, auditlog_pb2_grpc
from rat.common.v1 import audit_pb2

from server import AuditLogServicer
from store import AuditSink, canonical_bytes, chain_hash

VECTOR_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "contracts", "conformance", "auditlog-v1.json"
)
WRONG_HASH = "00" * 32  # a prev_hash that matches no real chain head


def load_vectors():
    with open(VECTOR_PATH, "r", encoding="utf-8") as f:
        v = json.load(f)
    assert v["axis"] == "auditlog/v1", f'vectors axis = {v["axis"]!r}, want auditlog/v1'
    return v


class Rig:
    def __init__(self) -> None:
        self.priv = ed25519.Ed25519PrivateKey.generate()
        self.sink = AuditSink(self.priv.public_key())
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
        auditlog_pb2_grpc.add_AuditLogServiceServicer_to_server(AuditLogServicer(self.sink), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.stub = auditlog_pb2_grpc.AuditLogServiceStub(self.channel)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(None)

    def record(self, rec_id, prev_hash, action, outcome, corrupt_sig=False):
        rec = audit_pb2.AuditRecord(
            id=rec_id, prev_hash=prev_hash, timestamp_unix_ms=1_700_000_000_000,
            subject="alice", tenant="acme", action=action, resource="",
            outcome=outcome, correlation_id="corr-1", key_id="core-key-1")
        sig = b"\x00" * 64 if corrupt_sig else self.priv.sign(canonical_bytes(rec))
        rec.signature = sig
        return rec

    def append(self, records):
        return self.stub.Append(auditlog_pb2.AppendRequest(records=records))


def _status(ack):
    name = auditlog_pb2.AppendStatus.Name(ack.status).replace("APPEND_STATUS_", "")
    if ack.status == auditlog_pb2.APPEND_STATUS_REJECTED:
        return f"REJECTED:{auditlog_pb2.RejectCode.Name(ack.reject_code).replace('REJECT_CODE_', '')}"
    return name


def run_scenario(rig: Rig):
    S = audit_pb2.AUDIT_OUTCOME_SUCCESS
    D = audit_pb2.AUDIT_OUTCOME_DENIED

    # 1) two valid chained records → both COMMITTED; watermark = hash(r1)
    r0 = rig.record("a1", "", "plugin.install", S)
    r1 = rig.record("a2", chain_hash(r0), "binding.change", S)
    resp = rig.append([r0, r1])
    assert [_status(a) for a in resp.acks] == ["COMMITTED", "COMMITTED"], [_status(a) for a in resp.acks]
    assert resp.last_committed_id == "a2"
    assert resp.last_committed_hash == chain_hash(r1)
    head = chain_hash(r1)

    # 2) idempotent retry of a1 → DUPLICATE (chain intact, head unchanged)
    resp = rig.append([rig.record("a1", "", "plugin.install", S)])
    assert [_status(a) for a in resp.acks] == ["DUPLICATE"], [_status(a) for a in resp.acks]
    assert resp.last_committed_hash == head

    # 3) forged record (valid chain position, corrupt signature) → REJECTED BAD_SIGNATURE
    resp = rig.append([rig.record("a3", head, "plugin.install", D, corrupt_sig=True)])
    assert [_status(a) for a in resp.acks] == ["REJECTED:BAD_SIGNATURE"], [_status(a) for a in resp.acks]

    # 4) gap (wrong prev_hash, valid signature) → REJECTED CHAIN_BREAK
    resp = rig.append([rig.record("a4", WRONG_HASH, "binding.change", S)])
    assert [_status(a) for a in resp.acks] == ["REJECTED:CHAIN_BREAK"], [_status(a) for a in resp.acks]

    # 5) prefix-only: good commits, bad rejects, everything after the rejection is uncommitted
    g = rig.record("a5", head, "credential.vend", S)
    bad = rig.record("a6", chain_hash(g), "credential.vend", S, corrupt_sig=True)
    after = rig.record("a7", chain_hash(g), "credential.vend", S)
    resp = rig.append([g, bad, after])
    assert [_status(a) for a in resp.acks] == ["COMMITTED", "REJECTED:BAD_SIGNATURE", "REJECTED:CHAIN_BREAK"], \
        [_status(a) for a in resp.acks]
    assert resp.last_committed_id == "a5"


def test_golden_vectors():
    load_vectors()
    rig = Rig()
    try:
        run_scenario(rig)
    finally:
        rig.close()


if __name__ == "__main__":
    load_vectors()
    rig = Rig()
    try:
        run_scenario(rig)
    finally:
        rig.close()
    print("PASS — rat-auditlog-inmemory-py conformed to auditlog/v1 golden vectors")
