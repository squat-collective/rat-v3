"""In-memory audit-log SINK for rat-auditlog-inmemory-py — the `audit-log` reference.

The audit RECORD is core-authored + core-signed (common/v1/audit.proto); this axis is
only the export sink (auditlog.proto). The sink's whole job is to store the core's chain
WITHOUT being able to forge/reorder/drop it undetectably. This reference enforces all
four freeze-blocker-#4 properties on the wire:

  1. signature verify — Ed25519 over the PINNED canonical serialization (all fields
     except `signature`, deterministic proto3 encoding). A bad signature → REJECTED.
  2. chain check — each record's `prev_hash` must equal the sink's current chain head
     (sha256 of the previous record's canonical bytes). A gap/fork → REJECTED.
  3. prefix-only commit — once a record in an Append is REJECTED, every later record in
     the same request is uncommitted (acked REJECTED), so the chain can't fork.
  4. idempotent duplicate — re-appending an already-stored id acks DUPLICATE (not an
     error; the chain is intact).

The sink is constructed with the core's published Ed25519 PUBLIC key; it can VERIFY but
never FORGE.
"""

import hashlib
import threading

from cryptography.exceptions import InvalidSignature

from rat.auditlog.v1 import auditlog_pb2
from rat.common.v1 import audit_pb2


def canonical_bytes(record) -> bytes:
    """The pinned canonical serialization the signature covers (audit.proto): the
    deterministic proto3 wire encoding of every field EXCEPT `signature`."""
    r = audit_pb2.AuditRecord()
    r.CopyFrom(record)
    r.ClearField("signature")
    return r.SerializeToString(deterministic=True)


def chain_hash(record) -> str:
    return hashlib.sha256(canonical_bytes(record)).hexdigest()


class AuditSink:
    def __init__(self, public_key) -> None:
        self._pub = public_key            # cryptography Ed25519PublicKey
        self._lock = threading.Lock()
        self._head_hash = ""              # hex sha256 of the last committed record
        self._committed_ids = set()
        self.records = []                 # committed records, in chain order

    def _verifies(self, record) -> bool:
        try:
            self._pub.verify(record.signature, canonical_bytes(record))
            return True
        except InvalidSignature:
            return False

    def append(self, records):
        """Returns (acks, last_committed_id, last_committed_hash)."""
        acks = []
        rejected = False
        with self._lock:
            for rec in records:
                if rejected:  # prefix-only: nothing after a rejection commits
                    acks.append(_ack(rec.id, auditlog_pb2.APPEND_STATUS_REJECTED,
                                     auditlog_pb2.REJECT_CODE_CHAIN_BREAK))
                    continue
                if rec.id in self._committed_ids:  # idempotent retry — chain intact
                    acks.append(_ack(rec.id, auditlog_pb2.APPEND_STATUS_DUPLICATE))
                    continue
                if not self._verifies(rec):
                    rejected = True
                    acks.append(_ack(rec.id, auditlog_pb2.APPEND_STATUS_REJECTED,
                                     auditlog_pb2.REJECT_CODE_BAD_SIGNATURE))
                    continue
                if rec.prev_hash != self._head_hash:
                    rejected = True
                    acks.append(_ack(rec.id, auditlog_pb2.APPEND_STATUS_REJECTED,
                                     auditlog_pb2.REJECT_CODE_CHAIN_BREAK))
                    continue
                # commit
                self._head_hash = chain_hash(rec)
                self._committed_ids.add(rec.id)
                self.records.append(rec)
                acks.append(_ack(rec.id, auditlog_pb2.APPEND_STATUS_COMMITTED))
            last_id = self.records[-1].id if self.records else ""
            return acks, last_id, self._head_hash


def _ack(rec_id, status, reject_code=auditlog_pb2.REJECT_CODE_UNSPECIFIED):
    return auditlog_pb2.RecordAck(id=rec_id, status=status, reject_code=reject_code)
