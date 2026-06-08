# ADR-025: Surfaces & consumers — plugins contribute per-surface interfaces; consumers are out-of-stack renderers

## Status: Accepted (2026-06-03) — built + proven (see roadmap/done.md)

## Context

[ADR-024](024-ui-assembled-from-plugin-contributions.md) made the UI *assembled from
contributions* — but with one generic `ui` namespace and an implicit single UI backend (the
bff). Two things it didn't model, surfaced in conversation:

1. **Multiple interface surfaces.** A plugin like dbt should be able to offer a **VS Code**
   interface, a **CLI** interface, and a **webapp** interface — *surface-tailored*
   presentations of the same underlying capabilities — not one generic component set.
2. **Interfaces are consumers, not stack plugins.** The VS Code extension runs **in VS Code**;
   nothing of it runs in the rat daemon. It has no link to the daemon other than *being a
   client*. Yet it must still consume plugin-to-plugin extensions targeted at its surface —
   "dbt for rat-vscode" — without dbt and VS Code knowing about each other.

The constraint that makes this clean: a plugin must NOT branch on "does VS Code exist?" and a
consumer must NOT enumerate plugins. They meet *only* through the contribution registry, scoped
by surface. This ADR names the model and the participant split; it is **design-only** (the
VS Code/CLI/webapp rendering is the part that can't be proven headlessly anyway — ADR-024's
discovery/aggregation already is).

## Decision

**There are two kinds of participants — stack plugins (in the daemon, provide capabilities) and
surface consumers (outside the daemon, render). A stack plugin contributes *per-surface*
interfaces, backed by its capabilities; each surface consumer renders only what targets its
surface; absence of a surface is inert (no checks).** Six parts:

### 1. Two participant categories (name the split)

| | stack plugin | surface consumer |
|---|---|---|
| runs | in the daemon, launched by the reconciler | on its own surface (VS Code, a shell, a browser) |
| relation to gateway | behind it; provides/requires capabilities | a *client* of it; consumes, never serves |
| examples | dbt, state, secret, scheduler | the VS Code extension, the CLI, the webapp |
| in the daemon? | yes | **no** — nothing of it runs there |

A surface consumer is a generic renderer. It has no counterpart plugin in the daemon. "dbt-for-
vscode" is **not** a daemon thing — it is dbt's *vscode-targeted contribution*, rendered by the
generic VS Code consumer.

### 2. Surfaces are slot namespaces (extends ADR-024's `target`)

ADR-024's `contributes.slots.target` is a capability-shaped URI. The **surface** is a segment of
it: `rat://ui/<surface>/<slot>`. A plugin contributes to whichever surfaces it supports:

```yaml
# dbt's manifest
contributes:
  slots:
    - { target: rat://ui/vscode/explorer, component: dbt-models-tree }
    - { target: rat://ui/vscode/webview,  component: dbt-lineage }
    - { target: rat://ui/cli/command,     component: dbt-commands }
    - { target: rat://ui/webapp/panel,    component: dbt-panel }
```

Each surface consumer reads only its namespace (`rat://ui/vscode/*`), so the same dbt presents
three surface-flavored interfaces from one set of capabilities.

### 3. Surface interfaces are declarative + capability-backed — NOT shipped code

A plugin's "specific interface" for a surface is a **bundle of declarative components whose data
and actions are the plugin's capabilities** — never executable code injected into the surface:

- a **view** → data from a plugin capability (`rat://dbt/v1/list-models` → a tree),
- a **command** → invokes a capability (`rat://strategy/v1/apply`),
- a **config** → a JSON schema + a set-capability,
- a **webview** (rich) → HTML/JSON **served by a plugin endpoint**, loaded into a generic chassis.

The consumer stays a generic renderer; the plugin supplies content + actions. This keeps the
trust story sane: a malicious plugin can declare components and name capabilities (still
C5-bounded) — it cannot run arbitrary code in your editor.

### 4. Conditional **by consumption**, never by check

A plugin publishes every surface interface it supports, unconditionally. Whether one is *used*
depends only on which consumers are present:

```
dbt publishes rat://ui/{vscode,cli,webapp}/*   (always)
VS Code running? → its consumer pulls the vscode set → dbt-in-vscode appears
no CLI running?  → the cli contributions sit inert, costing nothing
add a webapp later → dbt's webapp interface lights up, dbt unchanged
```

No `if surface exists` logic lives in any plugin. Absence is free; reality filters.

### 5. How a consumer reaches rat — both shapes allowed, consumer always out-of-daemon

- **Aggregator (recommended default):** a generalized **ui-aggregator stack plugin** (today's bff)
  serves `GET /api/ui?surface=<s>` — the contributions for that surface, grouped by slot — and a
  generic `/api/invoke`. Surface consumers stay thin. The aggregator is a *separate* stack plugin;
  the VS Code extension still runs nothing in the daemon.
- **Direct (option):** a consumer talks straight to the gateway (like `ratctl`) — reads the
  contribution registry + invokes capabilities itself. Fatter client, no aggregator.

Either way the consumer is a client; nothing of it is launched by the reconciler.

### 6. Consumer identity bounds contributed actions (C5 still gates)

A contributed action is a capability call; *something* must be the caller. A surface consumer
carries an **identity** (a connection: endpoint + tenant + token — the parked multi-connection
vscode model). C5 authorizes the action against that identity's `requires`, so a contribution
cannot escalate beyond the consumer's granted authority. (Today the bff is the single caller, so
contributions are bounded by the bff's `requires` — coarse but safe; per-consumer identity is the
refinement.)

## Consequences

**Positive.**
- **Marketplace-shaped extensibility on every surface.** A new plugin gains UI on all present
  surfaces by publishing per-surface contributions; a new *surface* gains every plugin's interface
  by adding a consumer — neither side touches the other.
- **Decoupling with zero conditionals** — "only if vscode exists" needs no code; it's emergent.
- **Safe by construction** — declarative components + capability actions; no plugin code runs in a
  surface; C5 bounds every action.
- **Honest about the daemon boundary** — consumers are clients, not stack plugins; the daemon stays
  the six things.

**Negative — accepted.**
- **Per-surface authoring cost** — a plugin tailors a component set per surface it wants to support
  (vs. one generic set). Mitigation: a default/`generic` surface a plugin can target once, plus the
  `contribute_ui` helper.
- **The rich path needs a content protocol** — a webview's HTML/JSON comes from a plugin-served
  endpoint; that data-leg contract (auth, shape) is unspecified here (a follow-on).
- **Consumer auth model required** — per-consumer identity/connection isn't built; until it is,
  actions are bounded by the shared aggregator's authority (coarse).
- **Surface namespace must be governed** — `rat://ui/<surface>/<slot>` is a convention; surfaces and
  their slot vocabularies need a light registry so consumers and plugins agree.

**Neutral.**
- ADR-024 is **extended, not superseded**: its contribution mechanism stays; this adds the *surface*
  axis to `target` and names the consumer/stack split.

## Open questions

- **Q01 — surface + slot registry.** How a consumer learns its surface's slot vocabulary, and how
  surfaces are registered (a well-known doc? a `rat://ui/<surface>/manifest` contribution?).
- **Q02 — webview/content protocol.** The data-leg contract for a plugin-served rich component
  (auth, paging, the HTML/JSON shape the chassis expects).
- **Q03 — consumer identity.** The connection/token model for a surface consumer (ties to the
  multi-connection vscode idea) so C5 bounds per-consumer, not per-aggregator.
- **Q04 — aggregator topology.** One shared ui-aggregator vs one per surface; whether `cli` even
  needs an aggregator (it may go direct).
- **Q05 — data capabilities for views.** A view's data needs a capability (e.g. `rat://dbt/v1/
  list-models`) or a generic data-leg; do plugins grow bespoke read axes, or is there a generic
  "describe/query" capability?

## Alternatives considered

1. **Single generic `ui` namespace (ADR-024 as-is).** Rejected: can't tailor per surface; a CLI and
   a VS Code tree are different idioms, not one component set.
2. **Plugins ship surface extension *code* (a real VS Code extension per plugin).** Rejected: heavy
   (N extensions to install/update) and a trust hole (arbitrary code in the editor). Declarative +
   plugin-served content covers "rich" without it.
3. **Surface consumers as stack plugins.** Rejected: they run on a surface (editor/browser/terminal),
   not in the daemon; modeling them as launched plugins is a category error and breaks the "consumer
   has no daemon link" property.
4. **Consumers enumerate plugins directly.** Rejected: couples the consumer to the plugin set;
   the contribution registry (scoped by surface) is the decoupled meeting point.

## Related

- [ADR-024](024-ui-assembled-from-plugin-contributions.md) — the contribution mechanism this extends
  with the surface axis + the consumer/stack split.
- [ADR-001](001-everything-is-a-plugin.md) — VSCode `contributes` as the model; the ui/notifications/
  marketplace experience axes.
- [ADR-023](023-rat-as-a-per-project-daemon.md) — clients vs the daemon (rat/ratctl); a surface
  consumer is a client of the same shape.
- ideas/inbox 2026-06-02 *(vscode-rat multi-connection — the consumer-identity/connection model)*;
  the marketplace/distribution + trust ideas (surface contribution discovery + trust).
