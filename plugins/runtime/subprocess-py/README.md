# rat-runtime-subprocess-py — ROUND-2 `runtime` reference (real isolation)

The **round-2** `runtime` reference: each `Execute` runs the work unit in a real
**child OS process** (`worker.py`) instead of in-thread. Runtime is the "where does
the code run" axis — this one actually runs it *somewhere else*.

It passes the **SAME shared golden vectors** as the in-memory runtime references
(`contracts/conformance/runtime-v1.json`) — the toy work_spec
(`{steps, rows, indeterminate, fail}`) is abstract enough that a child-process
runtime interprets it identically: emit `steps` progress events (with/without a
fraction) then a completion. And it earns a property the in-thread runtime can't:

| Round-2 property | Test | Why the in-thread runtime can't show it |
|---|---|---|
| **Process isolation** | `test_work_runs_in_a_separate_process` | the work unit reports a PID ≠ the server's | in-thread work is always the server's own PID |
| **Per-unit isolation** | `test_each_work_unit_gets_its_own_process` | two `Execute` calls run in two **distinct** child PIDs | in-thread, every unit shares one process |

(Process isolation is the seed of the real `deployment-runtime`/`runtime`
sandboxing story — a crashing or misbehaving work unit can't take the runtime down
with it. A container/WASM runtime is the natural next step up from a subprocess.)

## Files

| File | Role |
|---|---|
| `worker.py` | the work unit — runs in its own process; emits JSON progress/completed events (incl. its PID) |
| `server.py` | `Execute` spawns `worker.py`, streams its events as `ExecuteResponse`s; empty work_spec → `INVALID_ARGUMENT` |
| `main.py` | gRPC server entrypoint |
| `harness_test.py` | shared-vector cross-run + the two process-isolation tests |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/runtime/subprocess-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected:
`PASS — rat-runtime-subprocess-py conformed to runtime/v1 golden vectors + process isolation`.
