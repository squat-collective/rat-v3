# Ideas inbox

Append new ideas at the bottom. Format: `## YYYY-MM-DD — [tags] one-line title` + a few sentences.

---

## 2026-05-30 — [naming] Project codename

`RAT v3` is the working title since the folder is `rat/`. Possible future renames if we want a distinct identity:
- **Pith** (the central core of a stem)
- **Atrium** (the central courtyard everything flows through)
- **Conduit**
- **Sieve**
- **Burrow** (where RATs live — ties to brand)

Decision can wait until we ship. The internal codename matters less than the eventual product name.

---

## 2026-05-30 — [marketplace, distribution] Plugin distribution as a first-class concern

VSCode's marketplace + Cargo's crates.io are why those ecosystems flourished. RAT v3 needs a plugin marketplace from year 1, not as an afterthought. Options:
- GitHub-based: discover via topic tag `rat-plugin`, install via `gh` CLI.
- Dedicated registry: like crates.io, hosted by the project.
- Multi-source: operator points at N registries (one curated, one community, one internal).

Open question: should the marketplace ALSO be a plugin? (Yes, almost certainly. `kind: marketplace`.)

---

## 2026-05-30 — [contracts, schemas] Generate manifest schema from proto?

If proto files define the gRPC service shapes, we could generate the `plugin.yaml` manifest schemas from them. Reduces drift between "what a plugin says it provides" and "what the proto actually defines."

Tradeoff: protobuf and JSON Schema have different expressiveness. Generation works for simple cases; complex constraints (cross-field validation) might not transfer. Worth a spike.

Related: Q03 in [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md).

---

## 2026-05-30 — [event-bus, performance] Event bus failure modes

If the event bus is the coordination substrate, what happens when it's degraded?
- Stale notifications → reconciler converges slowly but eventually
- Lost events → reconciler needs idempotent retries (every loop iteration)
- Out-of-order events → reconciler must be reorder-tolerant

This argues for the reconciler being **the source of truth, not the events**. Events are hints for "you might want to look now"; the reconciler always re-reads state. Same model as K8s.

Future ADR: event-bus durability semantics + reconciler-as-source-of-truth.

---

## 2026-05-30 — [security] Plugin sandboxing

A 3rd-party `rat-plugin-foo` runs *somewhere* in the operator's environment. Trust model:
- Solo: same process as core; full trust. Container model overkill.
- Team: containerized; trust at the network level.
- Enterprise: signed images + capability whitelist + network policy.

Each level is a different `deployment-runtime` plugin doing different isolation. The core doesn't enforce sandboxing — the deployment-runtime does. Worth an ADR when we start implementing the runtime axis.

Related: v2's ADR-017 (Python pipeline trust model) — same pattern.

---

## 2026-05-30 — [migration] Bridge plugins from v2 to v3

**Resolved in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D7**: no migration plan now; build a tool reactively if a real production user surfaces. v2 has no production users today, so pre-building optimizes for users who don't exist.

---

## 2026-05-30 — [meta-principle, language-choice] AI-assisted rewriting lowers language-choice stakes

Tom's reasoning during Q01: *"let's go with Go we could rewrite it with AI if we want to go rust someday"*. This is a load-bearing meta-principle worth banking. When picking foundational tech (language, framework, etc.), the cost of "wrong choice" has shifted dramatically — AI-assisted refactoring of a 10k-LOC codebase is genuinely viable. So: bias toward velocity-friendly + ecosystem-aligned choices NOW; accept that re-language is a 2-4 week project later if needed. **Don't over-optimize for "perfect long-term language."** This applies recursively to framework choices, ORM choices, serialization choices, etc.

Save as principle for the project; cite in future ADRs when stuck on "this technology choice is hard."

---

## 2026-05-30 — [v2, strategy] Should v2 keep shipping?

**Q11 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** Implication of D7: if v2 has no production users and v3 is the real target, should we still implement v2's ADR-025 (on-demand planes) and ADR-026 (manifest+registry)? Those ADRs were valuable as *thinking* — they shaped v3's design — but actually building them in v2 might be wasted effort. Worth a separate decision when there's bandwidth.

Open question: when do we declare v2 feature-frozen?

---

## 2026-05-30 — [bundles] Default `rat-bundle-solo` composition

**Q12 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** What exact plugin set ships in the default solo bundle? Probably:
- state-backend: sqlite
- secret-backend: env
- scheduler-backend: in-process
- identity: anonymous
- deployment-runtime: local-process
- ui: web-portal
- engine: duckdb-embedded
- runtime: pyarrow-embedded
- format: iceberg-embedded
- catalog: sqlite-iceberg-catalog (or simpler — file-based catalog?)
- storage: local-fs
- observability: stdout
- marketplace: community-marketplace

But each is a real choice. Becomes ADR-003 (or similar) when Phase 0 lands. Versions matter — bundle pins specific plugin versions for reproducibility.

---

## 2026-05-30 — [security] Plugin authentication to core

**Q13 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** When a plugin contacts the core (or core contacts a plugin), what auth model? Options:
- Mutual TLS (cluster-style)
- Bearer tokens (simple but rotation matters)
- Both (mTLS for production, bearer for dev)
- None for solo (in-process), upgrade for team+

Probably the last — auth model varies by deployment-runtime. Future ADR when core API hardens.

---

## 2026-05-30 — [marketplace, UX] Marketplace plugin's discovery shape

**Q14 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** The marketplace plugin needs a UX: search by capability, by name, by author? Trust badges? Reviews? Compatibility checking (does this plugin work on my deployment)?

Worth a dedicated ADR when the marketplace plugin is being built. Look at: VSCode marketplace UX, Cargo's crates.io, Helm Hub, OperatorHub.io for patterns.
