# Plugin architecture — the founding invariant

> This rule has **no `paths:`** on purpose. It is the constitutional invariant for RAT v3 and applies project-wide, every session. Source of truth: [ADR-001](../../docs/architecture/adrs/001-everything-is-a-plugin.md). Reinforced by [ADR-002](../../docs/architecture/adrs/002-founding-tech-stack.md) and [ADR-003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md).

## The rule

**The core does six things. Everything else is a plugin.** No exceptions without an Accepted ADR explaining why the thing in question *cannot* be a plugin (chicken-and-egg argument required).

## The six irreducible core responsibilities

1. **Registry** — reads plugin manifests, indexes by `(kind, name, version)`, answers capability lookups.
2. **Identity gateway** — every request carries identity; validation delegates to identity plugin.
3. **State gateway** — `Get/Put/Watch/List` interface; implementation is a state-backend plugin.
4. **Event bus** — async pub/sub (NATS JetStream default); transport pluggable.
5. **Reconciler loop** — reads desired state, drives convergence (K8s controller pattern).
6. **API gateway** — single entry point (gRPC + REST); routes via identity gateway.

These six are **always** in the core, **never** plugins, and the count **stays at six** unless a future ADR proves otherwise.

## The plugin axes (16+, open-set)

**Data plane (7):** engine, runtime, format, strategy, catalog, storage, deployment-runtime.

**Control plane (7):** state-backend, secret-backend, scheduler-backend, identity, tenancy, billing, observability, audit-log.

**Experience (3):** ui, notifications, marketplace.

Community may add more `kind:` values without core changes. The set above is the v1 starting point.

## The hidden "tier 0" — name it, don't pretend it doesn't exist

The 5-perspective adversarial review surfaced that **state-backend + deployment-runtime + the embedded bus** are bootstrap-critical plugins selected at boot, not hot-swappable like the rest. Treat them as a documented "tier 0" — they're still plugins (different impls possible), but they're load-bearing for the core to start, so they get extra rigor and aren't marketed as "swap at runtime." See [reviews/01-adversarial-architect.md](../../reviews/01-adversarial-architect.md) Finding 6.

## Discipline rules

### When tempted to add a 7th core thing

You MUST first write an ADR proving the thing **cannot** be a plugin. The proof requires showing chicken-and-egg (the thing needs to exist before any plugin can load). Most attempted proofs fail this test — most "core" temptations are plugins in denial.

Track the temptation count in `roadmap/done.md`. Three temptations in a quarter = an early warning that the architectural premise needs revisiting.

### When designing a plugin

- It must declare `kind:` matching an existing axis (or propose a new axis via ADR).
- It must declare `provides:`, `requires:` (with capabilities, not concrete plugin names), `suggests:` (informational), `contributes:` (slots).
- It must NOT couple to specific peer plugins by name. Capability negotiation is the only mechanism.
- It must NOT bypass the registry to discover peers.
- It must NOT bypass the event bus to coordinate state changes.
- It must NOT write to other plugins' state-gateway namespaces.

### When designing the data-plane contracts

[ADR-003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) is binding: no data-plane contract may freeze without **two independent reference implementations** that pass conformance and run against each other on golden data. The 6 critical axes (engine, runtime, format, catalog, storage, state-backend) get this rigor. Control-plane axes get one reference + conformance.

### Cross-plugin concerns

The synthesis found one pattern repeatedly: *"everything is a plugin"* has a blind spot for genuinely cross-cutting concerns (trust, observability, conformance, distribution). These belong in the **core's enforcement layer**, not in a single plugin. Specifically:

- Trace context propagation (`traceparent` mandatory in every RPC + event)
- Plugin-to-core authentication (per-plugin token + mTLS-ready)
- State-gateway per-plugin isolation (deny-by-default cross-plugin)
- Capability enforcement at runtime (declared = enforced)
- Mandatory audit emission (even when no audit-log plugin installed)
- Native observability (`/metrics` + OTel spans independent of any observability plugin)

These are properties the existing six things must enforce. They are not new core responsibilities — they are the *correctness conditions* of the existing ones. See [reviews/00-synthesis.md](../../reviews/00-synthesis.md) Critical findings C1-C10.

## What this rule overrides

Convenience. Speed. "Just this once." If the simple thing is "put it in core," the simple thing is wrong. If the simple thing is "couple two plugins directly," the simple thing is wrong. The architecture's value is precisely in refusing those shortcuts.

## When this rule blocks you

That's the point. If you're stuck because the rule won't let you do the easy thing, two options:

1. **Find the plugin shape.** Most "I have to put this in core" instincts dissolve when you actually try to articulate the chicken-and-egg proof. Try writing the ADR; the writing forces clarity.
2. **Propose an exception via ADR.** If the proof of "this cannot be a plugin" is genuine, write the ADR, get it reviewed, and update this rule + ADR-001 to reflect the new shape.

There is no third option.

## Related

- [ADR-001](../../docs/architecture/adrs/001-everything-is-a-plugin.md) — the founding decision this rule enforces.
- [ADR-003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) — the C9 forcing function for data-plane contracts.
- [reviews/00-synthesis.md](../../reviews/00-synthesis.md) — the 5-perspective adversarial review that shaped the cross-cutting concerns list.
- [docs/vision.md](../../docs/vision.md) — the broader thesis + anti-goals.
