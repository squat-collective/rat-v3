# ADR-043: Leader election over the state axis — make multi-replica HA real

## Status: Accepted (2026-06-10)

## Context

The code-level review of `core/` found **gap #1**: the reconciler's leader-election lease was an
**in-process struct**. `rat serve` built its elector as:

```go
lease.NewElector("rat-serve", lease.NewStore(), 10*time.Second)
```

`lease.NewStore()` returns a `&Store{}` — a mutex-guarded map, created fresh **inside each
`rat serve` process**. The lease package's own doc claimed leadership runs on "the state-backend's
single-key linearizable CAS" (ADR-002 D5), but nothing wired the `Store` to a state backend. The
consequence is stark: **two `rat serve` replicas each hold their own private lease and both elect
themselves leader.** The entire `lease`/`Elector`/`Loop` apparatus — which is otherwise well-built
(TTL margin, thrash guard, minimum-hold) — was electing a process against itself. Multi-replica HA
was not merely unfinished; it was structurally impossible.

A second, coupled defect surfaced (the standing backlog item AV-1). `Store.Renew` returned a bare
`bool`. With a real, networked backend that conflates two very different outcomes:

- **CONFLICT** — a deterministic CAS failure: the lease was genuinely taken / expired. Step down.
- **UNKNOWN** — the backend couldn't be reached / didn't confirm (timeout, partition). The outcome
  is unknown; the holder may still hold the lease.

The frozen `state/v1` contract already models this exactly — `PutOutcome` has
`COMMITTED` / `CONFLICT` / `UNKNOWN`, with `UNKNOWN`'s doc warning that "a lease renewal that
returns UNKNOWN cannot be relied on for fencing." A bare `bool` throws that away: every transient
backend blip becomes a leadership flap — the precise etcd-slow→apiserver-flap thrash a durable
backend makes real. AV-1 flagged this as "a breaking refactor once a durable backend binds the
`bool` interface — do it *before* the durable backend lands." Binding the backend is this ADR, so
the refactor lands here.

## Decision

**Make the lease a pluggable `Backend`. Ship two: the in-memory `Store` (solo, the default) and a
`StateStore` that runs the lease as a single-key compare-and-set over a SHARED state-backend via
`state/v1`. `rat serve` selects between them by configuration. While binding the durable backend,
evolve the fencing contract so a renewal distinguishes "genuinely lost" from "unconfirmed," and the
elector holds leadership through transient errors until its local TTL genuinely lapses.**

### 1. The `Backend` interface (lease package stays pure)

```go
type Backend interface {
    Acquire(candidate string, now time.Time, ttl time.Duration) (ok bool, token uint64, err error)
    Renew(candidate string, token uint64, now time.Time, ttl time.Duration) (ok bool, newToken uint64, err error)
}
```

- `err != nil` ≠ `ok == false`. A nil-err `ok=false` is a genuine loss (CAS conflict / expiry); a
  non-nil err is "unconfirmed." (AV-1.)
- `Renew` returns `newToken` because the CAS revision re-stamps on every write — the state-backed
  store carries a *new* fencing token forward each renewal (the in-memory store returns it
  unchanged).

The in-memory `Store` implements it (err always nil). The `Elector` now drives a `Backend`, not a
concrete `*Store`, and holds leadership across an unconfirmed renewal while `now < leaseExpiry`.

### 2. `StateStore` — the lease as state/v1 CAS

A single key (default `rat/lease/rat-serve`) holds `{holder, expiry}`. The fencing token is the
key's revision.

- **Acquire**: `Get` the key; if held + live + not-us, fail. Else CAS-`Put` on the observed
  revision. Two contenders that observe the same revision both CAS on it; the backend's
  linearizable CAS lets exactly one commit (the loser sees a bumped revision → `CONFLICT`).
- **Renew**: CAS-`Put` under our token. `COMMITTED` → renewed (carry the new revision). `CONFLICT`
  → genuinely lost. `UNKNOWN`/transport → uncertain (the elector holds per local TTL).

It depends only on a minimal `StateCAS` seam (a linearizable `Get` + a CAS `Put`), so the package
keeps zero gRPC/proto deps and is trivially fakeable. `cmd/rat` adapts a real
`rat.state.v1.StateServiceClient` onto it, mapping `PutOutcome` → the committed/conflict/uncertain
trichotomy. Each backend call is deadline-bounded so a hung state-backend surfaces as the
"uncertain" err rather than pinning the reconcile tick.

### 3. Wiring + bootstrap constraint

`rat serve` defaults to the in-memory `Store` (solo — no external dependency). When
`RAT_LEASE_STATE_ADDR` is set, the lease lives in that **shared** state-backend (real HA). The
candidate id gets a random per-process suffix so two replicas are never indistinguishable holders.

The shared backend is the fleet's **etcd analogue**: external/attached, reachable by every replica
independently of the plane they reconcile. It is **not** a plugin a replica launches — that would
be circular (you need the lease to decide who launches). This is the tier-0 reality the state proto
already names; this ADR makes it operational.

## Consequences

### Positive

- **Multi-replica HA is now achievable, and proven at the lease layer.** Tests put two electors
  over one shared CAS and assert exactly one leader, correct failover only after genuine expiry,
  and CAS fencing (a stale token conflicts, deterministically, with no error). The in-memory store
  cannot pass the shared-leader test; the state store does.
- **No leadership thrash on transient backend errors (AV-1 closed).** A leader holds through
  unconfirmed renewals until its local lease truly lapses — the K8s lease discipline. Tested.
- **No wire change.** It rides the frozen `state/v1` Get/Put-CAS + `PutOutcome`. Six-thing count
  unchanged — leader election was always the reconciler's job; this gives it a real substrate.
- **Solo is untouched.** Default path is the in-memory store; no new dependency for `chmod +x ./rat`.

### Negative / costs

- **Create race on a never-before-existing lease key.** `state/v1` has no create-if-absent
  primitive (`if_revision` CASes an existing key or writes unconditionally). So *steady-state*
  contention — the case that matters for HA failover — is pure CAS and split-brain-free, but two
  replicas creating the key at the same instant on a cold cluster is a genuine race. Mitigate by
  pre-initializing the key or staggering replica starts (the second replica then sees it present).
  A create-if-absent amendment to `state/v1` would close it fully — see Open questions.
- **TTL leases assume bounded clock skew.** Standard for this lease style; choose `ttl` ≫
  renew-interval + max skew (the existing default ttl=10s / tick=200ms is comfortable).
- **`Backend`/`Renew` signature churn.** `Acquire`/`Renew` grew an `err` (and `Renew` a
  `newToken`). Contained to the lease package + its tests; updated here.

## Open questions

- **Q01 — create-if-absent for `state/v1`.** An additive `PutRequest` mode ("succeed only if the
  key is absent", e.g. `if_revision = -1`) would make cold-start election fully race-free. Additive,
  `make breaking`-clean, but a frozen-contract change → its own ADR. Until then: pre-init the key.
- **Q02 — fold the lease onto a real durable state-backend reference.** The tests use a fake
  linearizable CAS; the sqlite-py backend can serve as the first real durable lease store (it
  already exposes CAS via `if_revision`). Wire + conformance-test it against `RAT_LEASE_STATE_ADDR`.

## Alternatives considered

- **Keep the in-memory store, add a separate distributed-lock dependency (etcd/Consul direct).**
  Rejected: the platform already mandates a state-backend with linearizable CAS for exactly this
  (state.proto C5/CAS). A second coordination system is a redundant operational burden and a second
  tier-0 SPOF.
- **Leave `Renew` a bare bool, treat any failure as "lost."** Rejected: that is the thrash bug — a
  flaky backend would ping-pong leadership and starve reconciliation. The `UNKNOWN` distinction is
  in the wire contract precisely so we don't.
- **Run the lease *through* the gateway (as a plugin capability call).** Rejected for bootstrap: the
  lease is the core's own coordination state, needed before/independently of plane C5 wiring. A
  direct state client to the shared backend is the right seam; the core owns its lease namespace.

## Related

- [`state/v1/state.proto`](../../../contracts/proto/rat/state/v1/state.proto) — the CAS + `PutOutcome` this builds on (C5/CAS, the `UNKNOWN` fencing note).
- ADR-002 D5 — leader election via the state-backend's single-key linearizable CAS (the design this finally implements).
- ADR-042 — the sibling gap-#2 fix (channel-authenticated identity); same review.
- backlog AV-1 — "renewal-error ≠ lease-lost"; closed here.
- reviews: the 2026-06-10 code-level gap analysis (gap #1).
