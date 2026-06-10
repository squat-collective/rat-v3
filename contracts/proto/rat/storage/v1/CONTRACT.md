# `storage/v1` — plugin contract (author guide)

> **Status (2026-06-10) — the core is built and sealed.** What this guide describes **runs
> today**: capability routing, channel-authenticated plugin identity (C2, ADR-042), C5
> capability authz, deadline-bounding, and mandatory audit emission are enforced by the
> sealed core (`rat/2.0`, hardened through `rat/6.13`). `make conformance` checks the
> references against the golden vectors; `make composition` runs the cross-axis suite
> against real providers. The wire stays frozen (`rat/1`); post-freeze changes land as
> additive, capability-gated amendments (e.g. ADR-035 `delete` + ADR-049
> `create-if-absent` on `state/v1`).

> Canonical guide for implementing a `kind: storage` plugin. Pairs with the wire
> contract [`storage.proto`](storage.proto) and the golden vectors
> [`storage-v1.json`](../../../../conformance/storage-v1.json). Status: **v1 (frozen — rat/1, ADR-009)**.

## What a `storage` plugin is

A `kind: storage` plugin (S3, GCS, Azure Blob, local-fs) owns byte storage +
**credential vending**. The control plane NEVER sees bytes — it asks storage to vend
short-TTL, prefix-scoped, **tenant-scoped** credentials, and the engine/format then
talk to object storage directly with them. This is the **C7 tenancy enforcement
point**: a mis-scoped grant defeats an otherwise-correct tenancy boundary (reviews/01
Finding 3, reviews/04).

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://storage/v1/vend-credentials` | `VendCredentials` | issue short-TTL, prefix+tenant-scoped creds the data plane uses to read/write bytes directly |

(`rat://storage/v1/read` / `…/write` are declared capabilities satisfied by the vended
creds, not separate RPCs.)

## The RPC

- **`VendCredentials(prefix, mode)` → `{credentials, expires_unix_ms}`** —
  `prefix` is a **logical, provider-neutral** path (each backend resolves it per its
  own scheme). `mode` is `READ`/`WRITE`/`READ_WRITE` (`UNSPECIFIED` → `INVALID_ARGUMENT`).
  Empty `prefix` → `INVALID_ARGUMENT`. `credentials` is an opaque, provider-specific
  blob (an STS token set in production) marked `debug_redact` — never logged.
  `expires_unix_ms` is short-TTL; callers re-vend after it.

## Conformance obligations

1. **Tenant scoping is structural, not advisory** (C7) — the vended creds MUST be
   scoped to `context.identity.tenant` (read from the `rat-callmeta-bin` metadata
   header) **+ the requested prefix + mode**. The tenant comes ONLY from the
   core-stamped metadata, never a request field, so a caller cannot ask for another
   tenant's creds.
2. **Short TTL** — `expires_unix_ms` bounded.
3. Pass [`storage-v1.json`](../../../../conformance/storage-v1.json): vend
   READ/WRITE/READ_WRITE (the receipt asserts tenant + prefix + mode) + the 2 error
   vectors. (The conformance "scope receipt" is a JSON stand-in for an opaque STS
   token so the harness can assert the binding.)

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (CAS conflict, read-miss).

- **You READ the envelope.** `RequestContext` rides in the `rat-callmeta-bin` metadata
  header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md));
  storage is the axis that most depends on it — extract `identity.tenant` from
  metadata to scope the creds. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md)).
- The bytes path (using the vended creds) bypasses the core entirely.

## Writing a plugin

1. Implement `StorageService.VendCredentials`.
2. Read `identity.tenant` from the `rat-callmeta-bin` metadata; resolve `prefix` under
   the tenant's root and **enforce containment** (a prefix that escapes the tenant
   root → `PERMISSION_DENIED`).
3. Mint short-TTL provider creds scoped to (tenant, prefix, mode); never log them.
4. Pass [`storage-v1.json`](../../../../conformance/storage-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/storage/inmemory-go`](../../../../../plugins/storage/inmemory-go), [`inmemory-py`](../../../../../plugins/storage/inmemory-py) | 1 (wire) | first refs to READ tenant from the metadata envelope (C7) |
| [`plugins/storage/localfs-go`](../../../../../plugins/storage/localfs-go) | 2 (real) | real filesystem — PATH CONTAINMENT (escaping prefix → `PERMISSION_DENIED`) + TENANT ISOLATION (distinct per-tenant roots) |

## Related

[`storage.proto`](storage.proto) · [`storage-v1.json`](../../../../conformance/storage-v1.json) ·
[reviews/01](../../../../../reviews/01-adversarial-architect.md) F3 · [reviews/04](../../../../../reviews/04-security-reviewer.md) ·
[ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md) (identity in metadata)
