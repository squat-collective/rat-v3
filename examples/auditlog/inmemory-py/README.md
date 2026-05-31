# `rat-auditlog-inmemory-py` — the `audit-log` sink reference

The control-plane reference for the `auditlog/v1` axis. Unlike most axes, audit emission
is **not optional**: the core always emits a tamper-evident, append-only chain of
security-relevant events — even with no audit-log plugin installed (it falls back to a
core-local store). This axis decides only *where* the trail also goes; the record itself
is core-authored + core-signed ([common/v1/audit.proto](../../../contracts/proto/rat/common/v1/audit.proto)).
One reference + conformance suffices for a control-plane axis
([ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)).

## Capability

| capability | method | what it does |
|---|---|---|
| `rat://auditlog/v1/append` | `Append` | **core-only** — append core-signed records to the sink |

## What the sink enforces (freeze-blocker #4)

The sink holds only the core's **public** key — it can verify but never forge. Every
Append enforces, on the wire:

1. **Signature verify** — Ed25519 over the *pinned canonical serialization* (every field
   except `signature`, deterministic proto3 encoding). Forged → `REJECTED` /
   `BAD_SIGNATURE`.
2. **Chain check** — each record's `prev_hash` must equal the sink's current chain head
   (sha256 of the previous record's canonical bytes). Gap/fork → `REJECTED` /
   `CHAIN_BREAK`.
3. **Prefix-only commit** — once a record in an Append is `REJECTED`, every later record
   in the same request is uncommitted, so a partial failure can't fork the chain.
4. **Idempotent duplicate** — re-appending a stored id acks `DUPLICATE` (not an error),
   the safety valve for the core-only + signed retry model.

The response carries per-record acks + the `last_committed_id`/`last_committed_hash`
watermark a reconnecting emitter resumes from.

## How it's tested

[`auditlog-v1.json`](../../../contracts/conformance/auditlog-v1.json) via `make
conformance`. The harness plays the core: it generates an Ed25519 keypair, builds
chain-linked signed records, and drives Append through all five cases (two valid →
COMMITTED, duplicate → DUPLICATE, forged → BAD_SIGNATURE, gap → CHAIN_BREAK, prefix-only).

## Files

- [`store.py`](store.py) — the verifying, chain-linking sink (+ the canonical-serialization helper)
- [`server.py`](server.py) — the `AuditLogService` gRPC servicer
- [`harness_test.py`](harness_test.py) — the conformance harness (plays the signing core)
- [`main.py`](main.py) — standalone gRPC entrypoint
