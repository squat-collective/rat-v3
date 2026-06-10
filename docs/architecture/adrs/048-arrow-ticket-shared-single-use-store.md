# ADR-048: Arrow ticket single-use store is pluggable (shared/durable, not per-process)

## Status: Accepted (2026-06-10)

## Context

The code-level review of `core/` found **gap #7** in `core/arrowticket` — the reference implementation
of the bulk-data leg's anti-replay credential (the Arrow Flight ticket that gates the data plane, which
bypasses the core). Its single-use enforcement was a **per-process map**:

```go
// before
type Minter struct { key []byte; ...; mu sync.Mutex; used map[string]bool }
```

The ticket design is otherwise sound — HMAC-signed, short-TTL, bound to `{stream, caller, tenant}` — but
single-use was tracked only in that in-memory `used` set. So a producer **restart** starts with an empty
set (a ticket consumed before the restart is redeemable again), and a **second replica** has its own set
(a ticket can be spent once per replica). The replay window the ticket exists to close is reopened by the
two things production does constantly: restart and scale-out. (This is the "shared/durable single-use
store" half of backlog AV-6.)

## Decision

**Make the single-use store pluggable: a `SingleUseStore` interface, an in-memory default, and a shared
`CASStore` over an atomic create-if-absent primitive that closes replay across restart + replicas.
Validation fails CLOSED when the store can't confirm.**

```go
type SingleUseStore interface {
    // Consume atomically marks id used; firstUse=false on a replay. A non-nil err → fail closed.
    Consume(id string) (firstUse bool, err error)
}
```

- **`MemStore`** (the default, `NewMinter`) — the existing per-process behaviour, fine for a single
  producer.
- **`CASStore`** (`NewMinterWithStore`) — records consumed ticket ids in a backend with an **atomic
  create-if-absent**, so a restarted or replicated producer sees a ticket already consumed elsewhere.
- **Fail closed.** `Validate` rejects the ticket if `Consume` returns an error — an unconfirmable
  single-use check cannot guarantee no replay, so it must not silently accept.

### The create-if-absent dependency (honest cross-ref)

A correct shared store needs an **atomic create-if-absent** (two concurrent redemptions of one ticket
must yield exactly one `created=true`). The frozen `state/v1` lacks this primitive — the same gap
ADR-043 (the lease) hit, tracked as **ADR-043 Q01**. So `CASStore` rides a `SingleUseCAS` a backend
provides natively — etcd txn, Redis `SETNX`, or a DB unique constraint — until a create-if-absent
amendment to `state/v1` lands, at which point the state axis can back it directly (sharing the lease's
path).

## Consequences

### Positive

- **Replay can't be reopened by restart or scale-out** when a shared store is used. Proven by tests:
  two minters sharing one store can't both redeem a ticket (the restart/replica scenario); concurrent
  redemptions of one id yield exactly one `firstUse` (the atomic-CAS race, run under `-race`); a store
  error fails closed.
- **Backward compatible + honest about scope.** `NewMinter` is unchanged (per-process default); the
  shared store is opt-in. `arrowticket` remains a producer-side **SDK helper**, not core — this gap is
  the helper's correctness, not a core/wire change.
- **Reuses the gap-#1 shape.** Same pluggable-store + fail-closed pattern as the lease (ADR-043), and
  the same create-if-absent dependency — so when `state/v1` gains it, both benefit.

### Negative / costs

- **The default is still per-process.** A single-producer deployment is unchanged; multi-producer
  correctness requires wiring a shared `CASStore` — opt-in, documented, not automatic.
- **No `state/v1`-backed store yet. RESOLVED (2026-06-10) by [ADR-049](049-state-v1-create-if-absent.md).**
  The `state/v1` `CreateIfAbsent` amendment landed, and `core/arrowticket/statecas` bridges
  `SingleUseCAS` onto it — so the shared store now rides the **state axis** directly (no external
  etcd/Redis required). The per-process `MemStore` remains the solo default.

## Alternatives considered

- **Bind tickets to a channel/cert fingerprint instead of tracking single-use.** The other half of
  AV-6 — complementary, not a replacement; it raises the bar but a leaked ticket reused on the same
  channel still needs single-use. Out of scope here.
- **Keep the per-process map.** Rejected: it's the gap — restart/replica reopen replay.
- **Force a shared store always.** Rejected: a solo producer shouldn't need an external store; the
  in-memory default keeps `chmod +x ./rat` dependency-free.

## Related

- [`core/arrowticket/arrowticket.go`](../../../core/arrowticket/arrowticket.go) — the ticket helper this hardens (D2 / common/v1/data.proto SEC-14).
- ADR-043 — the lease's pluggable-store + create-if-absent (Q01) the shared store mirrors and waits on.
- backlog AV-6 — Arrow-ticket hardening (this is the shared/durable single-use half; per-producer key + key rotation + channel-binding remain).
- reviews: the 2026-06-10 code-level gap analysis (gap #7).
