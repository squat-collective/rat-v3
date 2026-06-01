# rat-storage-inmemory-py — second `storage` reference (ADR-003)

> ⚠️ **WIRE-CONTRACT REFERENCE ONLY — NOT A STARTER TEMPLATE.** This round-1 reference validates the `storage/v1` wire contract (proto shapes, error model, the cross-cutting `rat-callmeta-bin` envelope) with an **in-memory stand-in** — it deliberately fakes things a real plugin must not copy (in-process data stand-ins, ignored hints). For a production-shaped implementation, copy the **round-2 real backend** instead: [`localfs-go`](../localfs-go). See [reviews/08](../../../reviews/08-post-freeze-board-review.md) E3.

The **second independent** `kind: storage` reference. It satisfies the
[ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
gate for `storage/v1`: an independently-written implementation (different language,
different code path) that passes the **same golden vectors** as the first
([`inmemory-go`](../inmemory-go)).

Storage owns byte storage + **credential vending**. The control plane never sees
bytes — it asks storage to vend short-TTL, prefix-scoped, **tenant-scoped**
credentials, and the engine/format then talk to object storage directly. The single
RPC under test is `VendCredentials`.

## What's special about this axis

It is the **first reference that consumes identity from the metadata envelope.**
Tenant-scoping is storage's whole job (the C7 enforcement point — reviews/01
Finding 3, reviews/04), so the server reads `context.identity.tenant` from the
`rat-callmeta-bin` metadata header ([ADR-007](../../../docs/architecture/adrs/007-call-context-transport.md))
— **never** a request field, so a caller cannot ask for another tenant's creds.

The credential blob is a **conformance scope receipt** — JSON
`{tenant, prefix, mode, expires_unix_ms}` standing in for a real opaque STS token —
so the harness can assert the security obligation: the vended creds are bound to the
caller's tenant + the requested prefix + mode, with a short TTL.

## Files

| File | Role |
|---|---|
| `server.py` | `VendCredentials`; reads tenant from `rat-callmeta-bin`; empty prefix / unspecified mode → `INVALID_ARGUMENT` |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/storage-v1.json` and drives this impl over real gRPC; asserts scope + TTL + the tenant-from-metadata C7 property |

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/storage/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-storage-inmemory-py conformed to storage/v1 golden vectors`.
