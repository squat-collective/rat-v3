# ADR-049: `state/v1` create-if-absent — an additive, capability-gated atomic primitive

## Status: Proposed (2026-06-10)

> Decision-first. This amends the **frozen** `state/v1` wire, so it is written and reviewed before any
> code (CLAUDE.md #3). It follows the exact precedent of [ADR-035](035-state-axis-delete.md) (the
> additive `Delete` method) — so "additive amendment to a frozen axis" is a trodden path here, not a
> reopening of the freeze.

## Context

Two accepted ADRs landed with the **same honest caveat**: they need an **atomic create-if-absent** the
state axis does not provide.

- **[ADR-043](043-leader-election-over-the-state-axis.md) (the HA lease), Q01.** The lease lives in a
  single state key; steady-state contention is pure CAS and split-brain-free. The ONE race is two
  replicas creating a *never-before-existing* lease key at the same instant — there is no way to say
  "create this key only if it is absent," so both can write it.
- **[ADR-048](048-arrow-ticket-shared-single-use-store.md) (the Arrow-ticket replay store).** Marking a
  ticket consumed is *exactly* a create-if-absent: "record this ticket id, fail if already recorded."
  Without the primitive, the shared store must ride a non-`state/v1` backend (etcd / Redis `SETNX` / a
  DB unique constraint), and the in-memory default reopens replay on restart/replica.

Why today's `PutRequest` can't express it:

```proto
message PutRequest { ...; bytes value = 3; int64 if_revision = 4; } // 0 == unconditional; >0 == CAS on N
```

`if_revision = 0` overwrites; `if_revision = N>0` requires the key to *exist* at revision N. There is no
encoding for "succeed only if the key does **not** exist." For an absent key, two writers both observe
"absent" and both unconditionally write → last-write-wins, not exactly-one-creator.

### Why not just add a field / sentinel (the trap)

A new `create_only bool` field, or a sentinel `if_revision = -1`, is wire-additive — but an **old
backend silently ignores it** and does an unconditional write. So a client asking for create-if-absent
against a pre-amendment backend gets a silent OVERWRITE — the precise split-brain / double-spend the
primitive exists to prevent. An additive *field* makes the unsafe case look safe.

## Decision

**Add a new, optional, capability-gated RPC `CreateIfAbsent` to `StateService`, capability
`rat://state/v1/create-if-absent` — modeled exactly on ADR-035's `Delete`. Capability *presence* is the
negotiation: a backend that doesn't support it doesn't declare it, so the core can never silently misuse
it (the gateway returns "no provider" instead of overwriting).**

### 1. The wire (additive — `make breaking`-clean)

```proto
// rat://state/v1/create-if-absent — atomically create one key (plugin+tenant relative) only if it
// does not already exist. ADDITIVE amendment (ADR-049): a state-backend MAY leave it UNIMPLEMENTED;
// a backend declares this capability in `provides` only if it supports it ATOMICALLY, and consumers
// MUST handle its absence. This is the primitive leader-election lease bootstrap (ADR-043 Q01) and
// the Arrow-ticket single-use store (ADR-048) require; without it, neither can be backed by state/v1.
rpc CreateIfAbsent(CreateIfAbsentRequest) returns (CreateIfAbsentResponse) {
  option (rat.common.v1.capability) = "rat://state/v1/create-if-absent";
}

message CreateIfAbsentRequest {
  reserved 1;            // RequestContext rides in the rat-callmeta-bin metadata header (ADR-007)
  string key = 2;        // plugin-relative; subject to the file's KEY GRAMMAR. Non-empty.
  bytes value = 3;       // the value to store on creation.
}

message CreateIfAbsentResponse {
  // Reuses PutOutcome (no new enum): COMMITTED == created (revision = the new key's revision);
  // CONFLICT == the key ALREADY EXISTED (the create did not happen; revision = the existing rev);
  // UNKNOWN == the backend could not confirm — the caller MUST treat the result as unknown and NOT
  // assume it created the key (same fencing discipline as Put — reviews/06 C-4).
  PutOutcome outcome = 1;
  int64 revision = 2;
}
```

Distinct request/response messages satisfy buf `RPC_REQUEST_RESPONSE_UNIQUE`; the `PutOutcome` *enum* is
reused (enums may be shared), so the lease/ticket already understand the committed/conflict/unknown
trichotomy.

### 2. Negotiation = capability presence (the safe part)

A backend implements `CreateIfAbsent` **atomically** (two concurrent creates of one key yield exactly
one COMMITTED) or doesn't provide the capability at all. The core's consumers (lease, ticket store)
`require` `rat://state/v1/create-if-absent`; the registry routes only to a backend that provides it. A
backend that lacks it simply isn't eligible to back a multi-replica lease / shared ticket store — the
core falls back (refuse multi-replica HA on this backend; the ticket store stays in-memory or external)
rather than silently corrupting. **No old backend can be misused, because the capability isn't there.**

### 3. Conformance tier

`CreateIfAbsent` joins the existing **multi-replica-eligibility** conformance tier the state proto
already defines (single-key linearizable CAS + ordered Watch). The tier becomes: *linearizable CAS +
ordered Watch + atomic create-if-absent.* A backend claiming multi-replica eligibility MUST pass all
three, with golden vectors (the same sub-phase-0f gate, extended) — including a **concurrency vector**:
N simultaneous `CreateIfAbsent` of one key → exactly one COMMITTED, the rest CONFLICT.

### 4. What it unblocks (the payoff)

- **ADR-043 lease:** initialize the lease key once, race-free (`CreateIfAbsent`), then steady-state CAS
  as today. Closes Q01.
- **ADR-048 ticket store:** the shared `CASStore`'s `SingleUseCAS.PutIfAbsent` maps 1:1 onto
  `CreateIfAbsent`, so it can be backed by the **state axis** directly — no external etcd/Redis. Closes
  the honest caveat.

One primitive, both caveats closed, and the state axis can natively back both.

## Consequences

### Positive

- **Two open caveats closed by one additive RPC**, with the negotiation hazard structurally impossible
  (capability presence, not a silently-ignored field).
- **Precedent-exact + buf-clean.** Same shape as ADR-035's `Delete`: additive method, optional per
  backend, consumers handle absence. The frozen ≤`rat/2.0` methods/messages are untouched; `make
  breaking` passes.
- **Reuses the existing eligibility model.** No new negotiation mechanism — it's the platform's own
  capability negotiation, plus the conformance tier the lease already leans on.
- **Six-thing core unchanged.** The state *gateway* (a core thing) relays one more method; the semantics
  live in the state-backend plugin.

### Negative / costs

- **It IS a frozen-axis amendment.** Even additive, touching `state/v1` carries weight: proto change →
  regenerate the SDKs → new conformance vectors → at least one reference backend must implement it
  (sqlite-py is the natural first — a `WHERE NOT EXISTS` / `INSERT … ON CONFLICT DO NOTHING`). Not free.
- **Optionality means a fallback path stays.** Backends without it still exist, so the lease/ticket keep
  their non-`state/v1` fallback — two code paths until adoption is universal.
- **A new capability to govern.** `rat://state/v1/create-if-absent` is another entry in the conformance
  surface (versioning, golden vectors, the eligibility doc).

## Open questions

- **Q01 — reuse `PutOutcome` vs a dedicated `CreateOutcome`.** *Recommend reuse* (COMMITTED=created,
  CONFLICT=existed) — fewer symbols, the consumers already handle it. A dedicated `{CREATED, EXISTS,
  UNKNOWN}` reads clearer but adds an enum; decide at proto-review.
- **Q02 — fold into the conformance tier now, or ship the RPC first + gate later?** *Recommend* ship the
  RPC + the concurrency golden vector together (the vector is the whole point — an "atomic" create
  without a race test is honor-system).
- **Q03 — a symmetric `DeleteIfRevision`-style "delete only if absent"?** Not needed by 043/048; out of
  scope — raise separately if a use case appears.
- **Q04 — should the lease's `StateStore` (ADR-043) prefer `CreateIfAbsent` when available and keep the
  unconditional-create fallback otherwise?** *Recommend yes* — feature-detect via the capability, use
  the safe path when present. Implementation detail for the 043 follow-up, noted here for traceability.

## Alternatives considered

- **`create_only bool` field on `PutRequest` (or `if_revision = -1` sentinel).** Wire-additive but
  UNSAFE: an old backend ignores it and overwrites — silent split-brain/double-spend. Rejected: an
  additive field makes the unsafe case look safe; a new *capability* makes "unsupported" explicit.
- **A new capability mapped to the SAME `Put` method (mode flag).** Couples two capabilities to one
  method and still needs the flag on the wire (the field hazard). Rejected for a clean, distinct RPC.
- **Leave it to external backends (status quo).** What 043/048 do today. Rejected as the end state: it
  forces a non-`state/v1` dependency for core HA + replay safety, which the platform should provide
  natively through its own state axis.
- **A general `Txn` (etcd-style compare-and-op).** The fully-general primitive (arbitrary compare →
  then/else ops). Powerful, but a large new surface for one need; create-if-absent is the 95% case.
  Revisit if multi-key transactions become a real requirement.

## Migration

- Purely additive: existing planes/backends are unaffected; a backend opts in by implementing the RPC +
  declaring the capability. Consumers already handle a missing capability (the 043/048 fallbacks).
- Rollout: amend the proto → regenerate SDKs → implement in **sqlite-py** (first reference, atomic via a
  SQL unique constraint) + a conformance concurrency vector → switch the lease bootstrap + ticket store
  to feature-detect and prefer it. No existing valid deployment breaks at any step.

## Related

- [ADR-035](035-state-axis-delete.md) — the additive `Delete` method this is modeled on (same optionality + capability-gating pattern).
- [ADR-043](043-leader-election-over-the-state-axis.md) — the HA lease; **Q01** is what this closes.
- [ADR-048](048-arrow-ticket-shared-single-use-store.md) — the ticket single-use store; its `SingleUseCAS.PutIfAbsent` maps onto this RPC.
- [`contracts/proto/rat/state/v1/state.proto`](../../../contracts/proto/rat/state/v1/state.proto) — the axis amended; its CAS + conformance-tier language this extends.
- reviews/06 C-4 — the lease-fencing rigor (`PutOutcome` UNKNOWN) carried into `CreateIfAbsent`.
- Prior art: etcd `Txn` with `CreateRevision == 0`; SQL `INSERT … ON CONFLICT DO NOTHING` / unique constraints; Redis `SETNX`.
