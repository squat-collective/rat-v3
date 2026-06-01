# `identity/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: identity` plugin. Pairs with the wire
> contract [`identity.proto`](identity.proto) and the golden vectors
> [`identity-v1.json`](../../../../conformance/identity-v1.json). Status: **v1 (frozen — rat/1.2, ADR-003: control-plane = one ref + conformance)**.

## What a `identity` plugin is

A `kind: identity` plugin (anonymous, static-token, password, OAuth/OIDC, SAML, Keycloak) is the
**C2 keystone**: it backs the core's Identity Gateway (one of the six core things). Every request
the API gateway accepts is authenticated here before routing, and every coarse authz decision is
made here before the capability router forwards the call.

The split of responsibilities is precise:
- **Plugin side** — the *decision logic*: validate a credential into `(subject, tenant)`;
  evaluate a `(subject, action, resource)` against policy and return `(allowed, deny_code)`.
- **Core side** — the *routing enforcement*: raises `UNAUTHENTICATED` before any axis logic runs
  (C2); enforces per-capability `requires` from the manifest against the resolved subject (C5);
  stamps `RequestContext.identity.subject.principal` into `rat-callmeta-bin` (ADR-007) on the
  downstream hop so plugins never re-authenticate.

The **C2 default is NOT anonymous-root** (reviews/04). A conformant identity plugin issues a
per-plugin token at registration; `Authenticate` constant-time-compares it (see Reference
implementations). Anonymous-root is an explicit opt-in only available for local dev.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://identity/v1/authenticate` | `Authenticate` | validate an opaque credential → `(authenticated, subject, tenant)` |
| `rat://identity/v1/authorize` | `Authorize` | coarse allow/deny for a `(subject, action, resource)` |

## The RPCs

- **`Authenticate(credential)` → `{authenticated, subject, tenant}`** — `credential` is opaque
  bytes (bearer token, client-cert chain, etc.). The field carries `[debug_redact = true]` —
  it MUST NOT appear in logs. Returns `authenticated=true` + the resolved `subject` (stamped
  onto downstream `RequestContext.identity.subject.principal`) and `tenant` on success;
  `authenticated=false` + empty strings on failure. This is a **domain outcome**, not a gRPC
  status — a bad credential is a successful RPC that returns `authenticated=false`.

- **`Authorize(action, resource)` → `{allowed, deny_code, reason}`** — the subject is read
  **from the `rat-callmeta-bin` metadata envelope** (ADR-007), never from a request field. The
  core stamps it on the hop; the plugin reads `RequestContext.identity.subject.principal`. A deny
  is a **successful RPC** (`OK`) carrying `allowed=false` and a machine-readable `deny_code`
  (callers MUST branch on `deny_code`, never on `reason`). The `reason` string is
  **log/audit-only** — returning a rich reason to an untrusted caller is an enumeration oracle
  (reviews/04). See the `DenyCode` enum:

  | `DenyCode` | meaning |
  |---|---|
  | `DENY_CODE_UNSPECIFIED` | not a deny (`allowed=true`) OR reason not supplied |
  | `DENY_CODE_NOT_AUTHENTICATED` | no/invalid subject in metadata |
  | `DENY_CODE_INSUFFICIENT_ROLE` | authenticated but lacks required role |
  | `DENY_CODE_ACTION_FORBIDDEN` | action denied for this subject |
  | `DENY_CODE_RESOURCE_FORBIDDEN` | specific resource denied |

`RequestContext` has no subject field in `AuthenticateRequest` (field 1 is `reserved`) — this RPC
is precisely what *establishes* the subject. Trace correlation metadata still applies via the
`rat-callmeta-bin` header.

## Conformance obligations

Authentication and authorization correctness are gated by the golden vectors in
[`identity-v1.json`](../../../../conformance/identity-v1.json).

**Authentication vectors** drive `Authenticate` with an opaque credential and assert
`(authenticated, subject, tenant)`:
- `good_admin` — valid token resolves to `(alice, acme)`.
- `bad_token` — invalid token returns `authenticated=false`.

**Authorization vectors** set the end-user subject in `rat-callmeta-bin` metadata, drive
`Authorize`, and assert `(allowed, deny_code)` (enum NAME without the `DENY_CODE_` prefix):
- `admin_can_run` — alice with `runner` role may `pipeline.run`.
- `viewer_cannot_run` — bob with `viewer` role denied `pipeline.run` → `INSUFFICIENT_ROLE`.
- `anon_denied` — empty subject denied → `NOT_AUTHENTICATED`.

**Two constant-time requirements** the vectors do not enforce but the contract demands:
1. `Authenticate` MUST compare credentials using a constant-time algorithm (e.g.
   `hmac.compare_digest`) so a bad token cannot be distinguished from a good one by timing.
2. The implementation MUST iterate ALL candidates without early-return on first match, for
   the same reason.

Pass [`identity-v1.json`](../../../../conformance/identity-v1.json) via `make conformance`.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (auth failure, deny).
  For this axis specifically: `UNAUTHENTICATED` is raised by the **core gateway** (C2) before
  reaching the plugin, not by the plugin itself. A bad credential returned by `Authenticate` is
  `authenticated=false`, not a gRPC failure. A deny returned by `Authorize` is `allowed=false`
  with a `deny_code`, not a `PERMISSION_DENIED` status.

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the plugin implements a plain gRPC `IdentityService` server.

## Writing a plugin

1. Implement `IdentityService` (`Authenticate` + `Authorize`) over your identity backend.
2. Implement `Authenticate` with **constant-time credential comparison** (iterate all candidates;
   no early return; use `hmac.compare_digest` or equivalent). `credential` is `[debug_redact =
   true]` — treat it as opaque bytes and never log it.
3. Implement `Authorize` reading the subject from `rat-callmeta-bin` metadata, NOT from a request
   field. Return `allowed=false` + an appropriate `DenyCode` for every deny case. Return
   `reason` only for log/audit sinks — never expose it to the calling client.
4. Default to the non-anonymous-root policy. If your plugin is the C2 default, issue a
   per-plugin token at registration and verify it in `Authenticate`.
5. Pass [`identity-v1.json`](../../../../conformance/identity-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`examples/identity/static-token-py`](../../../../../examples/identity/static-token-py) | 1 (control-plane reference) | constant-time auth, role-based coarse authz, subject from `rat-callmeta-bin`, `deny_code` branching |

## Related

[`identity.proto`](identity.proto) · [`identity-v1.json`](../../../../conformance/identity-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) ·
[reviews/04](../../../../../reviews/04-security-reviewer.md) ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md)
