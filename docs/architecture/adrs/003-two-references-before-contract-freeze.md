# ADR-003: Two independent reference implementations before any contract freezes

## Status: Accepted (2026-05-30)

## Context

The 5-perspective adversarial review of [ADR-001](001-everything-is-a-plugin.md) + [ADR-002](002-founding-tech-stack.md) surfaced one finding that the team rated *most consequential*: **contracts designed in a vacuum are wrong in ways only revealed by the second and third real implementation of each axis** (see `reviews/01-adversarial-architect.md` Finding 8 and `reviews/00-synthesis.md` Theme 2). The single most-cited recommendation across all five reviewers was: don't freeze any data-plane contract until at least two independent implementations exist and have run against each other on golden data.

The forcing function exists because of a structural asymmetry the project commits to:

- The **core** can be rewritten cheaply (ADR-002 D1's meta-principle: AI-assisted rewriting). 10k LOC of Go is a 2-4 week project to redo.
- The **contracts** cannot. Once `engine/v1` is published and N plugins are written against it, breaking it requires coordinating a flag day across the ecosystem — the same flag-day problem [ADR-002 D4](002-founding-tech-stack.md) explicitly avoids for capability versioning by choosing the K8s major-only model.

The review found that the project's sequencing inverts this asymmetry: ADR-001's Phase 0 freezes the most-expensive-to-change artifact (the contract) earliest, when zero plugins exist to disprove the design, and defers the cheapest-to-change artifact (the core) for after. The AI-rewrite escape hatch is repeatedly invoked to de-risk the language choice, but it doesn't help when 50 community plugins depend on a frozen `engine/v1`.

This ADR resolves that inversion by introducing a hard process gate on contract freezing.

## Decision

**No data-plane contract may be tagged `v1-frozen` until two independent reference implementations exist, both pass the axis's conformance suite, and have been run against each other on golden data.**

Explicitly:

### The rule applies to these axes (the data plane)

- `engine/v1` — two engine implementations (e.g., duckdb + datafusion)
- `runtime/v1` — two runtime implementations (e.g., pyarrow + polars)
- `format/v1` — two format implementations (e.g., iceberg + delta)
- `catalog/v1` — two catalog implementations (e.g., nessie-fronting + sqlite-iceberg-catalog)
- `storage/v1` — two storage implementations (e.g., s3 + local-fs)
- `state/v1` — two state-backend implementations (e.g., sqlite + postgres)

### "Independent" means

- Written by different code paths (not just two configurations of the same library)
- Using different underlying technologies where possible (the iceberg-vs-delta example, not iceberg-on-nessie vs iceberg-on-lakekeeper)
- Targeting different consistency / semantic profiles where applicable (sqlite gives serializable; dynamodb gives per-item CAS — these are different enough)

### "Run against each other on golden data" means

The references are composed in a cross-combination and exercised on a published golden test set. At minimum:

- Engine A + Format A + Catalog A + Storage A (the baseline)
- Engine A + Format B + Catalog A + Storage A (format-substitution test)
- Engine A + Format A + Catalog B + Storage A (catalog-substitution test)
- Engine B + Format A + Catalog A + Storage A (engine-substitution test)

Cross-combinations that surface contract assumptions (e.g., "this only worked because both impls used the same Arrow dialect") are what justify the rule.

### Pre-freeze contracts ship as `v1-preview`

While Phase 0 is in flight, contracts are tagged `v1-preview` — they're public for collaborators and peer reviewers to read, but explicitly **not stable**. Breaking changes are allowed. Once the two-reference + cross-combination + conformance gates pass, the tag advances to `v1` and the rule applies in reverse: breaking changes now require a `v2`.

### The rule does NOT apply to

- **Control-plane axes** (`identity/v1`, `secret-backend/v1`, `scheduler/v1`, `tenancy/v1`, `audit-log/v1`, `observability/v1`, `notifications/v1`, `marketplace/v1`, `billing/v1`) — these are less coupled to other axes; one reference + conformance suite is sufficient for v1.
- **Experience axes** (`ui/v1`) — same reasoning.
- **The plugin manifest schema** (`plugin/v1`) — validated structurally; iterate freely during Phase 0; freeze when it stabilizes across the data-plane reference work.

The rule is targeted at the axes most likely to suffer from the orthogonality-assumption failure flagged in the synthesis (Theme 4).

## Consequences

**Positive.**

- The C9 forcing function eliminates the most-likely contract-freeze regret: discovering a wire-breaking flaw after publication.
- Reference implementations *are* documentation — they're the canonical answer to "how do I write a plugin for this axis."
- Phase 0 produces 12 working reference plugins (6 axes × 2 impls) that dual-purpose as starter templates for the ecosystem (the plugin-ecosystem-builder review's Stage 3 gap).
- Cross-combination testing surfaces semantic compatibility issues *before* the matrix explodes (the adversarial-architect review's Finding 2: "with 50 plugins the compatibility matrix is combinatorial").
- The `v1-preview` tier gives external collaborators something to integrate against without locking us in.

**Negative — accepted.**

1. **Phase 0 timeline grows from 1-2 months to 4-6 months.** Twelve reference implementations + cross-combinations + conformance suites is real work. Mitigation: highly parallelizable; 2-3 people can compress to ~4 months. The alternative — frozen contracts with wire-breaking flaws — is worse.
2. **More upfront engineering before the platform "exists" in any user-visible sense.** Mitigation: the reference plugins ARE usable (they pass conformance against the contract); they just aren't production-grade. They're concrete, runnable, demonstrable.
3. **Some axes may not have an obvious second implementation candidate.** For example, finding a credible second `storage` implementation that isn't S3-compatible takes care (GCS / Azure-blob are reasonable; local-fs is the easy fallback). Mitigation: pick second implementations early so they're known by the time they're needed.
4. **The rule is a process discipline that can be eroded under pressure.** A future maintainer under deadline pressure may be tempted to freeze a contract with one reference. Mitigation: the rule is documented here as an ADR; violations require a new ADR overriding this one (the discipline metric in [CLAUDE.md](../../../CLAUDE.md): "track the temptation count").

**Neutral.**

- This ADR doesn't apply retroactively to ADR-002's decisions (those are tech-stack choices, not contracts). It governs only the data-plane axis contracts in Phase 0 and beyond.

## Alternatives considered

1. **Single reference per axis, peer reviewed.** What ADR-001 originally proposed. **Rejected because:** peer review catches obvious flaws; only an independent implementation catches the subtle ones (the entire history of OSGi shows this). The cost of catching one wire-breaking issue at v0 vs v1 is N plugins' worth of coordination.

2. **Three or more references per axis.** Maximally rigorous. **Rejected because:** diminishing returns. Most cross-combinatorial issues surface with the second implementation; the third adds bake time but rarely new categories of finding. Two is the right tradeoff between rigor and effort.

3. **Defer the rule until after first ecosystem feedback.** Ship `v1` with one reference; iterate based on community plugin authors' issues. **Rejected because:** the moment `v1` is published, breaking changes have a coordination cost. The rule is specifically about avoiding the first breaking change being a forced one.

4. **Apply the rule to all axes uniformly.** Including control-plane and experience axes. **Rejected because:** control-plane axes are loosely-coupled in practice (an identity plugin doesn't constrain a tenancy plugin the way a format plugin constrains an engine plugin). Uniform application would double Phase 0 cost for marginal benefit on those axes.

## Migration

This ADR takes effect immediately. The Phase 0 description in [ADR-001](001-everything-is-a-plugin.md) is being updated in the same commit to reflect:
- The 4-6 month timeline (was 1-2 months)
- The Critical concerns that must be baked into the contracts before freeze (trace context, plugin auth, resource limits, state-gateway isolation, capability enforcement, conformance suites, tenancy structure)
- The two-reference-implementation gate from this ADR

No prior work needs to be undone — Phase 0 hasn't started.

## Related

- [ADR-001](001-everything-is-a-plugin.md) — "everything is a plugin"; this ADR refines its Phase 0.
- [ADR-002](002-founding-tech-stack.md) — founding tech stack; the meta-principle on AI-rewrite-as-escape-hatch is the asymmetry that makes this ADR necessary.
- [reviews/00-synthesis.md](../../../reviews/00-synthesis.md) — Critical finding C9 + Theme 2.
- [reviews/01-adversarial-architect.md](../../../reviews/01-adversarial-architect.md) — Finding 8 ("the contract triple is frozen in Phase 0, before any plugin exists to stress it") and "the single most dangerous bet."
- [reviews/02-plugin-ecosystem-builder.md](../../../reviews/02-plugin-ecosystem-builder.md) — Stage 5 (conformance suite as honor-system fix); Stage 3 (reference plugins as starter templates).
- Prior art: the K8s CSI conformance suite (`csi-sanity`), Kubernetes CNI Conformance, gRPC interop tests — same pattern, different domain.
