# `scheduler-backend/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: scheduler-backend` plugin. Pairs with the wire
> contract [`scheduler.proto`](scheduler.proto) and the golden vectors
> [`scheduler-v1.json`](../../../../conformance/scheduler-v1.json). Status: **v1 (frozen — rat/1.2, ADR-003: control-plane = one ref + conformance)**.

## What a `scheduler-backend` plugin is

A `kind: scheduler-backend` plugin (in-process cron, Temporal, Airflow-bridge, k8s-CronJob) owns
**"fire this at this time / on this interval"** — it is a **clock, not an orchestrator**. The
reconciler asks it *when*; the reconciler still decides *what* happens when a trigger fires
(see the reconciliation model in `docs/architecture/overview.md`). This separation is load-bearing:
swapping a cron-based scheduler for Temporal does not change what the reconciler does with a
fired trigger.

## Capabilities

| capability URI | RPC | what it does |
|---|---|---|
| `rat://scheduler/v1/schedule` | `Schedule` | register a trigger (cron expression or one-shot `at_unix_ms`) |
| `rat://scheduler/v1/cancel` | `Cancel` | remove a previously-registered trigger |
| `rat://scheduler/v1/watch-due` | `WatchDue` | **server-streaming** — emit triggers as they fire |

## The RPCs

- **`Schedule(trigger_id, cron, at_unix_ms)` → `{trigger_id}`** — register a trigger.
  `trigger_id` is caller-chosen and used to cancel + to correlate fired events. Supply
  `cron` for a recurring trigger or `at_unix_ms` for a one-shot; a plugin MAY reject a
  request with both or neither with `INVALID_ARGUMENT`. `RequestContext` rides in the
  `rat-callmeta-bin` metadata header (field 1 reserved — ADR-007); it is NOT a proto field.

- **`Cancel(trigger_id)` → `{cancelled: bool}`** — remove a registered trigger.
  `cancelled = false` means no trigger with that id was found; the call returns `OK`
  (absence is normal control flow, not an error). Cancelling an already-fired or
  never-registered trigger is safe and returns `cancelled = false`.

- **`WatchDue(∅)` → `stream WatchDueResponse{trigger_id, fired_at_unix_ms}`** — server-streaming.
  The reconciler consumes this to learn a pipeline is due. Each `WatchDueResponse` carries
  the `trigger_id` and the wall-clock `fired_at_unix_ms` the plugin assigned when it fired.
  See AT-LEAST-ONCE OBLIGATION below.

## Conformance obligations — AT-LEAST-ONCE (proto API-11)

`WatchDue` delivery is **AT-LEAST-ONCE**. This is a pinned wire-contract guarantee, not an
implementation detail:

- A fired trigger **MAY be redelivered** (e.g. consumer reconnects, plugin restarts). The
  consumer MUST treat `WatchDue` delivery as idempotent — it keys actions by
  `(trigger_id, fired_at_unix_ms)` and ignores a duplicate pair.
- The scheduler does **NOT** guarantee exactly-once delivery, nor that a trigger missed while
  the consumer was disconnected is silently dropped. On reconnect the reconciler re-derives
  due-ness from declared schedule state each loop — the scheduler is the clock, the
  reconciler is the guard.
- A richer ack/lease mechanism is additive (GA-track); the at-least-once guarantee is what
  is pinned at `rat/1.2`.

Pass [`scheduler-v1.json`](../../../../conformance/scheduler-v1.json): the lifecycle is
schedule-three-triggers (two past-due, one future) → cancel one → `WatchDue` → assert exactly
the one uncancelled past-due trigger fires (`expect_due: ["run-A"]`). The vectors gate on:

- past-due + not-cancelled → appears in `WatchDue` output
- past-due + cancelled → absent from `WatchDue` output
- future trigger → absent from `WatchDue` output (not yet due)

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response `bool` fields for normal domain outcomes (`cancelled = false` on
  `Cancel` of an unknown trigger).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the scheduler-backend implements a plain gRPC `SchedulerService` server.

## Writing a plugin

1. Implement `SchedulerService` (`Schedule` / `Cancel` / `WatchDue`) over your scheduling
   backend (in-process heap timer, cron parser, Temporal client, k8s CronJob API, …).
2. Implement `WatchDue` as a server-streaming RPC that emits `WatchDueResponse` for every
   currently-due, not-cancelled trigger. At-least-once is the contract — **do not** suppress
   redelivery; the consumer is required to handle it.
3. Make `Cancel` return `cancelled = false` (not an error) for unknown trigger ids — the
   reconciler may cancel triggers it is unsure it registered.
4. Do NOT implement orchestration logic. When a trigger fires, emit it. The reconciler
   decides what pipeline to run.
5. Pass [`scheduler-v1.json`](../../../../conformance/scheduler-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/scheduler/inmemory-py`](../../../../../examples/scheduler/inmemory-py) | 1 (control-plane reference) | one-shot triggers; at-least-once `WatchDue` via snapshot-and-yield; conformance harness integration |

## Related

[`scheduler.proto`](scheduler.proto) · [`scheduler-v1.json`](../../../../conformance/scheduler-v1.json) ·
[error model](../../common/v1/ERROR_MODEL.md) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (control-plane rigor: one ref + conformance) ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md) API-11 (at-least-once pin)
