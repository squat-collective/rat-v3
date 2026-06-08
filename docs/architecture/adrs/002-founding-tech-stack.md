# ADR-002: Founding tech stack + strategy decisions

## Status: Accepted (2026-05-30)

## Context

[ADR-001](001-everything-is-a-plugin.md) set the founding *principle* — the core does six things, everything else is a plugin. That ADR left ten open questions about the actual tech stack and strategy choices needed to start Phase 0 (contract design + reference implementations).

This ADR resolves all ten. Each was deliberated with Tom; rejected alternatives are listed so the reasoning survives.

## Decisions

| # | Decision | Rejected | Why |
|---|---|---|---|
| **D1** | **Core language: Go.** *(Locked & ratified by [ADR-004](004-core-language-go.md), 2026-05-30.)* | Rust, Zig, exotic | Every distributed control plane we model (etcd, NATS, K8s, Temporal, Crossplane) is Go. Mature gRPC tooling. Faster MVP. Larger contributor pool aligned with cloud-native ecosystem. **Meta-principle accepted:** AI-assisted rewriting lowers the cost of language migration; bias toward velocity-friendly language now and re-language later if needed. |
| **D2** | **Event bus default: NATS JetStream, embeddable in core.** | Redis Streams, Kafka, build-our-own, postgres LISTEN/NOTIFY | Only option that embeds in a Go binary (zero deps for solo dev) AND scales to production cluster. Same protocol throughout — embedded vs external is a config flag. JetStream gives durability + replay + consumer groups. Multi-language clients excellent. |
| **D3** | **Manifest validation: standalone JSON Schema, proto referenced by URI.** | Schema-from-proto generation, hybrid embed-proto, defer | Manifest is operator-editable; JSON Schema is built for that (IDE autocomplete, inline errors). Deep types live in proto files separately, referenced by capability URI string (e.g. `rat://engine/v1.EngineService`). Don't try to unify proto + JSON Schema — they serve different needs. Matches VSCode, K8s, npm, Cargo manifest patterns. |
| **D4** | **Capability versioning: major version only, encoded in URI.** | SemVer ranges, capability flags, multi-version compat | Same as K8s API versioning. URIs are `rat://kind/v1/capability`. Backward-compat additions stay in same major; breaking changes ship as new major. Multiple majors coexist. No range comparators, no SemVer libs needed, no per-language version-parsing inconsistency. |
| **D5** | **Reconciler durability: leader election + lease.** | Active-active + optimistic concurrency, sharded, single-replica | One replica holds lease via state-backend's CAS primitive; others serve API only. Same pattern as K8s scheduler / controller-manager. Failover ~15-20s — acceptable for control plane. Simpler than active-active; sufficient until we're processing >1k reconciliations/sec (years away). Sharded is the future answer at extreme scale. |
| **D6** | **Manifest source: in-image, with operator-side override.** | Separate registry-as-source, operator-only, all-three | Plugin image carries manifest at `/manifest.yaml` (or OCI label). Core extracts on install. Operators can override locally for compliance/air-gapped scenarios. Matches container-ecosystem norms; 95% of plugins just work. Marketplace becomes an *aggregator* of in-image manifests (not a source of truth) — Phase 2+. |
| **D7** | **v2 → v3 migration: none planned; build a tool reactively if a real production user surfaces.** | Bridge plugins, dual-API transitional core, hard cutover | v2 has no real production users today. Pre-building migration tooling optimizes for users who don't exist. If/when one shows up, build the tool *for their actual data shape*. v3 stays unconstrained by v2's design; v2 doesn't have to keep shipping aggressively. **Open implication:** v2's ADRs 025/026 may not be worth implementing on v2 if v3 is the real target — decide separately. |
| **D8** | **Cross-engine queries: core only raises typed errors; federation is plugin territory.** | Built-in federation engine in v1, engine-as-source pattern, runtime-fail-only | If a query references tables from planes with different engines, return a plan-time error: "Cannot query across engines X and Y in one statement." That's it. Core has no logic about materialization, federation, or suggestions. If anyone wants federation, write a `kind: engine` plugin (a coordinator that fans out + joins in Arrow). If anyone wants materialization-suggestions, write a `kind: pipeline-template-generator` plugin. **Core stays minimal, even at the cost of slightly worse default UX.** |
| **D9** | **Marketplace: plugin axis, with a community marketplace plugin in default solo bundle.** | Pure community in core, curated in core, no marketplace at all | Core has no built-in marketplace. `kind: marketplace` is a plugin axis — multiple competing marketplaces can coexist (community-open, curated, enterprise-internal). Default `rat-bundle-solo` includes a community marketplace plugin so solo devs get discovery out of the box. Enterprises swap it out for their internal one. |
| **D10** | **License: Apache 2.0.** | MIT, AGPL/SSPL, BSL | Cloud-native ecosystem standard (K8s, Iceberg, Delta, Kafka, Spark, Polars, Datafusion — every project we model). Permissive license + explicit patent grant. Allows commercial use → encourages adoption + ecosystem. **Doesn't protect against AWS forking** — accepted as a future problem (can re-license to BSL/SSPL later if it becomes real; Elastic did this). For now, "be too good to fork" is the better insurance. |

## Consequences

**Positive.**
- Phase 0 (contract design + reference implementations) can start. Every tech-stack ambiguity that would have blocked design is resolved.
- Decisions cluster around proven patterns: K8s for reconciliation + API versioning + leader election; NATS for messaging; Apache 2.0 + JSON Schema + OCI norms for ecosystem fit. **No exotic bets.**
- Strong "everything is a plugin" discipline: even cross-engine federation and marketplace are deferred to plugins. The core gets to stay minimal.
- The default solo bundle has a clear shape: Go binary + embedded NATS + sqlite + local-fs + duckdb + web-portal + community marketplace plugin. The 30MB-single-binary promise is concretely buildable.

**Negative — accepted.**
1. **Go locks us into GC pauses + ~10MB runtime overhead.** Mitigation: AI-assisted rewriting is the escape hatch (D1's meta-principle). If 5-year scale demands no-GC, port the core; plugins are language-agnostic and unaffected.
2. **No v2 migration commitment is a risk if v2 unexpectedly grows users.** Mitigation: monitor v2 adoption; if a real user surfaces, scope migration work then.
3. **Cross-engine queries fail with errors at v1.** Honest, but might frustrate early users. Mitigation: clear error messages; federation-as-plugin community contribution opportunity.
4. **Apache 2.0 doesn't prevent cloud-fork.** Mitigation: this is a year-3+ concern; build community + product first.
5. **NATS JetStream's track record is shorter than Kafka's.** Mitigation: NATS is mature for our scale; massive-scale users can swap in Kafka via the bus plugin axis.
6. **Manifest-in-image couples manifest version to image version.** Mitigation: operator-side override exists; works for the long tail.

**Neutral.**
- The deferred questions (federation engine, marketplace UX, v2 migration tool) all become *future plugins* or *future ADRs*. Each unblocked, each independently scopable.

## Open questions (deferred, not blocking Phase 0)

These came up during the discussion but didn't need locking now:

- **Q11.** Should v2's ADRs 025+026 still be implemented on v2, or pivot the energy to v3? (D7's open implication.)
- **Q12.** Default solo bundle composition — exact plugin list + versions. (Becomes ADR-003 when Phase 0 lands.)
- **Q13.** Plugin authentication to core — mutual TLS, bearer tokens, or both? (Becomes ADR-N when core API hardens.)
- **Q14.** Marketplace plugin's UX shape — discovery API, search semantics, trust badges. (Future ADR when marketplace plugin is built.)
- **Q15.** Sandboxing model for 3rd-party plugins — per `deployment-runtime`? Container-isolated? Signed images? (Future ADR with security focus.)

Captured in [`ideas/inbox.md`](../../../ideas/inbox.md) for later promotion.

## Alternatives considered

Each decision's `Rejected` column captures the alternatives. The meta-alternative — *don't lock these decisions yet, leave them open through Phase 0* — was rejected because:
- Contract design can't start without language (D1) and event bus model (D2)
- Manifest schema can't start without validation approach (D3) and versioning (D4)
- Reference implementations can't start without reconciler model (D5) and manifest source (D6)
- Strategic clarity (D7-D10) is needed to write the public-facing roadmap

Leaving them open would have meant doing Phase 0 with ambiguity baked in. Locking now lets Phase 0 be a forcing function for these decisions in practice — if any decision turns out wrong, we'll discover it during contract design and amend (or supersede) this ADR.

## Migration

This ADR is foundational; nothing to migrate from. The next actions:

- **Update [`docs/architecture/overview.md`](../overview.md)** to reference this ADR's resolutions in place of "known unknowns" Q01-Q10.
- **Update [`docs/architecture/adrs/001-everything-is-a-plugin.md`](001-everything-is-a-plugin.md)** to reference this ADR's resolutions in place of its Open Questions Q01-Q06.
- **Phase 0 begins:** spec proto files for every axis + `plugin.yaml` JSON Schema. Probably 2-4 weeks.

## Related

- [ADR-001](001-everything-is-a-plugin.md) — the founding principle; this ADR resolves its open questions.
- [docs/conversations/2026-05-30-the-vision-conversation.md](../../conversations/2026-05-30-the-vision-conversation.md) — the conversation that produced ADR-001 and ADR-002.
- [docs/conversations/2026-05-30-tech-stack-decisions.md](../../conversations/2026-05-30-tech-stack-decisions.md) — the conversation where these specific 10 decisions were debated.
- [docs/architecture/overview.md](../overview.md) — needs updating to reference this ADR.
- [research/prior-art/](../../../docs/research/prior-art/) — NATS, K8s, OSGi, VSCode references underpin many of these choices.
- v2's ADR-024 (decoupled axes), ADR-025 (on-demand planes), ADR-026 (manifests) — inform many of these decisions.
