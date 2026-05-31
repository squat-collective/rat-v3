# `rat-observability-inmemory-py` — the `observability` reference

The control-plane reference for the `observability/v1` axis: an **export sink**, not the
source of truth for core health. The core's own observability (its `/metrics`, OTel
spans, reconcile-loop SLIs) is **native** and does not depend on any observability plugin
([observability.proto](../../../contracts/proto/rat/observability/v1/observability.proto));
this axis is for export/fan-out. One reference + conformance suffices for a control-plane
axis ([ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)).

## Capability

| capability | method | cardinality | what it does |
|---|---|---|---|
| `rat://observability/v1/ingest` | `Ingest` | **bidi-streaming** | stream telemetry in; get cumulative acks back |

`Ingest` is bidi (observability.proto API-4 / freeze-blocker #9): the old client-stream
shape acked only at stream close (≈never for a process-lifetime stream). Bidi returns an
`IngestResponse` per inbound batch with the **cumulative** accepted/rejected counts, so
the emitter sees incremental progress and can flow-control. The core mediates bidi via
`InvokeBidiStream` ([ADR-008](../../../docs/architecture/adrs/008-streaming-capability-invocation.md)).

This reference accepts any point with a non-empty `name` and rejects unnamed points.

## How it's tested

[`observability-v1.json`](../../../contracts/conformance/observability-v1.json) via `make
conformance`: streams two batches and asserts each per-batch ack carries the expected
cumulative counts (2 named → accepted 2; then 1 named + 1 unnamed → accepted 3, rejected 1).

## Files

- [`store.py`](store.py) — the in-memory telemetry sink
- [`server.py`](server.py) — the `ObservabilityService` gRPC servicer (bidi `Ingest`)
- [`harness_test.py`](harness_test.py) — the conformance harness
- [`main.py`](main.py) — standalone gRPC entrypoint
