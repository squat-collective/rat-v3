# rat-format-inmemory-py — second `format` reference (ADR-003)

> ⚠️ **WIRE-CONTRACT REFERENCE ONLY — NOT A STARTER TEMPLATE.** This round-1 reference validates the `format/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory stand-in** — it deliberately fakes things a real plugin must not copy (in-process data stand-ins, ignored hints). For a production-shaped implementation, copy the **round-2 real backend** instead: [`parquet-py`](../parquet-py) / [`delta-py`](../delta-py). See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The **second independent** `kind: format` reference implementation. Its sole job is
to satisfy the [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
gate for `format/v1`: a second, independently-written implementation (different
language, different code path) that passes the **same golden vectors** as the first
([`inmemory-go`](../inmemory-go)), so the contract can advance from `v1-preview`
toward `v1`.

It is **not** production storage. Like the Go reference it keeps tables in memory and
carries the "bulk" Arrow leg through an in-process ticket registry — the control-plane
wire contract (Resolve / Append / Merge / Overwrite / Maintain) is what's validated.
The real Arrow Flight data wire is deferred to a production reference.

## Files

| File | Role |
|---|---|
| `store.py` | in-memory ordered-row tables: append / merge(upsert) / overwrite / scan / maintain |
| `streams.py` | in-process stand-in for the out-of-band Arrow leg (single-use ticket registry) |
| `server.py` | the five `FormatService` RPCs; empty `TableRef` / missing `merge_keys` → `INVALID_ARGUMENT` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/format-v1.json` and drives this impl through it over real gRPC |

## The golden vectors

The harness loads the **shared** vectors from
[`contracts/conformance/format-v1.json`](../../../contracts/conformance/format-v1.json)
— the exact same file the Go reference loads. That shared artifact is how "run against
each other on golden data" is satisfied literally rather than by convention.

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/format/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-format-inmemory-py conformed to format/v1 golden vectors`.

Runs under pytest too (the `test_*` functions) if you prefer:
`pip install -q -r requirements.txt pytest && pytest -q`.

## Why no SDK packaging

The generated Python SDK lives at `contracts/sdks/python/` (vendored, per
[ADR-006](../../../docs/architecture/adrs/006-sdk-distribution-and-plugin-layout.md)).
The plugin imports it via `PYTHONPATH` rather than a published package — Phase 0 is
pre-distribution. Packaging is a later concern.
