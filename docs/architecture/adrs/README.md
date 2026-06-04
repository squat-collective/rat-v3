# ADRs — RAT v3

Architecture Decision Records. One ADR per concept; no multi-topic ADRs. Numbered sequentially; status moves Proposed → Accepted (or Rejected / Superseded).

## Index

| # | Title | Status | Date |
|---|---|---|---|
| [001](001-everything-is-a-plugin.md) | Everything is a plugin | Accepted | 2026-05-30 |
| [002](002-founding-tech-stack.md) | Founding tech stack + strategy decisions | Accepted | 2026-05-30 |
| [003](003-two-references-before-contract-freeze.md) | Two independent reference implementations before any contract freezes | Accepted | 2026-05-30 |
| [004](004-core-language-go.md) | Core language locked — Go | Accepted | 2026-05-30 |
| [005](005-capability-invocation-model.md) | Capability invocation model — core-mediated control plane | Accepted | 2026-05-30 |
| [006](006-sdk-distribution-and-plugin-layout.md) | SDK distribution, reference-plugin layout, and codegen toolchain | Accepted | 2026-05-31 |
| [007](007-call-context-transport.md) | Call-context transport — cross-cutting context rides in transport metadata, not the payload | Accepted | 2026-05-31 |
| [008](008-streaming-capability-invocation.md) | Streaming capability invocation — per-cardinality Invoke variants, enforce-at-open | Accepted | 2026-05-31 |
| [009](009-data-plane-contract-freeze-v1.md) | Freeze the data-plane axis contracts at `v1` (`rat/1`) | Accepted | 2026-05-31 |
| [010](010-catalog-commit-linkage.md) | Catalog commit-linkage — additive `RegisterTable` + `CommitTable` RPCs | Accepted | 2026-06-01 |
| [011](011-manifest-schema-freeze-and-per-kind-layer.md) | Freeze the plugin manifest schema at `v1` + add the per-kind schema layer | Accepted | 2026-06-01 |
| [012](012-crash-safety-additive-fields.md) | Additive crash-safety fields for the data-plane write path (`rat/1.5`) | Accepted | 2026-06-01 |
| [013](013-phase-1-spike-and-commitment-gate.md) | Phase 1 entry — time-boxed contract-de-risking spike + commitment-gate posture | Accepted | 2026-06-01 |
| [014](014-spike-core-registry-and-invoke-gateway.md) | The spike core — a manifest-driven registry + capability-invoke gateway (C5 made real) | Accepted | 2026-06-01 |
| [015](015-phase-1-commitment-gate-cleared.md) | Phase-1 commitment gate CLEARED — commit to the full core build | Accepted | 2026-06-01 |
| [016](016-plugin-provisioning-via-deployment-runtime.md) | Plugin provisioning via the deployment-runtime axis — the core launches, it doesn't dial (D1) | Accepted | 2026-06-01 |
| [017](017-pre-unfreeze-contract-amendment-gate.md) | Pre-unfreeze contract-amendment gate (the Q02 dry-run punch-list) | Proposed | 2026-06-02 |
| [018](018-connectionless-codegen-local-plugins.md) | Connectionless codegen via local plugins | Accepted | 2026-06-02 |
| [019](019-rat-serve-daemon.md) | `rat serve` — the runnable core daemon + the beginner compose stack | Accepted | 2026-06-02 |
| [020](020-data-platform-bundle.md) | The data platform bundle — `platform/`, medallion conventions, VS Code + CLI (Phase 2) | Accepted | 2026-06-02 |
| [021](021-orchestrator-pipelines-as-code.md) | rat is a pure orchestrator — pipelines as code (dbt), the pipeline-runner axis, infra declares only plugins | Proposed | 2026-06-02 |
| [022](022-plugins-are-launched-not-composed.md) | Plugins are launched, not composed — adding a plugin is one declaration (socket-mount local, k8s prod) | Proposed | 2026-06-02 |
| [023](023-rat-as-a-per-project-daemon.md) | rat is a per-project daemon — poetry-style hybrid control over an external spec, isolated per instance | Proposed | 2026-06-03 |
| [024](024-ui-assembled-from-plugin-contributions.md) | The UI is assembled from plugin contributions, not hardcoded (the VSCode `contributes` model) | Proposed | 2026-06-03 |
| [025](025-surfaces-and-consumers.md) | Surfaces & consumers — plugins contribute per-surface interfaces; consumers are out-of-stack renderers | Proposed | 2026-06-03 |
| [026](026-plugin-authoring-and-packaging.md) | Plugin authoring & packaging — the `rat plugin` toolkit, the verified-plugin gate, scaffolded CI/CD | Proposed | 2026-06-03 |
| [027](027-live-plugin-control-rpc.md) | Live plugin control — the daemon's `ControlService` (Register/Deregister/List); live `rat add`/`remove` | Accepted | 2026-06-03 |
| [029](029-plugin-runtime-sdk.md) | The plugin runtime SDK — `ratplugin` (Serve · Call · CallerTenant) | Accepted | 2026-06-04 |
| [031](031-durable-local-storage.md) | Durable local storage — the per-project `/data` mount (configurable volumes later) | Accepted | 2026-06-04 |

## Template

```markdown
# ADR-NNN: Short title

## Status: Proposed | Accepted | Rejected | Superseded by ADR-XYZ (date)

## Context

What forces are in play? What's the problem? What did we learn from the v2 ADRs (or other prior art) that's relevant here?

## Decision

What we decided. Be specific. Show schema / protocol / code shape where it helps.

If the decision has sub-parts, use level-3 headings:
### 1. Sub-decision A
### 2. Sub-decision B

## Consequences

**Positive.** What we gain.

**Negative — accepted.** What it costs, listed honestly.

**Neutral.** What's different but value-neutral.

## Open questions

Things deferred to future ADRs. Number them Q01, Q02, etc. so they're trackable.

## Alternatives considered

Each option we looked at + why we rejected it. Future-us needs the rejection rationale, not just the chosen design.

## Migration

How we get from current state to this decision. Phase-by-phase if non-trivial. If "this is the design from day 1," say so.

## Related

- Other ADRs (in this repo or v2)
- Proto files, schema docs
- Prior-art references in `research/prior-art/`
- Conversations that shaped this decision
```

## Discipline

- **ADR-first for architectural decisions.** If the change affects a contract, a plugin axis, or the core's shape — write an ADR before code.
- **One ADR per commit.** ADRs land cleanly; no commit mixes an ADR with implementation.
- **ADRs are immutable once Accepted.** Edit only typos. If the decision changes, write a new ADR that supersedes the old one and update the old one's Status.
- **Reject is a valid status.** ADRs we considered and rejected stay in the index — that's the record of "we already thought about this."
- **Cross-link aggressively.** Cite other ADRs, v2 ADRs, prior art, conversations. The web of references is the institutional memory.

## Numbering

- ADR numbers are zero-padded to 3 digits (`001`, `002`, …).
- Numbers are assigned at PR time, not at draft time, to avoid collisions.
- Drafts use `XXX-title.md` until merged.
