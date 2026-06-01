# `rat-scheduler-inmemory-py` — the `scheduler-backend` reference

> ⚠️ **WIRE-CONTRACT REFERENCE — NOT PRODUCTION-HARDENED.** This validates the `scheduler-backend/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory store**. A production `scheduler-backend` plugin adds a durable/real backend + the enforcement the core will demand (Phase 1) — it demonstrates the contract, not a deployment. See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The control-plane reference for the `scheduler/v1` axis: a **clock, not an
orchestrator** ([scheduler.proto](../../../contracts/proto/rat/scheduler/v1/scheduler.proto)).
It owns "fire this at this time / on this interval"; the reconciler asks it *when* and
decides *what*. One reference + conformance is sufficient for a control-plane axis
([ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)).

## Capabilities

| capability | method | cardinality | what it does |
|---|---|---|---|
| `rat://scheduler/v1/schedule` | `Schedule` | unary | register a one-shot (or cron) trigger |
| `rat://scheduler/v1/cancel` | `Cancel` | unary | remove a trigger |
| `rat://scheduler/v1/watch-due` | `WatchDue` | server-streaming | stream triggers as they fire |

This reference implements **one-shot** triggers (`at_unix_ms`); a production backend
(cron, Temporal, cloud scheduler) would also parse `cron`. `WatchDue` is server-streaming
— the core mediates it via `InvokeServerStream` ([ADR-008](../../../docs/architecture/adrs/008-streaming-capability-invocation.md)).

## Delivery semantics

**At-least-once** (scheduler.proto, pinned at freeze). A fired trigger may be
redelivered; the reconciler keys actions by `(trigger_id, fired_at_unix_ms)` and ignores
duplicates. This reference yields every due / not-cancelled / not-yet-fired one-shot,
marks it fired, then completes the stream.

## How it's tested

[`scheduler-v1.json`](../../../contracts/conformance/scheduler-v1.json) via `make
conformance`: schedules triggers at offsets relative to now (negative = already due,
positive = future), cancels one, drains `WatchDue`, and asserts exactly the expected set
fired (A fires; B cancelled; C is in the future).

## Files

- [`store.py`](store.py) — the in-memory trigger store
- [`server.py`](server.py) — the `SchedulerService` gRPC servicer
- [`harness_test.py`](harness_test.py) — the conformance harness
- [`main.py`](main.py) — standalone gRPC entrypoint
