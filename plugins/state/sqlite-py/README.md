# rat-state-sqlite-py — ROUND-2 `state` reference (real backend)

The **round-2** `state-backend` reference: a *technologically-divergent* backend, not
another in-memory twin. Where [`inmemory-go`](../inmemory-go) and
[`inmemory-py`](../inmemory-py) hold state in a hashmap behind a mutex, this backs it
with **sqlite** — a real embedded transactional SQL database, file-on-disk, WAL.

This is the [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
*spirit* (not just letter): "different underlying technologies … different
consistency/semantic profiles." It earns that by doing two things the in-memory
twins **cannot**:

| Round-2 property | Test | Why the in-memory twin can't show it |
|---|---|---|
| **Durability** | `test_durability_survives_reopen` | write → close store → reopen the same db file → state is still there | a hashmap dies with the process |
| **Linearizable CAS** | `test_linearizable_cas_one_winner` | N threads race a compare-and-set from the same revision → **exactly one** COMMITs | the in-memory CAS is serialized by an *in-process mutex*; here it's enforced by **sqlite's `BEGIN IMMEDIATE`** locking — durable, cross-connection, the real lease primitive (reviews/06 C-4) |

And, crucially, it **passes the SAME shared golden vectors** as the in-memory
references (`contracts/conformance/state-v1.json`) — a real backend conforming to the
identical wire contract is the actual round-2 ADR-003 evidence.

> Note on independence: the divergence ADR-003 cares about here is the **backend
> technology** (real transactional SQL DB vs in-memory hashmap), which is orthogonal
> to language. Cross-checked against `inmemory-go` (different language) and
> `inmemory-py` (same language, different backend).

## Files

| File | Role |
|---|---|
| `store.py` | the sqlite-backed store: schema, transactional CAS (`BEGIN IMMEDIATE`), global revision, change log for Watch |
| `grammar.py` | KEY GRAMMAR validator (identical to the in-memory refs) |
| `server.py` | the four `StateService` RPCs over the sqlite store |
| `main.py` | gRPC server entrypoint (`$RAT_STATE_DB`, `$RAT_PLUGIN_ADDR`) |
| `harness_test.py` | shared-vector cross-run + the two round-2 semantic tests |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/state/sqlite-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected:
`PASS — rat-state-sqlite-py conformed to state/v1 golden vectors + durability + linearizable CAS`.
