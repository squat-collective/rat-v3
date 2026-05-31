# rat-secret-inmemory-py — `secret` reference (ADR-003 control-plane tier)

The `kind: secret-backend` reference. Control-plane axes get **one reference +
conformance** ([ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md));
this is it for `secret/v1`. An independently-written, in-memory implementation that
passes the **golden vectors** in
[`contracts/conformance/secret-v1.json`](../../../contracts/conformance/secret-v1.json).

Secret resolves an opaque **reference** (e.g. `ref://db/password`) to a short-TTL
value **at point of use**. Secrets never live in manifests, events, or logs — only
opaque references do (reviews/04 I13). This axis is for arbitrary app-level secrets
(API keys, DB passwords); storage-credential vending goes through `storage/v1`
`VendCredentials` instead. The single RPC under test is `Resolve`.

## Capabilities

| Capability | RPC | Behavior |
|---|---|---|
| `rat://secret/v1/resolve` | `Resolve` | `(secret_ref)` → `{found, value, expires_unix_ms}`, scoped to the caller's tenant |

## What's special about this axis — anti-enumeration found-semantics

`found=false` **deliberately conflates** "the ref does not exist" with "the ref
exists but you are not authorized to read it" (proto FOUND SEMANTICS, reviews/06
API-1d / freeze-blocker #9). A caller **MUST NOT** be able to distinguish the two:
a distinguishable "exists-but-forbidden" would leak which refs are real. So
authorization failures return `found=false` + empty value — **never** a
`PERMISSION_DENIED` status, never a distinguishable response. That is the entire
point of the axis.

## Tenant-scoping

Secrets are keyed by `(tenant, secret_ref)`, so resolution is intrinsically
tenant-scoped. The caller's tenant is read from `context.identity.tenant` in the
`rat-callmeta-bin` metadata header
([ADR-007](../../../docs/architecture/adrs/007-call-context-transport.md)) —
**never** a request field, so a caller cannot ask for another tenant's secrets. A
ref owned by a different tenant simply misses the lookup, falling into the same
`found=false` branch as a nonexistent ref.

## Files

| File | Role |
|---|---|
| `store.py` | `SecretStore`: in-memory `(tenant, secret_ref)` → value; anti-enumeration `resolve` returning `(found, value, expires)` |
| `server.py` | `Resolve`; reads tenant from `rat-callmeta-bin`; delegates to the store |
| `main.py` | gRPC server entrypoint (`$RAT_PLUGIN_ADDR`, default `127.0.0.1:0`) |
| `harness_test.py` | loads `contracts/conformance/secret-v1.json` and drives this impl over real gRPC; asserts found-semantics + value + the cross-tenant anti-enumeration property |

## How it's tested

The harness boots `SecretServicer` over real gRPC, sets `identity.tenant` in the
call metadata, and drives each `resolve` vector: known refs assert
`value.decode() == expected`; the unknown ref asserts `found=false` + empty value.
The **cross-tenant** vector re-resolves `ref://db/password` (which exists for `acme`)
under tenant `wonka` and asserts `found=false` — indistinguishable from a miss.

## Run it (containerized — no host installs)

```bash
# from the repo root
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/secret/inmemory-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — rat-secret-inmemory-py conformed to secret/v1 golden vectors`.
