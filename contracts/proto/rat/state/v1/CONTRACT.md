# `state/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugins here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: state-backend` plugin. Pairs with the
> wire contract [`state.proto`](state.proto) and the golden vectors
> [`state-v1.json`](../../../../conformance/state-v1.json). Status: **v1 (frozen — rat/1, ADR-009)**.

## What a `state-backend` plugin is

A `state-backend` plugin (sqlite, postgres, etcd, …) backs the core's **State
Gateway** — one of the six core things. It is a **tier-0** plugin: the core cannot
start without one (selected at boot, not hot-swappable). It stores opaque key/value
state on behalf of every other plugin, namespaced per-plugin and per-tenant.

## Capabilities

| capability URI | method | cardinality | what it does |
|---|---|---|---|
| `rat://state/v1/get` | `Get` | unary | read one key |
| `rat://state/v1/put` | `Put` | unary | write one key, optionally compare-and-set |
| `rat://state/v1/list` | `List` | unary | list keys under a prefix |
| `rat://state/v1/watch` | `Watch` | server-streaming | stream changes under a prefix |

## The RPCs

- **`Get(key)` → `{found, value, revision}`** — `found=false` + `revision=0` for a
  missing key. `revision` is the monotonic version for CAS.
- **`Put(key, value, if_revision)` → `{outcome, revision}`** — `if_revision=0` is an
  unconditional write; `>0` is compare-and-set. The outcome is the **`PutOutcome`
  enum**, NOT a gRPC error: `COMMITTED` (new `revision`), `CONFLICT` (current revision
  ≠ `if_revision`; `revision` carries the conflicting value; the write did NOT happen),
  `UNKNOWN` (the backend could not confirm — a lease renewal that returns UNKNOWN
  cannot be relied on for fencing). A CAS conflict is a *normal outcome*.
- **`List(prefix)` → `{keys}`** — `prefix` MAY be empty (== this plugin+tenant
  namespace).
- **`Watch(prefix, from_revision)` → stream `{type, key, value, revision}`** —
  `from_revision=0` == from now. Events MUST be delivered in revision order.

## Conformance obligations (NOT honour-system)

1. **Key grammar** (freeze-blocker #3 / SEC-2) — reject any `key`/`prefix` that is
   empty (key only), exceeds 512 bytes, is not valid UTF-8, contains a NUL/ASCII
   control char (< 0x20), or contains a `.`/`..` path component or `../`/`..\` →
   `INVALID_ARGUMENT`. This makes the gateway's namespace prefixing a real boundary,
   not naive string concat a crafted key can escape.
2. **Linearizable CAS + ordered Watch** (reviews/06 C-4) — to be eligible as the
   reconciler's leader-election lease backend, single-key CAS MUST be **linearizable**
   and Watch **ordered**. A backend that can only offer eventually-consistent reads
   MUST run in a strongly-consistent mode or declare itself solo-only (two stale CAS
   reads → two leaders → split-brain). This is gated by golden vectors, not a comment.
3. Both above are exercised by [`state-v1.json`](../../../../conformance/state-v1.json):
   the CAS COMMITTED/CONFLICT path + the 6 key-grammar rejections.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (CAS conflict, read-miss).

- **Context is in metadata, not the body** — `RequestContext` (trace + identity +
  tenant) rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  never a request field. The state gateway namespaces by `identity.caller_plugin` +
  `identity.tenant` (the C3 isolation boundary) — both server-stamped, never trusted
  from the wire.
- **Invocation is core-mediated** — callers reach you via the core's
  `CapabilityInvokeService` ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  `Watch` is mediated by `InvokeServerStream` ([ADR-008](../../../../../docs/architecture/adrs/008-streaming-capability-invocation.md)).
  You implement a plain gRPC `StateService` server; the gateway routes by capability.

## Writing a plugin

1. Generate the SDK (`make gen-sdks`) and implement `StateService` (Get/Put/List/Watch)
   against your backend.
2. Enforce the key grammar on every key/prefix → `INVALID_ARGUMENT`.
3. Return the `PutOutcome` enum for CAS (never a gRPC error for a conflict).
4. Pass [`state-v1.json`](../../../../conformance/state-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/state/inmemory-go`](../../../../../plugins/state/inmemory-go), [`inmemory-py`](../../../../../plugins/state/inmemory-py) | 1 (wire) | two language code paths conform to the wire contract |
| [`plugins/state/sqlite-py`](../../../../../plugins/state/sqlite-py) | 2 (real) | sqlite — DURABILITY (survives reopen) + LINEARIZABLE CAS (concurrent CAS via `BEGIN IMMEDIATE`, exactly one winner) |

## Related

[`state.proto`](state.proto) · [`state-v1.json`](../../../../conformance/state-v1.json) ·
ADR-002 D5 (leader election) · [reviews/06](../../../../../reviews/06-proto-contract-review.md) C-4 / SEC-2 ·
[plugin-architecture.md](../../../../../.claude/rules/plugin-architecture.md) (tier-0)
