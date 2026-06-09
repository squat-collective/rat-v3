# ADR-039: Driver plugins (empty `provides`) and the authoring validation gate

## Status: Accepted (2026-06-09)

## Context

The [clean-room rebuild](../../../CLEAN-ROOM.md) surfaced two related developer-experience gaps
([docs/guides/GAPS.md](../../guides/GAPS.md) #3, #8b):

1. **Validation drift.** `rat plugin check`/`pack` did only the structural manifest checks
   (`manifest.Load`) — kind known, name/version present, capability URIs real, axis coherence —
   but **not** the frozen [`plugin.v1.json`](../../../contracts/schema/plugin.v1.json) constraints.
   So a manifest missing the **C4-required `resources`** block passed `check` and `pack`, yet would
   fail the published schema (`make validate-manifests`). The CLI and the contract disagreed.

2. **The driver shape.** Two real clean-room plugins —
   [`scheduler/interval-py`](../../../plugins/scheduler/interval-py/) (fires a pipeline on a timer)
   and [`ui/web-bff-py`](../../../plugins/ui/web-bff-py/) (a web BFF) — **provide no capability**.
   They are *drivers*: they `require` capabilities and drive them, exposing no `rat://` service of
   their own. But `plugin.v1.json` set `provides` `minItems: 1`, so a driver was schema-invalid —
   while `rat plugin init --kind scheduler-backend` *scaffolded* `provides: []` anyway. The
   scaffold, the CLI, and the schema all disagreed about whether a driver was legal.

These are the same root cause: there was no single, agreed answer to "what makes a manifest valid
to author?", so the scaffold, the CLI gate, and the frozen schema diverged.

## Decision

**Bless the driver shape, and make the CLI authoring gate enforce the frozen schema's load-bearing
constraints.**

1. **A plugin must DO SOMETHING.** The authoring rule is: **declare ≥1 `provides` (a provider) OR
   ≥1 `requires` (a driver).** A manifest with neither is rejected by `rat plugin check`/`pack`.

2. **Relax the envelope `provides` to `minItems: 0`.** A driver legitimately provides nothing. This
   is a *permissive, backward-compatible* change within `rat/1` (every previously-valid manifest
   stays valid; some previously-invalid driver manifests become valid) — so it does not break the
   frozen wire. The `provides` description now documents the driver shape.

3. **The CLI gate (`rat plugin check`/`pack`) enforces the envelope constraints it used to skip** —
   via a new `validateAuthored`: the "provides-or-requires" floor (point 1) and the mandatory C4
   `resources.requests` block (Gap 3). The scaffold now emits a default `resources` block, so an
   authored plugin always carries one.

4. **Per-kind schemas remain the PROVIDER contract.** `contracts/schema/kinds/<kind>.v1.json` still
   mandates a provider's minimal-mandatory-core `provides` (e.g. a `state-backend` provides
   get/put; a `scheduler-backend` *provider* provides schedule/watch-due). Those constraints apply
   to plugins that **provide** that axis. A **driver** is validated by the envelope + the authoring
   gate, not by the provider per-kind schema. `kind` denotes the axis a plugin participates in; a
   driver participates by *consuming* it.

## Consequences

**Good:**
- The scaffold, the CLI gate, and the envelope schema now agree on what a valid manifest is.
- Drivers (schedulers, UIs, operators) are a first-class, documented shape — not an accident the
  CLI tolerated.
- `resources` (C4) is actually enforced at authoring time, where the cost belongs.

**Costs / residual:**
- The CLI gate enforces the *load-bearing* envelope constraints (provides-or-requires, resources),
  not the full JSON-Schema (regex patterns on version/quantities, etc.). Exhaustive per-kind
  JSON-Schema validation still lives in `scripts/validate-manifests.py` (`make validate-manifests`).
  Wiring the embedded frozen schemas into the Go CLI is a worthwhile follow-on, not done here.
- A driver that declares a *provider* `kind` (e.g. `interval-py` is `kind: scheduler-backend`) would
  not satisfy that kind's per-kind PROVIDER schema if run through `validate-manifests.py`. That
  validator only scans `contracts/examples/`, not `plugins/`, so there is no live conflict — but the
  tension is real and noted. A future `kind: driver`/`operator` (the envelope already permits any
  `^[a-z…]$` kind without a per-kind schema) would resolve it cleanly; deferred to avoid churn.

## Related
- [GAPS.md](../../guides/GAPS.md) #3, #8b — the gaps this resolves.
- [ADR-011](011-manifest-schema-freeze-and-per-kind-layer.md) — the envelope + per-kind schema layering.
- [plugin.v1.json](../../../contracts/schema/plugin.v1.json) — the relaxed `provides` minItems.
