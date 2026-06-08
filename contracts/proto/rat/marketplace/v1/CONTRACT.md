# `marketplace/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> the reference against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: marketplace` plugin. Pairs with the wire
> contract [`marketplace.proto`](marketplace.proto) and the golden vectors
> [`marketplace-v1.json`](../../../../conformance/marketplace-v1.json). Status: **v1 (frozen — rat/1.4; ADR-003: experience = one ref + conformance)**.

## What a `marketplace` plugin is

A `kind: marketplace` plugin (community-open, curated internal registry, enterprise vendor
catalog) is plugin **distribution + discovery**. It is an EXPERIENCE-axis plugin — there is no
data plane, no Arrow exchange, no branch model. Multiple marketplaces coexist; the solo bundle
ships a community one ([ADR-002](../../../../../docs/architecture/adrs/002-founding-tech-stack.md) D9).

The load-bearing job of this axis — the thing a marketplace *must* do that no other axis
does — is the **capability-aware compatibility filter**: given a caller's
`deployment_capabilities`, `Search` returns only listings whose `required_capabilities` are
fully satisfied by that set. "Does this plugin work on MY deployment?" is answerable precisely
because every listing MUST declare its `provided_capabilities` and `required_capabilities` as
mandatory fields (not optional metadata). A marketplace that cannot answer the compatibility
question has failed its one hard job (reviews/02 N2 + Stage 7).

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://marketplace/v1/search` | `Search` | capability-aware plugin discovery |
| `rat://marketplace/v1/get` | `Get` | full detail for one listing |

## The RPCs

- **`Search(query, kind, deployment_capabilities)` → `{listings: [Listing]}`** — free-text
  `query` (case-insensitive substring match against `plugin_id` / `description`; empty ==
  no filter), `kind` (exact axis filter; empty == all axes). The `deployment_capabilities`
  field is the compatibility gate: when non-empty, keep only listings whose
  `required_capabilities` are ALL present in the deployment set (`required ⊆ deployment`).
  Returns an empty list for zero matches — never `NOT_FOUND`.

- **`Get(plugin_id)` → `{listing: Listing}`** — full detail for one listing. Unknown
  `plugin_id` → `NOT_FOUND`.

### The `Listing` message — mandatory fields

`provided_capabilities`, `required_capabilities`, and `conformed_capabilities` are **not**
optional metadata — they are mandatory (reviews/02 N2). A listing without them cannot
participate in compatibility filtering and MUST NOT be accepted by a conformant
implementation. `signed` + `signed_by` carry supply-chain trust signals (C8). `support_url`
is the blame-attribution anchor (reviews/02 Stage 8): `rat diagnose` surfaces it when a
failure traces to this plugin.

> ⚠️ **Honesty (reviews/08 D4):** `conformed_capabilities` is currently **self-asserted**.
> There is no enforcer yet — the field is populated by the listing author, and the community
> reference hardcodes `conformed = provided` for its conformance-passing entries. In Phase 1,
> the marketplace WILL verify a signed conformance attestation (axis + vector-hash + result +
> signer), and `conformed_capabilities` will be derived from that attestation, not free text.
> Until then, treat `conformed_capabilities` as a declaration of intent, not a guarantee.

## Conformance obligations

Pass [`marketplace-v1.json`](../../../../conformance/marketplace-v1.json): the vectors cover
three behaviors the implementation must get right:

1. **Kind filter** (`by_kind_engine`) — `Search` with `kind="engine"` returns only engine
   listings.
2. **Compatibility gate — unsatisfied** (`compat_unsatisfied`) — a `strategy` listing that
   requires `rat://format/v1/merge` is **excluded** when the deployment's
   `deployment_capabilities` don't include it.
3. **Compatibility gate — satisfied** (`compat_satisfied`) — the same listing IS included once
   `rat://format/v1/merge` is present in `deployment_capabilities`.
4. **Get — known** (`get_engine`) — returns the full listing including `signed: true`.
5. **Get — unknown** (`get_unknown`) — `plugin_id` not in catalog → `NOT_FOUND`.

The subset test (`required ⊆ deployment`) is the axis's core invariant. An implementation
that passes the kind filter but silently returns incompatible listings fails the load-bearing
requirement.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response fields for normal domain outcomes (there are none for this axis —
  empty `listings` is the normal zero-match outcome, not an error).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header
  ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)), not a
  field (`reserved 1` in both request messages). However, this axis is **deployment-scoped,
  not tenant-scoped**: discovery is about what plugins exist and what they need, not about any
  tenant's data. The reference implementation therefore performs no `rat-callmeta-bin`
  processing; the gateway still validates the caller credential (C2) before routing
  ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md)).

## Writing a plugin

1. Implement `MarketplaceService` (`Search` / `Get`) over your listing store (community
   registry, internal artifact store, enterprise vendor catalog, etc.).
2. Ensure every `Listing` your store exposes carries non-empty `provided_capabilities`,
   `required_capabilities`, `conformed_capabilities`, `signed`, and `support_url`. A listing
   missing these fields is a malformed listing.
3. Implement the compatibility filter: `Search` with a non-empty `deployment_capabilities`
   MUST exclude any listing where `required_capabilities ⊄ deployment_capabilities`. This
   is a set-subset check, not a string match.
4. Return `NOT_FOUND` for `Get` of an unknown `plugin_id`. Return an empty `listings` list
   (not an error) for `Search` with zero matches.
5. Pass [`marketplace-v1.json`](../../../../conformance/marketplace-v1.json) via
   `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/marketplace/community-py`](../../../../../plugins/marketplace/community-py) | 1 (wire) | capability-aware filter (the axis's load-bearing job); three real-ish RAT plugin listings; `NOT_FOUND` on unknown `plugin_id` |

## Related

[`marketplace.proto`](marketplace.proto) · [`marketplace-v1.json`](../../../../conformance/marketplace-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (experience = one ref + conformance) ·
[reviews/02](../../../../../reviews/02-plugin-ecosystem-builder.md) Stage 7 + Stage 8 ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md) D4
