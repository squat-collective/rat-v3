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

## ⚠️ Driven directly, not gateway-mediated

Unlike the format/engine/storage references, runtime is **not** routed through the
stub core gateway. The gateway's `CapabilityInvokeService.Invoke` is **unary**
([invoke.proto](../../../contracts/proto/rat/core/v1/invoke.proto)), so it cannot
mediate a server-streaming capability — a contract finding this axis surfaced
([ideas/inbox.md](../../../ideas/inbox.md); a candidate streaming-invoke ADR). The
two references still cross-check on the shared golden vectors over real streaming
gRPC; the gateway-mediation seam is simply blocked until that ADR lands.

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
