# `billing/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: billing` plugin. Pairs with the wire
> contract [`billing.proto`](billing.proto) and the golden vectors
> [`billing-v1.json`](../../../../conformance/billing-v1.json). Status: **v1 (frozen — rat/1, ADR-003: control-plane = one ref + conformance)**.

## What a `billing` plugin is

A `kind: billing` plugin (none/noop, usage-metered, seat-based, cloud-marketplace) is a metering
**SINK**: the core emits usage events at well-defined points (pipeline run, credential vend,
storage bytes) and this plugin records and aggregates them. The plugin does not generate events —
it receives them. It is per-tenant **by construction** (C7): every `Record` call carries the
caller's tenant in the `rat-callmeta-bin` metadata envelope (ADR-007), so multi-tenant deployments
meter per tenant without the billing plugin re-deriving the boundary. A caller cannot bill another
tenant's account because the tenant boundary is set by the core, not the request body.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://billing/v1/record` | `Record` | record one or more meterable usage events under the caller's tenant |

## The RPCs

- **`Record(events: []UsageEvent)` → `{recorded: int64}`** — appends each `UsageEvent` to the
  per-tenant ledger and returns the count of events recorded. `events` may be empty (no-op,
  returns `recorded=0`). The tenant is read from `rat-callmeta-bin` (`RequestContext.identity.tenant`),
  **never** from a request field (C7). An unreadable or missing metadata envelope defaults to the
  empty-string tenant (single-tenant/solo deployments). Empty string `meter` or negative `quantity`
  in any event → `INVALID_ARGUMENT`; the entire batch is rejected.

### `UsageEvent` shape

| field | type | meaning |
|---|---|---|
| `meter` | `string` | metric name, e.g. `"pipeline.run"`, `"storage.bytes"`, `"credential.vend"` |
| `quantity` | `double` | quantity in the meter's natural unit (runs=1, bytes=N, …) |
| `timestamp_unix_ms` | `int64` | event timestamp; 0 is valid (implementations may substitute wall-clock on receipt) |
| `dimensions` | `map<string,string>` | optional breakdown labels (pipeline id, plugin id, …) |

## Conformance obligations

Pass [`billing-v1.json`](../../../../conformance/billing-v1.json) via `make conformance`. The
harness drives the following sequence against a live `BillingService` gRPC server:

1. **`two_events`** — `Record` two events (`pipeline.run` × 1, `storage.bytes` × 1024) under
   tenant `"acme"`. Assert `RecordResponse.recorded == 2`.
2. **`one_more_run`** — `Record` one further `pipeline.run` × 1 under the same tenant. Assert
   `RecordResponse.recorded == 1`.
3. **Aggregate check** — assert the running per-`(tenant, meter)` sum matches
   `expect_aggregate`: `pipeline.run` total = 2, `storage.bytes` total = 1024.

The harness also runs a **tenant-isolation check** (not in the JSON vectors): recording under a
second tenant must leave tenant `"acme"`'s aggregates unchanged. This is the C7 enforcement test.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/infrastructure failures; there
  are no domain-outcome fields in `RecordResponse` (a successful `Record` that records 0 events
  is still `OK` with `recorded=0`).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the billing plugin implements a plain gRPC `BillingService` server.

## Writing a plugin

1. Implement `BillingService` (`Record`) over your metering backend (in-memory, time-series DB,
   cloud billing API, Stripe, etc.).
2. Extract the tenant from `rat-callmeta-bin` (`RequestContext.identity.tenant`) on every `Record`
   call — **not** from a request field. An absent header defaults to empty string (solo deployment).
3. Accumulate per-`(tenant, meter)` aggregates so the conformance aggregate check can be satisfied.
   The raw event log (append-only, per tenant) enables audit/replay; the aggregate is the metering
   signal.
4. Pass [`billing-v1.json`](../../../../conformance/billing-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/billing/inmemory-py`](../../../../../examples/billing/inmemory-py) | 1 (control-plane ref) | in-memory ledger; tenant from metadata; per-`(tenant, meter)` aggregate; C7 isolation |

## Related

[`billing.proto`](billing.proto) · [`billing-v1.json`](../../../../conformance/billing-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md) (call-context transport)
