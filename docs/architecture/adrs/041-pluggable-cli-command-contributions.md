# ADR-041: A pluggable CLI — plugins contribute commands the `rat` client surfaces

## Status: Accepted (2026-06-09)

## Context

The clean-room rebuild is heading toward a remote dev flow ([[remote-dev-flow]]): data branching,
preview/query on a branch, across many environments. The obvious way to expose those is new CLI
verbs — `rat branch`, `rat checkout`, `rat query`, `rat merge`. But baking them into the `rat`
binary means **the CLI grows a verb every time a plugin ships a capability**, and the binary ends
up knowing about catalogs, engines, schedulers — exactly the coupling the six-thing-core discipline
([plugin-architecture.md](../../../.claude/rules/plugin-architecture.md)) exists to prevent.

There is already a seam for the right answer. ADR-025's `ui` axis treats the CLI as a *surface*:
`rat ui --surface cli` ([core/client/ui.go](../../../core/client/ui.go)) reads command
contributions plugins publish to the state-backend and invokes their capability through the gateway
— and since it is a plain gateway client (`--addr`), **it already works against a remote daemon.**
What's missing is the ergonomics to make it a real *adapter*: (1) it fires a *fixed* args blob, so
a user can't pass `my-exp` to `branch create`; (2) commands are reached via `rat ui run <id>`, not
as first-class `rat <command>`; (3) commands are only discoverable if a plugin publishes them at
runtime; (4) there is no remote *context* so the target/identity is re-typed each call.

## Decision

**The `rat` CLI is a thin, generic dispatcher; commands are plugin contributions it surfaces. A
plugin declares commands in its manifest; the daemon publishes them to a discovery index in state;
the client maps CLI args to the capability's request and invokes it through the connected (possibly
remote) gateway.**

### 1. Plugins declare commands — `contributes.commands` (additive manifest field)

```yaml
contributes:
  commands:
    - name: "branch create"                    # → rat branch create <name>
      capability: rat://catalog/v1/create-branch
      help: "Create a data branch off main"
      args:
        - { name: name, field: branch,      positional: true, required: true }
        - { name: from, field: from_branch, flag: true, default: main }
```

Additive + backward-compatible within `rat/1` (manifest-only, like ADR-040's `ports`). Static, so
`rat plugin check` validates it: the `capability` must be real, each `field` must exist on the
capability's request message, and a command `name` may **not** collide with a built-in verb.

### 2. The daemon publishes a discovery index — no proto change

Each plugin's `contributes.commands` is written to state at `cli/commands/<plugin>/<name>`, making
commands discoverable by any client through the existing **state gateway** — so it works
transparently through `rat hub` to a remote workspace, with **no change to any frozen proto**. Two
ways to populate it, same key space: **(v1)** the plugin publishes its own commands on boot via the
SDK (reusing the established `ui` contribution channel, ADR-025 — what `rat ui --surface cli`
already reads); **(target)** the daemon writes them from the manifest on registration (no per-plugin
publish code; the registry making its own contents discoverable via the state gateway it owns). The
v1 reuse is what the first prototype ships; the daemon-bridge is the productionization. Either way
the client reads the same `cli/commands/*` index — not a new core responsibility (see six-thing
analysis below).

### 3. The client dispatches — arg-mapping grammar

`rat <tokens…>`: the client checks built-in verbs first; on no match it loads the contributed-command
table from the connected context's gateway, longest-prefix-matches the command `name`, maps the
remaining CLI tokens to the request message, invokes the capability (C5 + audit), and renders the
response. Mapping:
- **positionals** bind in declared order; **flags** (`--from main`) bind by name; each targets a
  proto `field` (the request is built as protojson, then marshalled). `rat call --data '{…}'` stays
  the low-level escape hatch.
- **Collisions:** built-ins always win; a contributed command that shadows a built-in is rejected at
  `rat plugin check`. Contributed commands are grouped under a `PLUGIN COMMANDS (from <workspace>)`
  section in `rat help`, so the CLI self-describes the connected platform.

### 4. Remote by construction

Dispatch uses the current **`rat context`** (addr/token/workspace — the thin-client companion piece):
the command table, and every invocation, target whatever gateway the context points at. Connect to a
different workspace and `rat` surfaces *that* deployment's commands. The CLI **adapts to the remote**.

## Six-thing-core analysis (temptation ledger)

**Not a 7th core responsibility — the count stays six.** The dispatcher is **client-side** (the
`rat` binary's client face, an experience surface — the `ui` axis). The command *registry* is the
existing registry; the discovery *index* is published through the existing **state gateway**; the
*invocation* is the existing **API gateway** doing its generic-relay job (ADR-005). A plugin
contributing a command is data, exactly like a `provides` entry. The only new daemon behavior — the
registry writing a discovery index to state on register — is the registry + state gateway composing
the two things they already do, the same shape as ADR-027's live control. Logged in the ledger as
*examined, not a temptation.*

## Consequences

**Good:**
- The core CLI never grows a verb for a plugin again. `rat branch`/`query`/`merge` come from the
  catalog/engine plugins; the binary stays minimal.
- The CLI **is the remote platform's surface** — connect somewhere, see its commands. Different
  environments expose different commands with zero client changes.
- Authors get ergonomic commands for free by declaring them in the manifest — no per-plugin CLI code.

**Costs / residual:**
- Discovery is one `state/list` + N `state/get` per invocation (cheap; cacheable later). A plugin's
  commands are unavailable until the daemon is up and has indexed them — acceptable (the commands
  *operate* the platform, so the platform must be up anyway).
- The arg-mapping grammar covers scalar positionals/flags → fields; nested/repeated fields fall back
  to `rat call --data`. Richer mapping (nested, enums, file inputs) is a follow-on.
- A contributed command can't (yet) stream; server-streaming capabilities are surfaced as
  `rat call`-style until the dispatcher learns the stream variants.

## Related
- [ADR-025](025-surfaces-and-consumers.md) — the `ui` surfaces + consumers (the `cli` surface this builds on).
- [ADR-024](024-ui-assembled-from-plugin-contributions.md) — UI assembled from plugin contributions.
- [ADR-040](040-published-ports-for-ui-plugins.md) — the additive-manifest-field pattern reused here.
- [ADR-005](005-capability-invocation-model.md) — the generic gateway relay invocations ride.
- [[remote-dev-flow]] — the laptop→remote dev flow this CLI adapter serves.
