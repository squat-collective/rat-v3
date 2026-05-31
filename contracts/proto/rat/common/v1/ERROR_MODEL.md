# `common/v1` — the RAT error model (pinned at `rat/1`)

> **Status: frozen with `rat/1`.** This is the canonical, cross-axis convention every RAT
> plugin and the core gateway follow for reporting failure. It resolves freeze-review finding
> **M1** ([reviews/07](../../../../../reviews/07-freeze-review.md)): the codes were used
> consistently across the axes but the *rule* lived in scattered prose. Every axis
> [`CONTRACT.md`](.) and [`core/v1/invoke.proto`](../../core/v1/invoke.proto) references THIS
> file. gRPC status codes are not proto messages, so there is nothing for `buf` to freeze —
> the freeze is this committed, referenced document being part of the `v1` surface.

## The two-layer rule (the load-bearing distinction)

A failure is reported in exactly one of two ways, and **which one is itself part of the contract**:

1. **Domain outcome → a field in the response message** (an enum or a `bool`), NEVER a gRPC status.
   Use this when the "failure" is a **normal, expected result of correct usage** that the caller
   must branch on without exception semantics. The call *succeeded*; the outcome is data.
2. **gRPC status code → the RPC fails.** Use this for malformed requests, authorization
   failures, missing resources the caller asserted exist, unmet preconditions, and infrastructure
   errors. The call did not produce a domain result.

Getting this split wrong is a wire-shape regret (a `bool` that should have been a status, or a
status that should have been an enum, cannot be changed after freeze). The canonical cases:

| Situation | Layer | Shape |
|---|---|---|
| CAS revision mismatch on `state.Put` | outcome | `PutOutcome.CONFLICT` (the conflicting `revision` is returned) |
| `state.Get` of an unset key | outcome | `found = false`, `revision = 0` |
| `secret.Resolve` of an absent **or forbidden** ref | outcome | `found = false` (anti-enumeration — see below) |
| `catalog.MergeBranch` retry that already committed | outcome | `already_applied = true` |
| `catalog.GetTable` of a named, missing table | status | `NOT_FOUND` |
| `catalog.MergeBranch` whose `expected_into_snapshot` no longer matches | status | `FAILED_PRECONDITION` |

## The status-code table (when the RPC fails)

The full set a conformant plugin/gateway may return, and what each MUST mean. A plugin MUST NOT
use a code for a meaning other than the one below; an impl returning a different code for the same
failure as another impl of the same axis is **non-conformant** even if the proto allows it.

| gRPC code | Meaning in RAT | Caller action | Examples |
|---|---|---|---|
| `INVALID_ARGUMENT` | The request is malformed or violates a stated grammar/validation rule. A caller bug; retrying the identical request will fail identically. | Fix the request. | state key grammar violation (empty / >512B / non-UTF-8 / NUL / `.`·`..` traversal); engine empty or unparseable SQL; engine query of an unknown *column*/table within the SQL. |
| `NOT_FOUND` | A **named resource the caller asserted should exist** does not. Used only when absence is genuinely an error (not normal control flow, not a security-sensitive enumeration). | Treat as a real miss; do not retry blindly. | `catalog.GetTable` of an unknown identifier. |
| `FAILED_PRECONDITION` | The request is well-formed but the **current system state** forbids it right now. Distinct from `INVALID_ARGUMENT` (the request is fine) and from a domain outcome (this aborts the call). | Re-read state, then retry. | `catalog.MergeBranch` optimistic-concurrency guard (`expected_into_snapshot` ≠ current). |
| `PERMISSION_DENIED` | The **authenticated** caller is not authorized for this operation/resource. | Do not retry without different authority. | C5: capability not in caller's manifest `requires`; C7: `storage.VendCredentials` for a prefix outside the caller's tenant scope; any cross-tenant access. |
| `UNAUTHENTICATED` | No / invalid caller credential on the channel (C2). The gateway raises this before any axis logic. | Re-authenticate. | missing or unverifiable channel credential / `rat-callmeta-bin`. |
| `ALREADY_EXISTS` | A create would collide with an existing named resource, and the caller did **not** opt into idempotent/upsert semantics. | Read the existing resource or use the idempotent path. | `catalog.CreateBranch` of an existing branch (reserved — not yet exercised by a vector). |
| `RESOURCE_EXHAUSTED` | A quota / rate / size limit was hit. | Back off; retry later or smaller. | oversized payload; per-tenant rate limit. |
| `UNAVAILABLE` | The provider/backend is transiently unreachable. **Retryable** as-is. | Retry with backoff. | backend connection refused; provider draining. |
| `DEADLINE_EXCEEDED` | The gRPC deadline elapsed (the authoritative deadline — `RequestContext.deadline_unix_ms` is only a soft hint). | Retry if idempotent. | slow backend past the call deadline. |
| `INTERNAL` | An unexpected provider-side error (a bug). | File a defect; do not loop. | unhandled panic / invariant violation in a plugin. |
| `UNIMPLEMENTED` | The provider does not implement this method/capability. With capability negotiation this should be unreachable (the registry won't route an unprovided capability), so it is a wiring error if seen. | Treat as a registry/manifest mismatch. | calling a capability the provider does not `provide`. |

Codes not in this table MUST NOT be used. `OK` is success — including a success that carries a
domain-outcome field signalling `CONFLICT` / `found=false` / `already_applied`.

## The not-found rule (freeze-review M2 — the one settled inconsistency)

"Resource absent" is deliberately modeled **two** ways, governed by this rule:

- **`found: bool` (or an equivalent domain field), call returns `OK`** when **either**:
  - **(a)** absence is a *normal, expected* control-flow result the caller routinely branches on
    (`state.Get` of a key that simply isn't set yet), **or**
  - **(b)** distinguishing "absent" from "exists-but-forbidden" would **leak information**
    (`secret.Resolve` — see anti-enumeration below).
- **`NOT_FOUND` status, the RPC fails** when the caller **asserts the resource exists** and its
  absence is a genuine error (`catalog.GetTable` of a named table). This is catalog's deliberate
  choice: a table miss is not normal control flow and is not enumeration-sensitive, so it is an
  error, not a `found` field. **`GetTableResponse` therefore has no `found` field by design** —
  see the note in [`catalog.proto`](../../catalog/v1/catalog.proto).

### Anti-enumeration (the security-driven exception)

`secret.Resolve` **deliberately conflates** "no such ref" and "ref exists but you may not read it"
into a single `found = false` + empty value, and returns `OK` — never `NOT_FOUND` or
`PERMISSION_DENIED`. A distinguishable "exists-but-forbidden" response would let an attacker
enumerate which secret refs are real. This is the one place where `PERMISSION_DENIED` is
**forbidden** for an authorization failure; the collapse to `found=false` is the defense
(see [`secret.proto`](../../secret/v1/secret.proto) FOUND SEMANTICS).

## `reason` strings are log-only

Where an RPC returns a machine-readable failure (`deny_code` enum on `identity.Authorize` /
`tenancy.Decide`, the status codes above), any accompanying human-readable `reason` /
status-message string is **diagnostic only** — callers MUST branch on the code/enum, never parse
the string. Reason strings are attacker-influenceable and unstable across versions.

## Related

- [`core/v1/invoke.proto`](../../core/v1/invoke.proto) — the gateway surfaces enforcement failures
  (C2/C5/C7) as the status codes above.
- [reviews/07](../../../../../reviews/07-freeze-review.md) M1 + M2 — the findings this resolves.
- [reviews/06](../../../../../reviews/06-proto-contract-review.md) C-5 — the original gap.
