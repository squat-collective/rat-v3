# Backlog — queued but not started

Work that's been identified (from reviews, ideas, conversations) but isn't actively in flight. **This isn't a wish list** — every entry should be specific enough that the next Claude session knows what "starting" it means.

When an item moves to active work, promote it: cut it from here, add it to [current.md](current.md), update [phases.md](phases.md) status if applicable.

---

## Additive (NOT freeze-blocking) — roll `(rat.capability)` across remaining axes

Freeze-blocker #5 created `contracts/proto/rat/common/v1/annotations.proto` (the `(rat.common.v1.capability)` method option) and applied it to **format** + **engine**; **storage**, **runtime**, **catalog**, and **state** were added during their 0d work (2026-05-31) — so all 6 DATA-PLANE axes are annotated. Applying it to the remaining control/experience axis services (strategy, identity, tenancy, deployment-runtime, scheduler, secret, observability, audit-log, ui, notifications, marketplace, billing) is **additive** — adding a method option is wire-compatible (`buf breaking` FILE does not flag it), so it does NOT block the `rat/1` freeze. But the C5 gateway + C6 conformance harness need it on every method to function, so it should land before those are built (and each axis needs it before that axis can be 0d-tested through the stub gateway). Per method: add `import "rat/common/v1/annotations.proto";` + `option (rat.common.v1.capability) = "rat://<axis>/v1/<cap>";` inside each rpc (capability URIs already documented in each proto's header comment). Source: reviews/06 I-4 (AUTH-9).

---

## 0d/0e round-2 — a technologically-divergent reference per data-plane axis (real backends)

The current 0d references are **inmemory twins** (`inmemory-go` + `inmemory-py`): two independent *code paths* in two languages that pass one shared golden-vector file. That validates the **wire contract** (proto shapes, RPC cardinalities, error model, the `rat-callmeta-bin` envelope, gateway mediation) and the cross-cutting machinery — and it's where ADR-007 + ADR-008 came from. But it is the **weak form** of ADR-003 "independence": both impls use the same underlying tech (a hashmap), so they cannot surface the **orthogonality-assumption / semantic-divergence** failures ADR-003 targets ("this only worked because both used the same Arrow dialect"; snapshot-isolation vs CAS; serializable vs eventual).

**Round 2 (before any data-plane axis → `v1`):** make one reference per axis a *real divergent backend* with a different consistency/semantic profile. Cheapest + highest-value first:
- **`state` = sqlite — ✅ DONE 2026-05-31** (`examples/state/sqlite-py/`): passes the shared vectors + adds round-2 tests for DURABILITY (survives reopen) and LINEARIZABLE CAS (16 threads → exactly one winner, enforced by `BEGIN IMMEDIATE`, not a mutex). Establishes the round-2 pattern: real backend + same vectors + a backend-specific semantic test.
- **`storage` = local-fs — ✅ DONE 2026-05-31** (`examples/storage/localfs-go/`): passes the shared vectors (now provider-neutral logical prefixes) + adds round-2 tests for PATH CONTAINMENT (escaping prefix → `PERMISSION_DENIED`, dir created on disk) and TENANT ISOLATION (two tenants → distinct paths). The cross-tenant boundary, enforced by `filepath` resolution rather than convention.
- **`catalog` = sqlite — ✅ DONE 2026-05-31** (`examples/catalog/sqlite-py/`): passes the shared vectors + adds round-2 tests for DURABILITY (branches + snapshots + idempotency ledger survive reopen) and CONCURRENT-MERGE SAFETY (16 threads → exactly one winner via `BEGIN IMMEDIATE`, the publish gate's lost-update prevention).
- **`engine` = duckdb**, **`format` = parquet (pyarrow)**, **`runtime` = subprocess/container** — remaining (Arrow-heavy / real-execution). The format/engine ones subsume the typed-Arrow pass.

This also naturally subsumes the **typed-Arrow conformance pass** (a real format/engine backend forces the real Arrow Flight data leg, retiring the in-process stream-registry stand-in). Decision recorded 2026-05-31 (Tom: "finish wire contracts first, then round 2"). Until round 2 lands, the roadmap's per-axis "ADR-003 gate MET" means the wire-contract cross-run only.

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
| ADR-007 ✅ DONE | **Identity transport across the core-mediated hop** — DECIDED + MIGRATED 2026-05-31 ([ADR-007](../docs/architecture/adrs/007-call-context-transport.md)). `RequestContext` moved from message field 1 → `rat-callmeta-bin` metadata header across all 37 control sites; SDKs regenerated; both `format` refs + stub gateway updated; golden vectors green. Carry-over (optional, not blocking): a real SDK metadata interceptor so plugin code gets the reconstructed context automatically. | C1/C2 keystone |
| ADR-008 ✅ | **Streaming capability invocation** — DECIDED 2026-05-31 ([ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md)): add `InvokeServerStream` + `InvokeBidiStream` to `core/v1 CapabilityInvokeService` (generic byte-relays, enforce-at-open, identity in `rat-callmeta-bin` per ADR-007). **Migration is now the in-flight next step** (see [current.md](current.md)): add the 2 RPCs to `invoke.proto`, regen SDKs, server-stream relay in the stub gateway, route `runtime.Execute` through `InvokeServerStream` + add runtime's deferred `(rat.capability)` annotation, re-run runtime vectors. Unblocks gateway mediation for `state.Watch`/`scheduler.WatchDue`/`observability.Ingest` too. | ARCH (streaming) |

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

One of the two original deferred items has been resolved; one remains.

**Trigger (remaining):** land when real proto authoring experience reveals style patterns.

| Item | What | Why deferred |
|---|---|---|
| Path-scoped proto/manifest rule | A new `.claude/rules/proto-contracts.md` with `paths: ["**/*.proto", "**/plugin.yaml", "**/plugin/v1.json"]` capturing proto-authoring conventions: field naming, message nesting, service naming for Go gRPC, `buf.yaml` layout, capability URI format per ADR-002 D4. | The always-load `plugin-architecture.md` already captures architectural invariants. The path-scoped rule earns its place only once real proto files reveal tool-specific style patterns that don't belong in the always-load rule. Write after the first proto pass (sub-phase 0b). |

**Resolved (not landed):** `PostToolUse` auto-format hook — rejected on latency grounds. See `done.md` 2026-05-30 entry for the decision record.

**How to land the remaining item:**
1. Draft the rule content from actual proto authoring experience first.
2. Add the `paths:`-frontmatter file at `.claude/rules/proto-contracts.md`.
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
