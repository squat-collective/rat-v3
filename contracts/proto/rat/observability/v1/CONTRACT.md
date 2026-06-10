# `observability/v1` — plugin contract (author guide)

> **Status (2026-06-10) — the core is built and sealed.** What this guide describes **runs
> today**: capability routing, channel-authenticated plugin identity (C2, ADR-042), C5
> capability authz, deadline-bounding, and mandatory audit emission are enforced by the
> sealed core (`rat/2.0`, hardened through `rat/6.13`). `make conformance` checks the
> references against the golden vectors; `make composition` runs the cross-axis suite
> against real providers. The wire stays frozen (`rat/1`); post-freeze changes land as
> additive, capability-gated amendments (e.g. ADR-035 `delete` + ADR-049
> `create-if-absent` on `state/v1`).

> Canonical guide for implementing a `kind: observability` plugin. Pairs with the wire
> contract [`observability.proto`](observability.proto) and the golden vectors
> [`observability-v1.json`](../../../../conformance/observability-v1.json). Status: **v1 (frozen — rat/1, ADR-003: control-plane = one ref + conformance)**.

## What an `observability` plugin is

A `kind: observability` plugin (stdout, Prometheus, OTel collector, Datadog, CloudWatch) is an
**export sink** — it receives telemetry the core + plugins emit and ships it onward. It is NOT the
source of the core's own health data.

**Critical distinction (observability.proto + [plugin-architecture.md](../../../../../.claude/rules/plugin-architecture.md)
cross-cutting):** the core's own observability — its native `/metrics` endpoint, its OTel spans,
its reconcile-loop SLIs — is **native and unconditional**. It does not depend on any
`observability` plugin being installed. `"observability: none"` must still leave the core
self-observable. This axis is the optional **export / fan-out tier**: it routes telemetry onward
to external systems and allows plugins to contribute their own telemetry into the same stream.

## Capabilities

| capability URI | RPC | what it does |
|---|---|---|
| `rat://observability/v1/ingest` | `Ingest` | bidi-stream telemetry batches into the sink; sink returns incremental accepted/rejected counts |

## The RPCs

- **`Ingest(stream IngestRequest)` → `stream IngestResponse`** — **bidi-streaming**. The emitter
  sends batches of `TelemetryPoint`; for each inbound batch the sink emits one `IngestResponse`
  with the **cumulative** `accepted` / `rejected` counts for the stream so far, providing
  incremental backpressure over a process-lifetime telemetry stream.

  A `TelemetryPoint` carries: `type` (`METRIC` / `SPAN` / `LOG`), `name`, `value` (numeric;
  ignored for spans/logs), `attributes` (structured map — include `trace_id`/`run_id` for
  cross-signal correlation, C1), and `timestamp_unix_ms`.

  `RequestContext` is NOT a field — it rides in the `rat-callmeta-bin` metadata header
  ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)).

  **Rejection rule (reference):** a point with an empty `name` MUST be counted as `rejected`, not
  `accepted`. Any sink-specific validation (type filtering, attribute constraints) counts rejected
  points the same way — never silently drops them.

## Conformance obligations

The vectors gate on the bidi-stream cumulative-count semantics and the empty-name rejection rule:

- **Batch 1:** 2 named `METRIC` points → `{accepted: 2, rejected: 0}`.
- **Batch 2:** 1 named `METRIC` + 1 unnamed `LOG` → cumulative `{accepted: 3, rejected: 1}`.

Pass [`observability-v1.json`](../../../../conformance/observability-v1.json) via `make conformance`.
The bidi shape is wire-breaking (it is the reason for the pre-freeze API-4 change); a unidirectional
client-stream implementation is non-conformant.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/infrastructure failures. The
  `Ingest` stream has no domain-outcome fields — `accepted`/`rejected` counts are normal telemetry
  data, not error signals; infrastructure failure raises `UNAVAILABLE` or `INTERNAL` on the stream.

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the plugin implements a plain gRPC `ObservabilityService` server.

## Writing a plugin

1. Implement `ObservabilityService.Ingest` as a **bidi-streaming** handler: iterate
   `request_iterator`, process each batch through your sink backend, and `yield` one
   `IngestResponse` per batch with the cumulative `accepted` / `rejected` totals.
2. Count any point with an empty `name` as `rejected`; apply any additional sink-side validation
   the same way. Never silently drop points — every point is either `accepted` or `rejected`.
3. Emit counts as **cumulative stream totals** (not per-batch deltas); the emitter uses them for
   backpressure.
4. Declare `provides: [rat://observability/v1/ingest]` in your `plugin.yaml`. No other capability
   is defined for this axis.
5. Pass [`observability-v1.json`](../../../../conformance/observability-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/observability/inmemory-py`](../../../../../plugins/observability/inmemory-py) | 1 (control-plane) | bidi-stream Ingest; cumulative accepted/rejected counts; empty-name rejection; `store.TelemetrySink` as a separable sink object |

## Related

[`observability.proto`](observability.proto) · [`observability-v1.json`](../../../../conformance/observability-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[`.claude/rules/plugin-architecture.md`](../../../../../.claude/rules/plugin-architecture.md) (cross-cutting self-observability) ·
[reviews/06](../../../../../reviews/06-proto-contract-review.md) API-4
