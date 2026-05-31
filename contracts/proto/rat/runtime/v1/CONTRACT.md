# `runtime/v1` — plugin contract (author guide)

> Canonical guide for implementing a `kind: runtime` plugin. Pairs with the wire
> contract [`runtime.proto`](runtime.proto) and the golden vectors
> [`runtime-v1.json`](../../../../conformance/runtime-v1.json). Status: **v1-preview**.

## What a `runtime` plugin is

A `kind: runtime` plugin (pyarrow, a container exec runtime, a WASM host, a subprocess
worker, …) executes a **unit of work** on behalf of a strategy/engine. It is the
"where does the code run" axis — distinct from `deployment-runtime` (where *plugins*
run).

## Capabilities

| capability URI | method | cardinality | what it does |
|---|---|---|---|
| `rat://runtime/v1/execute` | `Execute` | server-streaming | run a unit of work; stream liveness, then the outcome |

## The RPC

- **`Execute(work_spec, inputs)` → stream `ExecuteResponse`** — `work_spec` is opaque
  bytes the runtime knows how to run (a serialized plan, a script ref, a container
  entrypoint — the control plane does not interpret it). `inputs` are optional source
  `ArrowStream`s the work consumes. The response is a **stream**: zero or more interim
  `ExecuteProgress` updates, then one terminal `ExecuteCompleted`.
  - `ExecuteProgress{fraction?, message}` — `fraction` is proto3 `optional`: ABSENT ==
    indeterminate progress (not a negative sentinel).
  - `ExecuteCompleted{success, error, result: WriteResult}` — `error` populated on
    failure.

## Conformance obligations

- Pass [`runtime-v1.json`](../../../../conformance/runtime-v1.json): determinate
  progress (fraction present, final == 1.0), indeterminate (fraction absent),
  zero-progress, failure (`success=false` + `error`), and an empty-`work_spec` error
  (`INVALID_ARGUMENT`).
- Streamed messages are named `ExecuteResponse` per the `*Response` convention buf
  STANDARD requires, even for streaming RPCs.

## Cross-cutting (every axis)

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field.
- **Streaming invocation is core-mediated via `InvokeServerStream`** ([ADR-008](../../../../../docs/architecture/adrs/008-streaming-capability-invocation.md))
  — the gateway enforces + stamps once at stream-open, then relays each
  `ExecuteResponse` frame opaquely. You implement a plain server-streaming
  `RuntimeService.Execute`.

## Writing a plugin

1. Implement `RuntimeService.Execute` (server-streaming) over your execution backend.
2. Stream `ExecuteProgress` for liveness (omit `fraction` when indeterminate), then a
   single terminal `ExecuteCompleted`.
3. Empty `work_spec` → `INVALID_ARGUMENT`.
4. Pass [`runtime-v1.json`](../../../../conformance/runtime-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/runtime/inmemory-go`](../../../../../examples/runtime/inmemory-go), [`inmemory-py`](../../../../../examples/runtime/inmemory-py) | 1 (wire) | in-thread work; the Go ref routes through the `InvokeServerStream` relay |
| [`examples/runtime/subprocess-py`](../../../../../examples/runtime/subprocess-py) | 2 (real) | each work unit runs in a real CHILD OS PROCESS — process isolation (distinct PID per unit), the seed of the sandboxing story |

## Related

[`runtime.proto`](runtime.proto) · [`runtime-v1.json`](../../../../conformance/runtime-v1.json) ·
[ADR-008](../../../../../docs/architecture/adrs/008-streaming-capability-invocation.md) (streaming invocation) ·
[`deployment_runtime.proto`](../../deploymentruntime/v1/deployment_runtime.proto) (the sibling "where plugins run" axis)
