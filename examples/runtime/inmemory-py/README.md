# rat-runtime-inmemory-py — second `runtime` reference (ADR-003)

The **second independent** `kind: runtime` reference. It satisfies the
[ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
gate for `runtime/v1`: an independently-written implementation (different language,
different code path) that passes the **same golden vectors** as the first
([`inmemory-go`](../inmemory-go)).

A runtime executes a unit of work and **streams liveness** back. `runtime/v1` is the
first 0d axis with a **server-streaming** RPC: `Execute` returns a stream of
`ExecuteResponse` (a `progress | completed` oneof) — interim `ExecuteProgress`
updates then a terminal `ExecuteCompleted`.

The reference runs a tiny work_spec — JSON `{steps, rows, indeterminate, fail}` —
emitting `steps` progress messages (fraction `(i+1)/steps`, or **absent** when
indeterminate, exercising the proto3 optional double) then a terminal completion
carrying `success` + `WriteResult.rows_affected`, or `success=false`+`error` when
`fail` is set. The point under test is the **streaming wire contract**, not compute.

## Gateway mediation (ADR-008)

This axis surfaced a contract finding: the gateway's original `Invoke` was **unary**
and couldn't mediate a server-streaming capability. That was resolved by
[ADR-008](../../../docs/architecture/adrs/008-streaming-capability-invocation.md),
which added `InvokeServerStream` to
[invoke.proto](../../../contracts/proto/rat/core/v1/invoke.proto). The **Go**
reference (`inmemory-go`) now routes `Execute` through the stub gateway's streaming
relay, exercising the C1/C5/C8 + identity-stamp seams on a streaming call. This
**Python** harness, like the other Python references, drives the plugin **directly**
over real streaming gRPC (the stub gateway lives only in the Go reference). Both
still cross-check on the same shared golden vectors.

## Files

| File | Role |
|---|---|
| `server.py` | `Execute` (streaming generator); empty work_spec → `INVALID_ARGUMENT` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/runtime-v1.json`; drives Execute, asserts progress framing + optional-fraction presence + terminal completion |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/runtime/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-runtime-inmemory-py conformed to runtime/v1 golden vectors`.
