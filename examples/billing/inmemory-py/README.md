# rat-billing-inmemory-py — first `billing` reference

> ⚠️ **WIRE-CONTRACT REFERENCE — NOT PRODUCTION-HARDENED.** This validates the `billing/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory store**. A production `billing` plugin adds a durable/real backend + the enforcement the core will demand (Phase 1) — it demonstrates the contract, not a deployment. See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

A `kind: billing` plugin is a **metering sink**: the core emits usage events at
well-defined points (pipeline run, credential vend, storage bytes) and this plugin
**records + aggregates** them so a deployment can be costed/charged. This in-memory
reference is the conformance reference for the `billing/v1` axis — it drives the
shared golden vectors ([`contracts/conformance/billing-v1.json`](../../../contracts/conformance/billing-v1.json))
over real gRPC.

The single RPC under test is `Record`.

## What's special about this axis

Billing is **per-tenant by construction (C7).** Every usage event is metered under
the tenant the caller carried in the `rat-callmeta-bin` metadata header
([ADR-007](../../../docs/architecture/adrs/007-call-context-transport.md)) —
**never** a request field. There is no field on `RecordRequest` that could ask to
bill a different tenant, so a caller cannot move another tenant's totals. The server
reads `context.identity.tenant` and the ledger keys every write by it.

The ledger keeps two structures: an append-only list of recorded events per tenant
(for audit/replay) and a running per-`(tenant, meter)` sum, which is what the harness
asserts against.

## Capabilities

| Capability | RPC | Description |
|---|---|---|
| `rat://billing/v1/record` | `Record` | Record one or more meterable usage events for the caller's tenant; returns the count recorded. |

## Files

| File | Role |
|---|---|
| `store.py` | `BillingLedger`: per-tenant event log + per-`(tenant, meter)` aggregate |
| `server.py` | `BillingServicer.Record`; reads tenant from `rat-callmeta-bin`; meters per the metadata tenant (C7) |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/billing-v1.json` and drives this impl over real gRPC; asserts the recorded count, per-`(tenant, meter)` aggregation, and per-tenant isolation |

## How it's tested

- **Golden vectors** — records the vector cases and asserts `RecordResponse.recorded`
  per case, then asserts the per-`(tenant, meter)` aggregate matches `expect_aggregate`.
  The Rig shares a single `BillingLedger` with the test so aggregation can be read back
  directly after the gRPC calls.
- **Per-tenant aggregation** — multiple `Record` calls for the same meter fold into one
  running sum (`pipeline.run` recorded twice → `quantity = 2`).
- **Per-tenant isolation (C7)** — recording under `globex` against a shared ledger
  leaves `acme`'s aggregate unchanged; each tenant is metered independently.

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/billing/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-billing-inmemory-py conformed to billing/v1 golden vectors`.
