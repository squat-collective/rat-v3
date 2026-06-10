# `tenancy/v1` — plugin contract (author guide)

> **Status (2026-06-10) — the core is built and sealed.** What this guide describes **runs
> today**: capability routing, channel-authenticated plugin identity (C2, ADR-042), C5
> capability authz, deadline-bounding, and mandatory audit emission are enforced by the
> sealed core (`rat/2.0`, hardened through `rat/6.13`). `make conformance` checks the
> references against the golden vectors; `make composition` runs the cross-axis suite
> against real providers. The wire stays frozen (`rat/1`); post-freeze changes land as
> additive, capability-gated amendments (e.g. ADR-035 `delete` + ADR-049
> `create-if-absent` on `state/v1`).

> Canonical guide for implementing a `kind: tenancy` plugin. Pairs with the wire
> contract [`tenancy.proto`](tenancy.proto) and the golden vectors
> [`tenancy-v1.json`](../../../../conformance/tenancy-v1.json). Status: **v1 (frozen — rat/1.2; ADR-003: control-plane = one ref + conformance)**.

## What a `tenancy` plugin is

A `kind: tenancy` plugin (none, namespace, org, hierarchical) answers POLICY questions the
core poses at tenant-boundary decision points — permission checks, cross-tenant sharing
grants, quota tests. It is a **C7 STRUCTURAL** axis: tenant isolation is *not* an emergent
property of plugins agreeing; it is enforced directly by core primitives. `RequestContext.identity.tenant`
threads every RPC, the state gateway namespaces by it, and storage vends tenant-scoped
credentials. This plugin does NOT re-implement isolation — the core enforces the boundary
structurally. The plugin only computes verdicts the core can't hardcode (e.g. "may tenant A
share dataset X with tenant B?", "is tenant A over quota?"), and the core enforces them.

The dangerous reading — "isolation is emergent" — is explicitly rejected: see
[tenancy.proto](tenancy.proto) header (C7) and [reviews/00](../../../../../reviews/00-synthesis.md) Theme 4,
[reviews/01](../../../../../reviews/01-adversarial-architect.md) Finding 3.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://tenancy/v1/decide` | `Decide` | answer a policy decision the core poses at a hook point; the core enforces the verdict |

## The RPCs

- **`Decide(kind, subject_action, counterparty_tenant)` → `{allowed, deny_code, reason}`** —
  compute a policy verdict for the calling tenant. The caller's tenant is read by the core
  from the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md))
  — it is **not a request field** — so a plugin can never be tricked into deciding as a
  different tenant.

  **`kind`** selects the decision class:
  - `DECISION_KIND_PERMISSION` (1) — in-tenant permission check (e.g. `pipeline.run`).
  - `DECISION_KIND_SHARING` (2) — cross-tenant share request; `counterparty_tenant` names
    the other side.
  - `DECISION_KIND_QUOTA` (3) — quota/limit check; stateful (the plugin tracks consumption).
  - `DECISION_KIND_UNSPECIFIED` (0) / unknown — MUST deny with `DENY_CODE_POLICY_FORBIDDEN`.

  **`allowed: bool`** — the verdict. `deny_code` is set only when `allowed = false`.

  **`deny_code: DenyCode`** — machine-readable deny reason; callers MUST branch on this,
  never on `reason` (anti-enumeration-oracle rule):
  - `DENY_CODE_POLICY_FORBIDDEN` (1) — a tenancy policy forbids the action.
  - `DENY_CODE_QUOTA_EXCEEDED` (2) — a quota or limit was exceeded.
  - `DENY_CODE_CROSS_TENANT_DENIED` (3) — the cross-tenant share/access is denied.
  - `DENY_CODE_UNSPECIFIED` (0) — not a deny (`allowed = true`), or reason not supplied.

  **`reason: string`** — human-readable diagnostic. **LOG/AUDIT-ONLY.** Callers MUST NOT
  branch on this string; it is unstable across versions and attacker-influenceable.

  A deny on a successful `Decide` is an **in-band domain outcome** (the call returns `OK`;
  `allowed = false` + `deny_code` carry the result). Transport/operational failures use
  gRPC status codes per the [error model](../../common/v1/ERROR_MODEL.md).

## Conformance obligations

`Decide` is the single RPC. Pass [`tenancy-v1.json`](../../../../conformance/tenancy-v1.json):
the ordered sequence of six decisions that must be evaluated in order (quota is stateful):

1. `permission_ok` — `PERMISSION` for `pipeline.run` → `allowed: true`.
2. `share_allowlisted` — `SHARING`, `counterparty_tenant: partner` → `allowed: true`.
3. `share_denied` — `SHARING`, `counterparty_tenant: rival` → `allowed: false`, `deny_code: CROSS_TENANT_DENIED`.
4. `quota_1` — first `QUOTA` decision → `allowed: true` (counter = 1 of 2).
5. `quota_2` — second `QUOTA` decision → `allowed: true` (counter = 2 of 2).
6. `quota_over` — third `QUOTA` decision → `allowed: false`, `deny_code: QUOTA_EXCEEDED` (counter = 3, exceeds limit).

**Order is mandatory** because `QUOTA` is stateful (a per-tenant counter). The vectors
must be run in sequence against the same plugin instance.

The `_comment` in [`tenancy-v1.json`](../../../../conformance/tenancy-v1.json) documents
the reference allowlist (`acme` may share with `partner` only) and the quota ceiling
(`QUOTA_LIMIT = 2`). A conformant plugin may use different policy data — but MUST still
honour the `deny_code` contract and the `reason`-is-log-only invariant.

ADR-003 control-plane rule: one reference + conformance (no second reference required for
this axis).

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response enum/`bool` fields for normal domain outcomes (a deny is a domain
  outcome, not a gRPC error).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the tenancy plugin implements a plain gRPC `TenancyService` server.

## Writing a plugin

1. Implement `TenancyService` (`Decide`) in your language of choice.
2. Read the caller's tenant **from the `rat-callmeta-bin` metadata header only** (via the
   `RequestContext.identity.tenant` field) — never from a request field. The reference
   [`server.py`](../../../../../plugins/tenancy/inmemory-py/server.py) shows the correct
   pattern; `store.py` shows the pure policy logic with no gRPC coupling.
3. Return `allowed = true` + `DENY_CODE_UNSPECIFIED` on success. Return `allowed = false`
   + the appropriate `deny_code` on deny. Never set `deny_code` when `allowed = true`.
4. Populate `reason` for logs/audit if useful; mark it clearly as non-branching in your
   docs. Do NOT return different `deny_code` values than the four defined ones.
5. Treat `DECISION_KIND_UNSPECIFIED` (and any unknown kind) as an implicit deny with
   `DENY_CODE_POLICY_FORBIDDEN`.
6. For stateful decisions (quota, rate limits): the plugin owns the counter/state backend.
   The core does not track quota on the plugin's behalf.
7. Pass [`tenancy-v1.json`](../../../../conformance/tenancy-v1.json) via `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/tenancy/inmemory-py`](../../../../../plugins/tenancy/inmemory-py) | 1 (control-plane reference) | in-memory policy engine — permission, cross-tenant allowlist (SHARING), stateful quota counter; tenant read from `rat-callmeta-bin` metadata |

## Related

[`tenancy.proto`](tenancy.proto) · [`tenancy-v1.json`](../../../../conformance/tenancy-v1.json) ·
[`../../common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[reviews/00](../../../../../reviews/00-synthesis.md) Theme 4 ·
[reviews/01](../../../../../reviews/01-adversarial-architect.md) Finding 3 ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (control-plane freeze rule)
