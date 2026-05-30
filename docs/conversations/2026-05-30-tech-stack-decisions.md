---
date: 2026-05-30
participants: [Tom, Claude Opus 4.7]
topics: [tech-stack, language-choice, event-bus, manifest-validation, capability-versioning, reconciler-ha, manifest-source, v2-migration, cross-engine-queries, marketplace, license]
key_decisions:
  - All 10 open questions from ADR-001 resolved. See [ADR-002](../architecture/adrs/002-founding-tech-stack.md) for the full table.
  - Core language: Go (with AI-rewrite escape hatch as meta-principle).
  - Event bus: NATS JetStream, embedded for solo + external for team+.
  - Manifest validation: standalone JSON Schema (no proto generation).
  - Capability versioning: major version only (K8s pattern).
  - Reconciler HA: leader election + lease.
  - Manifest source: in-image with operator override.
  - v2 migration: none planned (no production users to migrate).
  - Cross-engine queries: core raises errors only; federation is plugin territory.
  - Marketplace: plugin axis; community plugin in default bundle.
  - License: Apache 2.0.
key_questions_opened:
  - Q11 — Should v2 keep shipping (since no production users)?
  - Q12 — Default solo bundle composition (exact plugin list + versions)?
  - Q13 — Plugin authentication to core (mTLS / bearer / both)?
  - Q14 — Marketplace plugin's UX shape?
  - Q15 — Sandboxing model for 3rd-party plugins?
status: distilled
---

# Tech stack decisions — 2026-05-30

## Context

Same day as [the vision conversation](2026-05-30-the-vision-conversation.md). After the founding ADR-001 ("everything is a plugin") landed with 6 open questions, plus 4 more flagged in `docs/architecture/overview.md`, Tom asked to walk through all 10 and lock them in.

This conversation was the walk-through: one question at a time, Claude recommended + offered alternatives via `AskUserQuestion` forms, Tom decided. Total: ~90 minutes of focused architectural debate. All 10 resolved.

## The shape that emerged

### Q01 — Core language: Go

Tom's reasoning was important: *"let's go with Go we could rewrite it with AI if we want to go rust someday."* This isn't just an answer to Q01 — it's a **meta-principle** for the project: AI-assisted refactoring lowers the cost of language migration enough that we should optimize for *velocity-friendly + ecosystem-aligned choices* now, accepting that re-language is a 2-4 week project later if needed. Stop over-optimizing for the perfect long-term substrate.

Concretely: Go aligns with our reference set (etcd, NATS, K8s, Temporal, Crossplane); has mature gRPC tooling; supports the 30MB-single-binary target; faster contributor onboarding; faster MVP. Rust would have given us smaller binary + compile-time safety + alignment with the data-tooling-Rust trend (Polars, Materialize, Datafusion) — but at the cost of slower velocity and a steeper learning curve. The AI-rewrite escape hatch eliminates the long-term risk of choosing wrong.

### Q02 — Event bus: NATS JetStream

Embeddable in the Go core for solo deployment (zero external deps); external cluster for team+. Same protocol throughout — switching mode is config, not code. JetStream's durability + replay + consumer groups cover the platform's needs. Multi-language clients excellent. Operationally simpler than Redis or Kafka.

Rejected: Redis (can't embed; bigger ops surface), Kafka (heavyweight; JVM; wrong fit for solo), build-our-own (reimplementing NATS poorly), Postgres LISTEN/NOTIFY (no replay, 8KB limit, doesn't scale).

### Q03 — Manifest validation: standalone JSON Schema

Manifest is operator-editable; JSON Schema is built for that (IDE autocomplete, inline errors). Proto types live separately, referenced by capability URI string (`rat://engine/v1.EngineService`). **Don't try to unify the two** — they serve different needs.

Pattern matches every successful plugin manifest system: VSCode `package.json`, K8s manifests, npm, Cargo.toml. Generation-from-proto would be over-engineered (proto's type system doesn't map cleanly to JSON Schema's strengths).

### Q04 — Capability versioning: major version only

Capability URIs encode version (`rat://format-capability/v1/merge`). Plugins require a specific major. Backward-compatible additions stay in same major; breaking changes ship as new major. Multiple majors coexist.

Same as K8s API versioning. No SemVer comparator semantics, no `>=1.2, <2` parsing libs, no cross-language inconsistency. Simpler and clearer.

Rejected: SemVer ranges (too granular, comparator semantics are subtle), capability flags (basically same as A in different syntax), multi-version compat (optional addition for migration scenarios only).

### Q05 — Reconciler HA: leader election + lease

K8s pattern. One replica holds the lease (via state-backend's CAS primitive); others serve API requests only. Failover ~15-20s — acceptable for control plane work. Simpler than active-active; sufficient until we're processing >1k reconciliations/sec (years away).

Active-active is the future scaling answer; sharded reconcilers are the extreme-scale answer. Both can be added later without breaking the leader+lease default.

### Q06 — Manifest source: in-image with operator override

Plugin image carries manifest at `/manifest.yaml` (or OCI label). Core extracts on install. Operators can override for compliance/air-gapped scenarios. Matches container-ecosystem norms; 95% of plugins just work.

Marketplace plugins become *aggregators* of in-image manifests (not source of truth). Phase 2+ work.

### Q07 — v2 migration: none planned

Tom's clarification was decisive: *"actually no v2 deployed really so we will maybe provide a migration tool later on top of it if needed by some actual users but I don't think we will need it for now."*

v2 has no production users. Pre-building migration tooling would optimize for users who don't exist. If/when one shows up, build the tool *for their actual data shape*. v3 stays unconstrained by v2's design.

**Side effect:** v2 doesn't need to keep shipping aggressively. Whether to still implement v2's ADRs 025/026 becomes a separate question (captured as Q11).

### Q08 — Cross-engine queries: core raises errors only

Tom's framing was sharper than Claude's recommendation: *"maybe someone will develop a plugin for it but not the job of the core I guess, the core should only raise errors I think."*

This is tighter than "refuse + suggest fix" — the core has no logic about materialization, federation, or suggestions. It just enforces the contract (one engine per query) and stops. Anyone wanting federation writes a `kind: engine` plugin (coordinator that fans out + joins in Arrow). Anyone wanting smart suggestions writes a `kind: pipeline-template-generator` plugin.

Maximally consistent with the founding ADR. Core stays minimal even at the cost of slightly worse default UX.

### Q09 — Marketplace: plugin axis + community plugin in default bundle

Tom added a nuance to Claude's recommendation: *"maybe with the community plugin included defaultly."* Core has no built-in marketplace. `kind: marketplace` is a plugin axis. Multiple competing marketplaces can coexist. **The default solo bundle includes a community marketplace plugin** so solo devs get discovery out of the box; enterprises swap it for their internal one.

Best of both: minimal core (no marketplace logic) + good default UX (plugin discovery works) + enterprise flexibility (replace the plugin).

### Q10 — License: Apache 2.0

Cloud-native ecosystem standard (K8s, Iceberg, Delta, Kafka, Spark, Polars, Datafusion). Permissive + explicit patent grant. Allows commercial use → encourages adoption + ecosystem.

Doesn't protect against AWS forking — accepted as a future problem. Can re-license later if needed (Elastic did this). For now, "be too good to fork" is the better insurance.

Rejected: MIT (missing patent grant — bad for systems infra), AGPL (adoption tax for enterprise; right answer for some businesses but wrong for substrate-ambition), BSL (controversial, blocks OSS-only procurement).

## Concrete artifacts produced

- [docs/architecture/adrs/002-founding-tech-stack.md](../architecture/adrs/002-founding-tech-stack.md) — locks all 10 decisions with the rejected alternatives.
- [docs/architecture/adrs/README.md](../architecture/adrs/README.md) — updated index includes ADR-002.
- [docs/architecture/adrs/001-everything-is-a-plugin.md](../architecture/adrs/001-everything-is-a-plugin.md) — Open Questions section updated to reference ADR-002.
- [docs/architecture/overview.md](../architecture/overview.md) — Known Unknowns section updated to reference ADR-002.
- [ideas/inbox.md](../../ideas/inbox.md) — new entries for Q11 (v2 strategy), Q12 (default bundle), Q13 (plugin auth), Q14 (marketplace UX), and the meta-principle on AI-rewrite.

## Open threads

These came up during the conversation but didn't need resolving:

- **Q11 — Should v2 ADRs 025/026 still ship?** Implication of D7 (no migration planned). If v2 has no users and v3 is the target, those ADRs were valuable as *thinking* but maybe not as code. Separate decision; tracked in inbox.
- **Q12 — Default solo bundle composition.** What exact plugins ship in `rat-bundle-solo`? Becomes its own ADR when Phase 0 progresses.
- **Q13 — Plugin auth.** Mutual TLS, bearer tokens, both? Probably varies by deployment-runtime.
- **Q14 — Marketplace UX shape.** Search by capability? Trust badges? Future ADR when the marketplace plugin is being built.
- **Q15 — Plugin sandboxing.** Per `deployment-runtime`? Signed images? Capability whitelist? Future ADR with security focus.

## Tone notes

This conversation was *fast and convergent*. Most questions took one form + one answer (vs the longer debates we had on the architecture itself). Tom's clarifications were sharp at exactly two moments:

1. **Q07 (migration):** Tom paused the form to clarify that v2 has no production users — which made the decision trivial (no migration plan). This is a useful pattern: when an option list contains assumptions the user wants to reframe, pause the form and clarify.
2. **Q08 (cross-engine queries):** Tom's "core only raises errors" framing was tighter than Claude's "refuse + suggest fix." Worth noting that when Tom reframes a recommendation, his framing is often more consistent with the founding principles than Claude's initial take.

**Other notable moment:** Q01's "let's go with Go we could rewrite it with AI if we want to go rust someday" — this single sentence became the project's meta-principle on language/framework choice. Whole-philosophy decisions sometimes emerge as one-liners; capture them as principles, not just answers.

This conversation unblocks Phase 0 (contract design + reference implementations). Next major decision point: bundle composition (Q12) when Phase 0 lands.
