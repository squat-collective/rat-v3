# Backlog — queued but not started

Work that's been identified (from reviews, ideas, conversations) but isn't actively in flight. **This isn't a wish list** — every entry should be specific enough that the next Claude session knows what "starting" it means.

When an item moves to active work, promote it: cut it from here, add it to [current.md](current.md), update [phases.md](phases.md) status if applicable.

---

## ADRs to write (from synthesis — 23 of 26 not yet written)

Numbered as proposed in [reviews/00-synthesis.md](../reviews/00-synthesis.md). Most are Phase 0 wire-breaking concerns that land *during* Phase 0 as the contracts get drafted, NOT before. They're listed here so they're not lost.

### Phase 0-blocking (must land during Phase 0)

| Prospective ID | Title | Addresses |
|---|---|---|
| ADR-004 | Wire contract: trace context + correlation IDs | C1 |
| ADR-005 | Plugin-to-core authentication | C2 (Q13) |
| ADR-006 | State-gateway isolation + per-plugin namespacing | C3 |
| ADR-007 | Resource asks / limits in manifest contract | C4 |
| ADR-008 | Capability enforcement at runtime | C5 |
| ADR-009 | Conformance suite obligations per axis | C6 |
| ADR-010 | Tenancy as structural isolation | C7 |
| ADR-013 | API gateway hardening + listener split | C10 |

### Pre-GA (land during Phase 1-4)

| Prospective ID | Title | Addresses |
|---|---|---|
| ADR-011 | Plugin supply-chain trust (Sigstore + manifest signing) | C8 |
| ADR-014 | Core-native observability + SLOs | I1 |
| ADR-015 | Upgrade safety: version skew + preflight + reversible migrations | I2 |
| ADR-016 | Backup/restore + GitOps desired-state | I3 |
| ADR-017 | Plugin scaffolding + local dev loop | I4, I5 |
| ADR-018 | Event-bus authn/authz | I6 |
| ADR-019 | Desired-state RBAC + admission control | I7 |
| ADR-020 | Mandatory audit trail | I8 |
| ADR-021 | Deployment-runtime minimum isolation profile | I9 |
| ADR-022 | Reconciler robustness (backoff, jitter, fairness) | I10 |
| ADR-023 | Plugin publish + signing pipeline | I11 |
| ADR-024 | Plugin deprecation + compatibility governance | I12 |
| ADR-025 | Secret-handling contract | I13 |
| ADR-027 | Incumbent-stack migration path strategy | I16 |
| ADR-028 | First-five-minutes wow + front-door demo | I17 |

### Now (land before Phase 0 starts if real product launch is coming)

| Prospective ID | Title | Addresses |
|---|---|---|
| ADR-026 | GTM positioning + message canon | I15 |

---

## Open questions parked in ideas/inbox.md (synthesis Q11-Q15)

These came up during ADR-002's locking session and are tracked in [ideas/inbox.md](../ideas/inbox.md). They surface as ADRs when relevant:

- **Q11** — Should v2's ADRs 025/026 still be implemented on v2 (D7 implication)?
- **Q12** — Exact default solo bundle composition (becomes ADR-003-bundle when Phase 0 produces references)
- **Q13** — Plugin auth model (resolved by ADR-005 above)
- **Q14** — Marketplace plugin UX shape (becomes a Phase 5 ADR)
- **Q15** — Plugin sandboxing model (resolved by ADR-011 + ADR-021)

---

## Engineering work queued (will activate when phases begin)

### Phase 0 work items
- Draft `plugin/v1.json` JSON Schema (sub-phase 0a)
- Draft 20 axis proto files (sub-phase 0b)
- Draft cross-cutting protos: `common/v1/context.proto`, `common/v1/descriptors.proto` (sub-phase 0c)
- Build 12 reference implementations (sub-phases 0d + 0e)
- Build `rat-conformance` test harness (sub-phase 0f)
- Write 20 `CONTRACT.md` author-facing docs (sub-phase 0g)
- Recruit external peer reviewers (OSGi / K8s / VSCode contributors)
- Per-RPC latency benchmark (sub-phase 0f)

### Phase 1 work items
- Implement the 6 core things in Go (~12-15k LOC)
- Mock plugin set for integration tests
- The 11 weekly milestones from ADR-001 Phase 1 description

### Phase 2 work items
- Promote 12 reference plugins to production grade
- Build `rat plugin new` scaffolding
- Build `rat dev --plugin` local dev loop
- Port v2's `demo-loader` as the front-door wow

### Phase 4 work items
- All hardening items from Phase 4 section of [phases.md](phases.md)
- All GTM items (4 of 5 non-engineering)

---

## Claude Code config: deferred until first code file

These two additions to `.claude/` were audited and recommended but deliberately not landed yet — they need real code to exist first so patterns can crystallize rather than being speculative.

**Trigger:** land both when the first `.go`, `.rs`, or `.proto` file is committed.

| Item | What | Why deferred |
|---|---|---|
| `PostToolUse` auto-format hook | `PostToolUse` + `Edit\|Write` matcher in `settings.json`. Runs `gofmt -w` on `.go` files, `cargo fmt` on `.rs` files, `buf format -w` on `.proto` files after every edit. Prevents the model from forgetting to format after edits — formatting failures in Go/Rust block CI. | No Go/Rust/proto files exist yet. The hook targets specific file extensions; adding it now would be a no-op with stale intent. |
| Path-scoped proto/manifest rule | A new `.claude/rules/proto-contracts.md` with `paths: ["**/*.proto", "**/plugin.yaml", "**/plugin/v1.json"]` capturing proto-authoring conventions: field naming, message nesting, service naming for Go/Rust gRPC, `buf.yaml` layout, capability URI format per ADR-002 D4. | The always-load `plugin-architecture.md` already captures architectural invariants. The path-scoped rule earns its place only once real proto files reveal tool-specific style patterns that don't belong in the always-load rule. Write after the first proto pass (sub-phase 0b). |

**How to land when ready:**
1. Spawn `claude-engineer` agent with "add the deferred PostToolUse format hook from backlog."
2. For the proto rule: draft the rule content from actual proto authoring experience first, then add the `paths:`-frontmatter file.
3. Update this backlog entry → cut it + append to `done.md`.

Source: `claude-engineer` audit 2026-05-30. Doc citations: `https://code.claude.com/docs/en/hooks-guide.md` (PostToolUse pattern), `https://code.claude.com/docs/en/sub-agents.md` (rules path-scoping).

---

## GTM / non-engineering work (deferred until commitment)

These need the project commitment decision before they make sense to start:

- Reposition project message (anti-lock-in / cost-ownership canon)
- Hand-pick 3-5 design partners (year 1)
- Build a public reproducible benchmark (the Polars pattern)
- Plan a content/distribution motion (founder-led)
- Design dbt→RAT and Airflow→RAT migration UX
- Commercial-path planning (managed cloud later)

---

## Ideas that may or may not become work

In [ideas/inbox.md](../ideas/inbox.md) — naming, plugin distribution patterns, manifest-from-proto generation, etc. Promote to backlog if they sharpen into specific work.

---

## Maintenance

When new work is identified during a working session:

1. Capture it here (this file) with enough specificity to be actionable.
2. Note where it came from (review, idea, conversation, ADR).
3. Don't worry about ordering — that's done at promotion time (when item moves to [current.md](current.md)).

When an item starts:

1. Cut it from this file.
2. Add to [current.md](current.md) with the immediate next concrete step.
3. Update [phases.md](phases.md) status if it changes a phase boundary.

When an item is dropped (decided against, superseded):

1. Cut it from this file.
2. Note the decision in the relevant ADR or `ideas/inbox.md` archive.
3. Don't leave dead entries here.
