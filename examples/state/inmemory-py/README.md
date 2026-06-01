# rat-state-inmemory-py — second `state-backend` reference (ADR-003)

> ⚠️ **WIRE-CONTRACT REFERENCE ONLY — NOT A STARTER TEMPLATE.** This round-1 reference validates the `state-backend/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory stand-in** — it deliberately fakes things a real plugin must not copy (in-process data stand-ins, ignored hints). For a production-shaped implementation, copy the **round-2 real backend** instead: [`sqlite-py`](../sqlite-py). See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The **second independent** `kind: state-backend` reference, and the one that
**closes out 0d round 1**: with it, all six data-plane axes have two cross-checked
references on shared golden vectors. A state-backend is a **tier-0** plugin — it
backs the core's State Gateway, and the core can't start without one.

The four RPCs under test:

| RPC | What the reference does |
|---|---|
| `Get` | read a key → `{found, value, revision}` |
| `Put` | write with optional compare-and-set; returns the **PutOutcome** enum (`COMMITTED` / `CONFLICT`) — a CAS conflict is a *normal outcome*, not a gRPC error |
| `List` | keys under a prefix (sorted) |
| `Watch` | **server-streaming** ordered replay of the change log (via the ADR-008 `InvokeServerStream` relay in the Go reference; direct here) |

It enforces the **KEY GRAMMAR** (freeze-blocker #3 / SEC-2): empty / oversize
(>512B) / NUL / control-char / path-traversal keys → `INVALID_ARGUMENT`. The
`grammar.py` validator mirrors the Go reference's `grammar.go` exactly.

> **Round-1 scope.** In-memory, so CAS is trivially linearizable and never returns
> `UNKNOWN`. The *semantic* test — does CAS actually serialize under contention,
> and is Watch genuinely ordered across writers? — is exactly what a **round-2**
> real backend (`state`=sqlite) is for. This reference validates the **wire
> contract**.

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/state/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-state-inmemory-py conformed to state/v1 golden vectors`.
