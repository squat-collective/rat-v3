# `audit-log/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> the reference against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: audit-log` plugin. Pairs with the wire
> contract [`auditlog.proto`](auditlog.proto) + the cross-cutting record type
> [`audit.proto`](../../common/v1/audit.proto) and the golden vectors
> [`auditlog-v1.json`](../../../../conformance/auditlog-v1.json). Status: **v1 (frozen — rat/1, ADR-009)**.

## What an `audit-log` plugin is

A `kind: audit-log` plugin (file, postgres, Splunk, Kafka, immutable object store) is an
**append-only, tamper-evident export sink** for the core's mandatory audit stream. The core
authors, signs, and chain-links every `AuditRecord` (reviews/04 I8); this axis decides
**where the trail goes**, never **whether it exists**. "audit-log: none" must NOT mean "no
audit" — the core always emits, falling back to a core-local append-only store when no
plugin is installed. This is a **cross-cutting enforcement property** of the core
([plugin-architecture.md](../../../../../.claude/rules/plugin-architecture.md)), not a
plugin-axis feature.

**Append is CORE-ONLY.** The `rat://auditlog/v1/append` capability is not grantable to
ordinary plugins. No plugin can inject records, fork the chain, or race `prev_hash`. The
sink verifies but can never forge.

**Tamper-evidence is on the wire.** Four properties enforced by every conformant sink (freeze-blocker #4 / reviews/06 C-3):

1. **Signature verify** — each record carries an Ed25519 signature over its pinned canonical
   serialization (all fields of `AuditRecord` except `signature`, in deterministic proto3
   encoding, ascending field order). A record whose signature does not verify → `REJECTED`.
2. **Hash-chain check** — each record's `prev_hash` must equal the sink's current chain head
   (sha256 of the previous record's canonical bytes). A gap or fork attempt → `REJECTED`.
3. **Prefix-only commit** — once any record in an `Append` is `REJECTED`, all subsequent
   records in the same request are uncommitted. The committed set is always a contiguous
   prefix; the chain cannot fork on partial failure.
4. **Idempotent duplicate** — re-appending an already-stored `id` acks `DUPLICATE`, not an
   error. The chain is intact; retry on transport failure is safe.

**Tail-drop detection** relies on the core-local copy and the `last_committed_id` /
`last_committed_hash` watermark returned by the sink — not on the sink alone. A reconnecting
emitter resumes from the watermark with no gap or fork (reviews/08 E8).

**Audit-on-deny is mandatory** (reviews/07 S3 / `common/v1/audit.proto`): the gateway emits
exactly one `AuditRecord` per enforcement decision, including denials (`AUDIT_OUTCOME_DENIED`
on a failed C2/C5/C7 check). Silent drops on the deny-path are non-conformant.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://auditlog/v1/append` | `Append` | receive core-authored, core-signed records; verify, chain-link, and durably store them |

## The RPCs

- **`Append(records[]) → {acks[], last_committed_id, last_committed_hash}`** — each element
  of `acks` corresponds positionally to the matching element of `records`. Per-record
  disposition is one of:
  - `APPEND_STATUS_COMMITTED` — durably stored and chain-linked.
  - `APPEND_STATUS_DUPLICATE` — already stored (idempotent retry); NOT an error; the chain
    is intact.
  - `APPEND_STATUS_REJECTED` — not stored; `reject_code` identifies the reason. Implies
    prefix-only: every later record in the same request is also uncommitted.

  `RejectCode` values:
  - `REJECT_CODE_BAD_SIGNATURE` — signature did not verify against the core's key.
  - `REJECT_CODE_CHAIN_BREAK` — `prev_hash` does not match the sink's current chain head.
  - `REJECT_CODE_STORAGE_ERROR` — sink-side durable-storage failure (retryable).

  `last_committed_id` and `last_committed_hash` report the chain head after this call.
  Empty strings when no records are yet committed. A reconnecting core emitter uses the
  watermark to resume without gap or fork.

  Empty `records` list → `OK` with empty `acks` and the current watermark. `records`
  referencing an ill-formed `AuditRecord` (missing `id`) → `INVALID_ARGUMENT`.

## Conformance obligations

Pass [`auditlog-v1.json`](../../../../conformance/auditlog-v1.json) via `make conformance`.
The harness plays the core (holds the Ed25519 private key; the sink holds only the public
key), builds and signs records at run time, and asserts the per-step acks. Scenarios gated:

- **`two_valid_chained`** — two well-formed, chain-linked records → both `COMMITTED`.
- **`idempotent_retry`** — re-append the first record → `DUPLICATE`; chain intact.
- **`forged_signature`** — record with a corrupted signature → `REJECTED:BAD_SIGNATURE`.
- **`chain_gap`** — record with a wrong `prev_hash` → `REJECTED:CHAIN_BREAK`.
- **`prefix_only_commit`** — valid + corrupt-sig + dependent: first `COMMITTED`, second
  `REJECTED:BAD_SIGNATURE`, third `REJECTED:CHAIN_BREAK` (prefix-only enforcement).

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/infrastructure failures;
  in-response enum fields (`AppendStatus`, `RejectCode`) for normal domain outcomes (reject,
  duplicate). `REJECT_CODE_STORAGE_ERROR` is retryable — surface as `UNAVAILABLE` only if
  the entire `Append` call fails at the transport layer; per-record storage failures use the
  in-response `REJECTED` + `REJECT_CODE_STORAGE_ERROR` path.

- `RequestContext` rides in the `rat-callmeta-bin` metadata header
  ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)), not a
  field (field 1 in `AppendRequest` is reserved to enforce this). Invocation is
  core-mediated
  ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the plugin implements a plain gRPC `AuditLogService` server.

## Writing a plugin

1. Construct the sink with the core's published **Ed25519 public key** (never the private
   key; the sink verifies, never signs).
2. Implement `AuditLogService.Append` with all four tamper-evidence properties:
   - **Verify** every record's signature over its canonical serialization before committing.
   - **Check** `prev_hash` against the current chain head.
   - **Prefix-only** commit: on the first `REJECTED` record, mark all remaining records in
     the request uncommitted (ack them `REJECTED` with `REJECT_CODE_CHAIN_BREAK`).
   - **Idempotent**: a record whose `id` is already committed acks `DUPLICATE`, returns `OK`.
3. Hold the chain head (`last_committed_hash`) in durable storage — not only in memory —
   so a restart does not silently accept a chain-break.
4. Return `last_committed_id` + `last_committed_hash` after every call (even if no new
   records committed).
5. Pass [`auditlog-v1.json`](../../../../conformance/auditlog-v1.json) via `make conformance`.

**No Arrow / data-plane protocol.** This is a control-plane axis; `Append` is a plain
unary gRPC call. No streaming, no Arrow IPC.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/auditlog/inmemory-py`](../../../../../examples/auditlog/inmemory-py) | 1 (control-plane ref) | all four tamper-evidence properties; canonical-bytes helper; chain-head watermark; prefix-only + idempotent-duplicate in-memory |

## Related

[`auditlog.proto`](auditlog.proto) · [`audit.proto`](../../common/v1/audit.proto) ·
[`auditlog-v1.json`](../../../../conformance/auditlog-v1.json) ·
[`ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[plugin-architecture.md](../../../../../.claude/rules/plugin-architecture.md) (cross-cutting audit enforcement) ·
[reviews/06](../../../../../reviews/06-proto-contract-review.md) C-3 (freeze-blocker #4) ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md) E8 (tail-drop + keyring)
