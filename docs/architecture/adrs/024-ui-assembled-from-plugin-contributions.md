# ADR-024: The UI is assembled from plugin contributions, not hardcoded

## Status: Accepted (2026-06-03) — built + proven (see roadmap/done.md)

## Context

The platform UI (`platform/bff.py` + a VS Code client) currently hardcodes what it shows:
run history, a trigger button, lake tables. That doesn't scale — every new capability means
editing the UI. The founding thesis is *everything is a plugin*, and [ADR-001](001-everything-is-a-plugin.md)
explicitly cited **VSCode's `contributes` model** as the analog for `provides`. The frozen
manifest schema already carries it:

```json
"contributes": { "slots": [ { "target": "<capability-shaped slot URI>", "component": "<name>" } ] }
```

So a plugin can *already declare* "I contribute component X into UI slot Y." What's missing is
the **runtime mechanism**: how the UI discovers those contributions and renders them, so that
adding a plugin adds UI **without touching the UI**. This ADR defines that mechanism. It does
NOT change the frozen contract — `contributes.slots` is the declarative binding; this adds the
runtime layer beneath it.

The constraint: the VS Code shell can't be unit-tested headlessly, but the **discovery +
aggregation** (the load-bearing, scalable part) can — so the mechanism lives in the bff and is
provable; the shell is a thin generic renderer over it.

## Decision

**The UI is a generic shell that renders whatever plugins contribute. Contributions are
discovered at runtime and served by the bff; the shell hardcodes no platform-specific view.**

### 1. Slots: the UI is a set of named slots a contribution targets

The shell exposes a small, stable set of **slots** (capability-shaped, per the manifest's
`target`):

| slot | what it renders | a contribution supplies |
|---|---|---|
| `rat://ui/v1/explorer` | tree/table views | a `data` endpoint (bff route) returning rows/items |
| `rat://ui/v1/command` | invokable actions (palette / buttons) | a `capability` to invoke (+ default `args`) |
| `rat://ui/v1/config` | config forms | a JSON `schema` + get/set `capability` |

New slot kinds are additive (the shell ignores slots it doesn't know).

### 2. A contribution is a manifest binding + a runtime component spec

- **Declarative binding (frozen manifest):** `contributes.slots: [{target, component}]` — *which
  slot, which component name*. This is the plugin's intent, visible at install time.
- **Runtime component spec:** the rich definition (title, icon, the `data` endpoint / `capability`
  / `schema`) is published to the **state-backend** under `ui/components/<plugin>/<component>`
  (a JSON value). A plugin self-publishes its specs at boot via `state/v1/put` — no core change,
  no new axis; the state-backend is the contribution registry, exactly as it is the project store
  (ADR-021). *(Target evolution: a registry-introspection capability so the bff reads `contributes`
  + specs straight from manifests; the state-publish is the v1 that needs no core surface.)*

### 3. The bff aggregates; the shell renders

- **bff `GET /api/ui`** does `state/v1/list ui/components/` + get-each, and returns a descriptor
  grouped by slot: `{ "slots": { "explorer": [...], "command": [...], "config": [...] } }`. The
  bff hardcodes no view — it aggregates contributions. (It MAY seed the platform's own defaults
  into `ui/components/platform-bff/*` on boot, as bootstrap data, not as hardcoded rendering.)
- **bff `POST /api/invoke`** `{capability, data}` — the generic action path a contributed command
  fires, routed through the gateway (C5-authorized + audited). One endpoint serves every command.
- **The shell** fetches `/api/ui`, renders each slot generically (explorer→tree, command→palette,
  config→form), and drives actions through `/api/invoke`. Adding a contributing plugin changes
  the rendered UI with **zero shell or bff edits**.

## Consequences

**Positive.**
- **Scalable by construction** — a new plugin's UI appears by publishing a contribution; the shell
  and bff never change. This is the `contributes` model the manifest already promised.
- **No new contract** — uses the frozen `contributes.slots` + existing `state/v1` capabilities.
- **Actions stay governed** — contributed commands route through the gateway (C5 + audit) like any
  call; the UI is not a privileged backdoor.
- **Honest about the unprovable bit** — the discovery/aggregation is testable headlessly; only the
  pixel-rendering needs VS Code, and it's a thin generic layer.

**Negative — accepted.**
- **Two-part contribution** (manifest binding + state-published spec) until the registry-introspection
  capability lands — a plugin must both declare the slot AND publish the spec. Mitigation: a small SDK
  helper (`contribute_ui`) does the publish in one call.
- **Trust** — a contributed command names a capability; the *caller's* `requires` still gates it (C5),
  so a contribution can't escalate. But a malicious plugin could publish a misleading component; the
  v1 platform is single-operator, so this is noted, not solved (ties to the marketplace-trust idea).
- **State as the registry** is a convention, not enforced — a stale `ui/components/*` lingers if a
  plugin vanishes without cleanup. Prune-on-list mitigates; a TTL/owner-check is a follow-on.

**Neutral.**
- The bff grows a data leg (`/api/ui`, `/api/invoke`) atop the existing run-history/trigger routes.

## Alternatives considered

1. **Hardcode views in the shell (status quo).** Rejected: every capability edits the UI — the
   opposite of the plugin thesis.
2. **Registry-introspection capability now** (core lists plugins + their `contributes`/specs).
   The clean target, but a new core/proto surface against the sealed `rat/2.0`. Deferred: the
   state-publish mechanism proves the model with no core change; promote to introspection later.
3. **Richer `contributes` in the manifest** (data endpoints, schemas inline). Rejected: the manifest
   is frozen (`additionalProperties:false`); the rich spec belongs at runtime (state), not baked
   into the install-time manifest.
4. **A bespoke UI plugin per feature.** Rejected: that's N UIs, not one extensible shell.

## Related

- [ADR-001](001-everything-is-a-plugin.md) — cites VSCode `contributes` as the model for `provides`.
- [ADR-021](021-orchestrator-pipelines-as-code.md) — the state-backend as a registry (project store);
  here it is the contribution registry too.
- [ADR-022](022-plugins-are-launched-not-composed.md) — adding a plugin is one declaration; this makes
  *its UI* part of that one declaration.
- `contracts/schema/plugin.v1.json` — the frozen `contributes.slots` this implements.
- ideas/inbox 2026-06-02 *(vscode-rat multi-connection)*; the marketplace/trust ideas (contribution trust).
