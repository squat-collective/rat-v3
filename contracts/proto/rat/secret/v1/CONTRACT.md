# `secret-backend/v1` — plugin contract (author guide)

> **Status (2026-06-10) — the core is built and sealed.** What this guide describes **runs
> today**: capability routing, channel-authenticated plugin identity (C2, ADR-042), C5
> capability authz, deadline-bounding, and mandatory audit emission are enforced by the
> sealed core (`rat/2.0`, hardened through `rat/6.13`). `make conformance` checks the
> references against the golden vectors; `make composition` runs the cross-axis suite
> against real providers. The wire stays frozen (`rat/1`); post-freeze changes land as
> additive, capability-gated amendments (e.g. ADR-035 `delete` + ADR-049
> `create-if-absent` on `state/v1`).

> Canonical guide for implementing a `kind: secret-backend` plugin. Pairs with the wire
> contract [`secret.proto`](secret.proto) and the golden vectors
> [`secret-v1.json`](../../../../conformance/secret-v1.json). Status: **v1 (frozen — rat/1.2, ADR-003: control-plane = one ref + conformance)**.

## What a `secret-backend` plugin is

A `kind: secret-backend` plugin (env, file, Vault, AWS Secrets Manager, GCP Secret Manager,
sealed-secrets) resolves opaque secret **references** to short-lived secret **values**, on demand,
at the point of use. Secrets NEVER appear in manifests, events, or logs — only the reference
string (e.g. `ref://vault/prod/db-password`) travels outside the resolution call. The value is
returned with a TTL hint; callers re-resolve rather than cache past it. Values MUST NOT be
persisted by the caller. Vended credentials for storage access go through `storage/v1`
`VendCredentials` — this axis is for arbitrary app-level secrets (API keys, DB passwords).

The load-bearing security property is **anti-enumeration**: a caller MUST NOT be able to
distinguish "this ref does not exist" from "this ref exists but you may not read it." Both
outcomes collapse to `found=false` with an empty value and an `OK` status. See [The RPCs](#the-rpcs)
and [Conformance obligations](#conformance-obligations) for the exact requirement.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://secret/v1/resolve` | `Resolve` | resolve an opaque secret reference → a short-lived value |

## The RPCs

- **`Resolve(secret_ref)` → `{found, value, expires_unix_ms}`** — resolve a secret reference for
  the caller's tenant (tenant read from `rat-callmeta-bin`, NOT a request field).

  **ANTI-ENUMERATION (reviews/06 API-1d / freeze-blocker #9, pinned at freeze):** `found=false`
  DELIBERATELY conflates "the ref does not exist" with "the ref exists but you are not authorized
  to read it." A caller MUST NOT be able to distinguish these two cases — collapsing them is the
  defense against an attacker enumerating which secret refs are real. Consequently:

  - Authorization failures return `found=false` + empty `value` + `OK` status — **never
    `PERMISSION_DENIED`**.
  - Cross-tenant refs (a ref that exists for another tenant) return `found=false` + empty `value`
    + `OK` status — indistinguishable from a missing ref.
  - `PERMISSION_DENIED` is **forbidden** as a response to a secret-resolution failure. It is
    reserved for C5/C7 capability-level enforcement by the core gateway before the plugin is
    reached.

  `value` carries `[debug_redact = true]` (reviews/06 SEC-8): proto reflection and text-marshal
  omit it structurally. The core's redaction obligation (reviews/04 I13) ensures it never appears
  in logs.

  `expires_unix_ms` is a TTL hint (unix epoch milliseconds); `0` means no expiry hint. Callers
  re-resolve rather than cache past this value.

  Empty `secret_ref` → `INVALID_ARGUMENT`.

  > **Timing side-channel (reviews/08 F3):** The response-layer anti-enumeration above is
  > structurally airtight — the wire shape is identical for miss and forbidden. Constant-time
  > backend resolution (so response latency does not leak "exists vs absent") is a documented
  > additive hardening target for GA; it is **not** a conformance gate at `rat/1`.

## Conformance obligations

Pass [`secret-v1.json`](../../../../conformance/secret-v1.json) via `make conformance`. The
vectors exercise the full anti-enumeration contract:

- `resolve/known` — a ref the caller's tenant owns resolves to `found=true` + the correct value.
- `resolve/known_apikey` — a second known ref for the same tenant.
- `resolve/unknown` — a ref that does not exist at all returns `found=false` (not an error status).
- `cross_tenant/cross_tenant_hidden` — re-issues a ref that EXISTS for `acme` under a DIFFERENT
  tenant (`wonka`) and asserts `found=false` — the same outcome as `resolve/unknown`. This is the
  anti-enumeration gate: if a plugin returns `PERMISSION_DENIED` or any distinguishable response
  for this case, it fails conformance.

The conformance harness drives `SecretService.Resolve` over real gRPC. All vectors run under the
top-level `"tenant": "acme"` except the `cross_tenant` vector, which overrides the tenant to
`"wonka"` in the metadata header.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed requests and infrastructure errors;
  in-response `bool` fields for normal domain outcomes. **The anti-enumeration requirement
  is the security-driven exception to the standard not-found rule** — see the error model's
  "Anti-enumeration" section for the normative statement.

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. The tenant identity MUST be read from that header (not from `secret_ref` or any
  request field — a request field could be forged by the caller). Invocation is core-mediated
  ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the plugin implements a plain gRPC `SecretService` server.

## Writing a plugin

1. Implement `SecretService` (`Resolve`) backed by your secret provider (env, file, Vault,
   AWS SM, GCP SM, …).
2. Read the caller's tenant from the `rat-callmeta-bin` metadata header (ADR-007). **Never** read
   it from the request message.
3. Key resolution on `(tenant, secret_ref)`. A ref for a different tenant MUST miss silently —
   return `found=false` + empty `value` + `OK`, identical to a genuinely unknown ref.
4. **Never return `PERMISSION_DENIED` for a secret lookup.** Authorization failures are
   `found=false`. `PERMISSION_DENIED` is raised only by the core gateway for capability-level
   enforcement (C5/C7) before the plugin is reached.
5. Set `expires_unix_ms` to the provider's expiry/rotation hint (or `0` if unknown).
6. Ensure `value` is never written to logs, traces, or state. The `debug_redact` annotation
   handles proto text-marshal; runtime redaction is the caller's and core's obligation.
7. Pass [`secret-v1.json`](../../../../conformance/secret-v1.json) via `make conformance`,
   including the `cross_tenant` anti-enumeration gate.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/secret/inmemory-py`](../../../../../plugins/secret/inmemory-py) | 1 (control-plane reference) | tenant-keyed in-memory store; anti-enumeration via `(tenant, secret_ref)` lookup key; cross-tenant miss indistinguishable from absent; TTL hint |

## Related

[`secret.proto`](secret.proto) · [`secret-v1.json`](../../../../conformance/secret-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) (anti-enumeration normative text) ·
[reviews/06](../../../../../reviews/06-proto-contract-review.md) API-1d ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md) F3
