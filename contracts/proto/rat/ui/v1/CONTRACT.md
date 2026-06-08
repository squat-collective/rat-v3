# `ui/v1` — plugin contract (author guide)

> ⚠️ **Status (2026-06-01) — the orchestrating core is NOT built yet (Phase 1).** The C2/C5/C7
> enforcement, capability routing, and audit emission this guide describes are the contract the
> core MUST implement — they do **not** run today. The wire contract + reference plugin here are
> real and frozen (`rat/1`); the core is *designed, not running*, and `make conformance` tests
> references against golden vectors, **not** a live deployment. See
> [reviews/08](../../../../../reviews/08-post-freeze-board-review.md).

> Canonical guide for implementing a `kind: ui` plugin. Pairs with the wire
> contract [`ui.proto`](ui.proto) and the golden vectors
> [`ui-v1.json`](../../../../conformance/ui-v1.json). Status: **v1 (frozen — rat/1.4; ADR-003: experience = one ref + conformance)**.

## What a `ui` plugin is

A `kind: ui` plugin (web-portal, CLI, Slack bot, VS Code extension) is an **experience
surface** — the human interface layer of the platform. The multi-UI story (overview.md /
Phase 5) is "each UI is a separate `ui` plugin," so the contract is deliberately thin: a UI
plugin mostly **CONSUMES the API gateway like any other client**. What the core needs from a
`ui` plugin is narrow: (a) discovery of the surfaces and hosted slots it exposes, and (b)
resolution of contributed components into render info.

**The slot/contribution model.** A `ui` plugin declares which slots it can host
(`HostedSlot.slot` — a capability-shaped URI, e.g. `rat://ui/v1/pipeline-detail`). Other
plugins declare which components they contribute into those slots via `contributes.slots` in
their `plugin.v1.json` manifest, naming the target slot URI. At render time the UI calls
`RenderSlot(slot, component)` to obtain the asset ref and props schema it needs to mount the
contributed component. The registry brokers the discovery; the UI plugin resolves the
concrete render payload.

This axis carries **no tenant/context scoping** — `Describe`/`RenderSlot` are surface
metadata, not tenant-scoped operations. There is no `rat-callmeta-bin` handling required.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://ui/v1/describe` | `Describe` | enumerate the surfaces this UI exposes and the slots it can host contributions into |
| `rat://ui/v1/render-slot` | `RenderSlot` | resolve a contributed component for a slot → asset ref + props schema |

## The RPCs

- **`Describe()` → `{display_name, slots: [HostedSlot]}`** — returns a human label for this
  UI surface and the set of slots it hosts. Each `HostedSlot` carries the slot URI and a
  short description. No request body (context travels in `rat-callmeta-bin` per ADR-007, but
  this axis does not consume it). The response MUST include all slots the plugin is prepared
  to host; the registry uses this set to route `contributes.slots` declarations from peer
  plugins.

- **`RenderSlot(slot, component)` → `{asset_ref, props_schema}`** — given a slot URI and a
  contributing component name (as declared in the contributor's `contributes.slots`), returns
  the asset reference (e.g. an OCI image ref or JS bundle URL) the UI loads and a
  JSON Schema (serialized as `bytes`) describing the props the component accepts. Unknown
  `(slot, component)` pair → `NOT_FOUND`. Empty `slot` or `component` → `INVALID_ARGUMENT`.

## Conformance obligations

Pass [`ui-v1.json`](../../../../conformance/ui-v1.json) via `make conformance`. The harness
asserts:

1. **`Describe`** returns a non-empty `display_name` and a set of slot URIs that matches the
   declared slots (order-insensitive). The reference set includes
   `rat://ui/v1/pipeline-detail` and `rat://ui/v1/dataset-overview`.

2. **`RenderSlot` — known component** (`slot=rat://ui/v1/pipeline-detail`,
   `component=lineage-graph`) returns a non-empty `asset_ref` and a non-empty JSON-object
   `props_schema`.

3. **`RenderSlot` — unknown component** (`component=does-not-exist`) returns `NOT_FOUND`.

Per [ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md),
the experience axis requires one reference implementation plus conformance (data-plane axes
require two). `web-portal-py` is the single required reference.

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/missing/infrastructure
  failures; no domain-outcome fields are needed for this axis (the only branching outcome is
  `NOT_FOUND` on a missing component, which is a genuine caller error, not normal control
  flow).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the plugin implements a plain gRPC `UiService` server.

## Writing a plugin

1. Implement `UiService` (`Describe` / `RenderSlot`) for your surface (web portal, CLI,
   Slack bot, VS Code extension, etc.).
2. In `Describe`, return all slot URIs your surface can host. Use capability-shaped URIs
   (`rat://ui/v1/<slot-name>`) so contributors can reference them in `contributes.slots`.
3. In `RenderSlot`, look up `(slot, component)` in your contribution registry (populated from
   peer plugins' manifests at boot). Return a non-empty `asset_ref` and a valid JSON Schema
   in `props_schema`. Unknown pair → `NOT_FOUND`; empty inputs → `INVALID_ARGUMENT`.
4. **Do not** scope `Describe`/`RenderSlot` to a tenant — these are surface metadata. Tenant
   context is relevant only in the UI's downstream calls to the API gateway as a client.
5. Pass [`ui-v1.json`](../../../../conformance/ui-v1.json) via `make conformance`.

## Reference implementations

| ref | demonstrates |
|---|---|
| [`plugins/ui/web-portal-py`](../../../../../plugins/ui/web-portal-py) | experience-axis reference; slot hosting + component resolution; `NOT_FOUND` on unknown component; no tenant scoping |

## Related

[`ui.proto`](ui.proto) · [`ui-v1.json`](../../../../conformance/ui-v1.json) ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (experience axis: one ref + conformance) ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md)
