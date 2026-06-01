# rat-tenancy-inmemory-py — `tenancy` reference (control-plane)

> ⚠️ **WIRE-CONTRACT REFERENCE — NOT PRODUCTION-HARDENED.** This validates the `tenancy/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory store**. A production `tenancy` plugin adds a durable/real backend + the enforcement the core will demand (Phase 1) — it demonstrates the contract, not a deployment. See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The control-plane `kind: tenancy` reference. It loads the
[`tenancy-v1.json`](../../../contracts/conformance/tenancy-v1.json) golden
vectors and drives this implementation's `TenancyService.Decide` over real gRPC —
the one-reference-plus-conformance bar for a control-plane axis (data-plane axes
get the two-reference [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
treatment; tenancy gets one reference + conformance).

A `tenancy` plugin answers tenant-boundary **policy** questions the core poses at
decision points — a permission check, a sharing grant, a quota test — via the
single RPC `Decide`. The core enforces the verdict; the plugin only computes it.

## What's special about this axis

**Tenancy is policy ON TOP of the core's structural C7 isolation — it does NOT
re-implement isolation.** Per the [`tenancy.proto`](../../../contracts/proto/rat/tenancy/v1/tenancy.proto)
header (reviews/00 Theme 4, reviews/01 Finding 3): the tenant dimension already
lives in core primitives — `identity.tenant` threads every RPC, the state gateway
namespaces by it, storage vends tenant-scoped creds. The dangerous reading
("isolation is an emergent property of plugins agreeing") is rejected: isolation
is the core's job. This plugin only answers the **policy** the core can't
hardcode (e.g. "may tenant A share dataset X with tenant B?").

Like the storage reference, the caller's tenant is read from the
`rat-callmeta-bin` metadata header ([ADR-007](../../../docs/architecture/adrs/007-call-context-transport.md))
— **never** a request field — so a caller cannot pose a decision as another
tenant.

**Error model:** a deny on a successful `Decide` is in-band — the `allowed` bool
plus an enumerated `deny_code`. Callers branch on `deny_code`, **never** on the
free-text `reason` (anti-enumeration-oracle; same convention as
`identity.Authorize`).

## Capabilities

| Capability | RPC | Role |
|---|---|---|
| `rat://tenancy/v1/decide` | `Decide` | answer a tenant-boundary policy decision the core poses at a hook point |

## Decision kinds & deny codes (this reference)

| `DecisionKind` | Reference behavior | Deny code on failure |
|---|---|---|
| `PERMISSION` | always allowed (in-tenant; finer RBAC is identity's job) | — |
| `SHARING` | allowed iff `counterparty_tenant` is in the tenant's allowlist (`acme → {partner}`) | `CROSS_TENANT_DENIED` |
| `QUOTA` | allowed while the per-tenant counter ≤ `QUOTA_LIMIT` (2); **stateful** | `QUOTA_EXCEEDED` |

`QUOTA` is stateful — the per-tenant counter lives on the policy instance and is
incremented on every QUOTA decide — so the golden vectors are evaluated **in
order** (two allowed, the third over the limit).

## Files

| File | Role |
|---|---|
| `store.py` | `TenancyPolicy` — pure decision logic (allowlist + stateful quota counter), no gRPC |
| `server.py` | `Decide`; reads tenant from `rat-callmeta-bin`; delegates to `TenancyPolicy` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/tenancy-v1.json` and drives this impl over real gRPC, in order |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/tenancy/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-tenancy-inmemory-py conformed to tenancy/v1 golden vectors`.
