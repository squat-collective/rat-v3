# Synthesis — 5-perspective adversarial review of RAT v3

> Five independent agents reviewed the founding architecture (ADRs 001–002 + vision + overview + conversations) from non-overlapping angles. Their conclusions converge on a tight cluster of cross-cutting findings that no single perspective would have surfaced alone.

## Team & individual reviews

| Perspective | File | Top finding (one sentence) |
|---|---|---|
| Adversarial architect | [01](01-adversarial-architect.md) | The 16 axes aren't orthogonal; contracts freeze before plugins exist to disprove the assumption. |
| Plugin ecosystem builder | [02](02-plugin-ecosystem-builder.md) | Best-in-class plugin *contract* design; near-zero plugin author *machinery* (scaffold, conformance, dev loop, signing). |
| Operations / SRE | [03](03-operations-sre.md) | "Operability is someone else's plugin" — every cross-cutting concern (trace, telemetry, upgrade, DR) is unowned. |
| Security reviewer | [04](04-security-reviewer.md) | Every security control deferred to a plugin → "trust the operator, verify nothing"; v2's solved patterns un-inherited. |
| Product / GTM | [05](05-product-gtm.md) | "Architecture is the product" is a precondition, not a strategy. Distribution motion is absent from every doc. |

---

## The five cross-cutting themes

The reviews don't just produce 5 different lists of findings — they keep surfacing the same underlying patterns from different angles. These five themes are the synthesis.

### Theme 1: The "everything is a plugin" mantra has a load-bearing blind spot

The principle ("the core does six things; everything else is a plugin") is correct for *axis*-shaped concerns where many independent implementations exist (engine, format, catalog, storage). It's *wrong* for *cross-cutting* concerns that genuinely have to span plugins:

- **Cross-plugin diagnostics** (ops) — no trace context in the wire contract.
- **Native core telemetry** (ops) — observability-as-plugin means the platform's own SLIs are opt-in.
- **Audit trail** (security) — `audit-log: none` is a valid setting, so there's no immutable record.
- **Plugin authentication** (security) — Q13 deferred, default `anonymous-root`.
- **Capability enforcement** (security + adversarial) — declared ≠ enforced; manifests are honor-system.
- **Trust boundary** (security) — sandboxing pushed to deployment-runtime with no contract.
- **Distribution / first-run UX** (gtm) — "the architecture is the product" leaves the user unowned.

**The pattern:** anything that has to be true across multiple plugins simultaneously, or that the *core* must enforce because no plugin can be trusted to, has been deferred or pushed onto the plugin layer. The discipline "the core stays minimal" has been applied to things that *can't* be minimized without breaking the platform.

> "Everything is a plugin" has quietly become "every cross-cutting concern is opt-in, and the default is off." — security reviewer
>
> "Operability was deferred to 'it's a plugin'." — ops/SRE
>
> "Architecture is the product" is a precondition, not a strategy. — gtm

### Theme 2: Contracts are being frozen before being stress-tested by real plugins

ADR-001's Phase 0 locks the most expensive-to-change artifact (the proto + manifest + capability triple) first, before any plugin exists to disprove the design. The AI-rewrite escape hatch (D1 meta-principle) explicitly only covers the *core* — it doesn't help when 50 community plugins depend on `engine/v1`.

Three reviewers independently flagged things that *must* be in the contract before freeze because they're wire-breaking to retrofit:

- **Trace / correlation context** in every RPC + every event envelope (ops).
- **Plugin-to-core authentication** primitive (security).
- **Resource asks / limits** as mandatory manifest fields (ops).
- **Conformance test obligations** as part of the axis contract (adversarial + plugin-eco).
- **Per-plugin state-gateway namespace** isolation (security).
- **Capability enforcement** (not just declaration) — adversarial + security.

**The fix consensus:** don't freeze any data-plane contract until **two independent reference implementations** exist and have run against each other on golden data. Let the second implementation, not a paper review, declare the contract done.

### Theme 3: V2 already solved problems v3 has un-solved

The most embarrassing pattern in the review set. The v2 codebase (`~/sandbox/ratatouille-v2/`) shipped:

- **ADR-020** — per-startup platform token with constant-time-compare; even hardened after a real `!=` leak.
- **ADR-019** — internal-listener-vs-public-listener split; the network trust boundary.
- **ADR-017** — the second-party trust model + container hardening (`read_only`, `cap_drop: ALL`, `no-new-privileges`).
- **~12 working plugins** — pg-sync, secrets, diff, compaction, demo-loader, docs-assistant, etc.

**V3 inherits none of these as contracts.** ADR-001/002 treat the security model as a `TODO` in `ideas/inbox.md`. The plugin corpus is treated as a future port, not a current asset. The "from-scratch" framing in vision.md ("we built it on lessons from v2") has not been operationalized — the lessons aren't in the v3 contracts.

> "v2 *already solved* the impersonation problem (ADR-020) and the network trust boundary (ADR-019), but v3 inherits neither as a contract." — security
>
> "RAT doesn't have to launch with an empty shelf — it can port a starter catalog." — gtm

### Theme 4: The 16 axes are not truly orthogonal

The data-plane cluster (engine → runtime → format → strategy → catalog → storage) is sold as 6 independent axes but is actually one coupled pipeline sharing Arrow buffers, file paths, vended credentials, and an isolation boundary. The manifest's `requires` expresses *structural* fit but not *semantic* compatibility.

Concrete consequences flagged:

- **Compatibility matrix explosion** (adversarial): 50 plugins × untested combinations = "fits ≠ works."
- **Capability honor-system** (plugin-eco): a format can declare `provides: merge` and fake it; the strategy author who trusted it gets blamed.
- **Multi-tenant breakout** (security): cross-tenant isolation requires 4 plugins (tenancy, identity, storage, engine) to agree on a boundary the core never sees.
- **State-gateway consistency leakage** (adversarial): the reconciler's correctness depends on consistency guarantees that differ between postgres / DynamoDB / sqlite.

**The fix consensus:** axis "orthogonality" must be a *tested* property, not an *assumed* one. Conformance suites per axis + verified-compatibility matrix.

### Theme 5: GTM is treated as downstream of architecture — the unique failure mode

Four out of five reviewers focus on engineering concerns. The fifth (product-gtm) flags the meta-pattern: the project knows the architectural failure modes ("OSGi outcome", "worse Snowflake") but proposes mitigations ("be too good to fork", "let architecture be a quiet credibility moat") that are restatements of the build-it-and-they-will-come trap, not escapes from it.

**Specifically absent from every doc:**
- A user-felt day-1 wow that *isn't* "look how cleanly this composes"
- A migration path off the *incumbent* stack (dbt/Airflow → RAT) — not just v2→v3
- A benefit-led marketing message that names a pain or enemy
- A founder-led, hand-to-hand first-100-users distribution motion
- A credible longevity / commercial-path signal

> "The most likely actual failure mode — 'we built it, shipped it, and nobody came because there was no distribution motion' — is not in the failure catalog." — gtm

---

## Findings, prioritized

### 🚨 Critical — address before any Phase 0 contract freezes

These are wire-breaking to retrofit. They must be in the contract triple v1.

| # | Finding | Owner |
|---|---|---|
| C1 | **Trace / correlation context** mandatory in every proto RPC + every event envelope (W3C `traceparent`). | ops |
| C2 | **Plugin-to-core authentication** — resolve Q13 *now*; port v2 ADR-020 (per-plugin token, constant-time compare) or mTLS. | security |
| C3 | **State-gateway isolation** — per-plugin key namespaces enforced by the gateway, capability-checked, deny-by-default cross-plugin. | security |
| C4 | **Resource asks / limits** as mandatory manifest fields (K8s-shaped). | ops |
| C5 | **Capability enforcement** — declared ≠ enforced; per-plugin call authorization at the gateway. | security + adversarial |
| C6 | **Conformance suite contract** — every axis defines its golden-data test set; "valid plugin" requires passing. | adversarial + plugin-eco |
| C7 | **Tenancy as structural** (not advisory hook) — state, bus subjects, credential scopes carry a tenant dimension in core primitives. | security |
| C8 | **Plugin supply-chain trust** — Sigstore/cosign signing default-required for team+; "install on utility" is not a trust model. | security |
| C9 | **Don't freeze any data-plane contract until 2 reference implementations exist** and have run against each other on golden data. | adversarial |
| C10 | **API gateway hardening** — internal-vs-public listener split (v2 ADR-019), rate limiting, restrictive CORS, mandatory authn. | security |

### ⚠️ Important — address by GA

| # | Finding | Owner |
|---|---|---|
| I1 | Native core observability (Prometheus `/metrics`, OTel spans) — independent of any observability plugin. Reconcile-loop SLOs published. | ops |
| I2 | Upgrade safety model — version skew policy (kubelet/apiserver-style), `rat preflight upgrade`, reversible state-schema migrations. | ops |
| I3 | Backup/restore — consistent backup set across state-backend + JetStream + plugin configs; RPO/RTO targets; pipeline/plane definitions as git-managed YAML. | ops |
| I4 | Plugin scaffolding — `rat plugin new --kind X --lang Y` producing a compiling, registering no-op plugin. Mock plugins per axis for isolated dev. | plugin-eco |
| I5 | Plugin local dev loop — `rat dev --plugin ./my-strategy` with manifest-on-disk override + file-watch reload. | plugin-eco |
| I6 | Event-bus authn/authz — bind publisher identity to events; NATS subject-level permissions per plugin (JetStream supports this). | security |
| I7 | Desired-state RBAC + admission control — who may create planes, bind axes, add subscriptions; signed/allowlisted image admission. | security |
| I8 | Mandatory audit trail — core-emitted, append-only, tamper-evident record of installs, binding changes, credential vends. Cannot be `audit-log: none`. | security |
| I9 | Deployment-runtime minimum isolation profile — non-root, `cap_drop: ALL`, `no-new-privileges`, read-only FS, seccomp, blocked metadata egress. | security |
| I10 | Crash-loop backoff + lease-renewal jitter + reconcile fairness — K8s lessons currently being re-lived. | ops |
| I11 | Plugin publish + signing path — `rat plugin publish`, OCI + Sigstore default. Pull Q15 forward. | plugin-eco + security |
| I12 | Plugin deprecation & compatibility governance — N-1 skew, `compatible_core: rat/1` checked gate, `rat plugin doctor`. | plugin-eco |
| I13 | Secret-handling contract — no secrets in events/logs/manifests; redaction obligation; short-TTL vended creds; encryption-at-rest. | security |
| I14 | Per-pipeline RPC amplification benchmark — measure before contract freezes (Phase 0 open thread). | adversarial |
| I15 | Reposition marketing message — anti-lock-in / cost-ownership, not "extensible." Lock the message before the first blog post sets the frame. | gtm |
| I16 | Migration path off the *incumbent* stack — dbt → RAT, Airflow → RAT (not v2 → v3). | gtm |
| I17 | First-five-minutes wow that *isn't* about plugins — port v2's `demo-loader` to the front door. | gtm |

### ✨ Nice-to-have — address as ecosystem matures

| # | Finding | Owner |
|---|---|---|
| N1 | Multi-region documentation — call out state-backend consensus requirement. | ops |
| N2 | Marketplace contract (`marketplace/v1`) — capability-aware "works on my deployment?" filter; signature display; federated publish. | plugin-eco |
| N3 | Plugin support / responsibility model — required `support_url` field; cross-plugin issue routing via `rat diagnose`. | plugin-eco |
| N4 | `rat audit` / `rat diagnose --security` — surface effective trust posture. | security |
| N5 | Plugin sandboxing tiers (first-party / verified / community) with per-tier capability defaults. | security |
| N6 | Public reproducible benchmark (Polars pattern) — one quantifiable hero metric. | gtm |
| N7 | Design-partner program — hand-pick 3-5 in year 1. | gtm |
| N8 | Founder-led distribution motion (content, community, design partners). The thing the docs don't talk about. | gtm |

---

## What's strong (don't lose this in the critique)

Every reviewer started with calibration. Compiled here so the critique doesn't drown the genuine wins:

1. **Engine / runtime / format / strategy split** is a real architectural insight — Arrow as the seam, strategies on runtime not engine. (adversarial)
2. **Data plane bypasses core for bytes** — correct, non-obvious, prevents the coordinator-as-chokepoint failure. (all 5)
3. **Reconciliation-as-source-of-truth** with events-as-hints — the right K8s lesson; degrades gracefully. (adversarial + ops)
4. **ADR discipline + rejected-alternatives tables + "track temptation count"** as a drift metric — most teams discover this in year-4 pain. (adversarial)
5. **No exotic bets on the stack** (Go + NATS + JSON Schema + Apache 2.0 + K8s patterns) — risk budget spent on architecture, not tech. (adversarial)
6. **Language freedom is first-class commitment** (contract = proto + manifest, no language-specific SDKs). (plugin-eco)
7. **Capability-not-implementation negotiation** (`requires: format-capability: [merge]` instead of `requires: format: iceberg`) — the cleanest single idea in the design *if* enforced. (plugin-eco)
8. **Major-version-only capability versioning** — refusing SemVer-range semantics dodges the npm peerDependencies swamp. (plugin-eco)
9. **No blessed vendor** — 3rd-party plugins are peers of first-party. Strongest motivational property in the design. (plugin-eco)
10. **30MB single binary + `chmod +x ./rat`** — the only GTM precondition you can't backfill. The MotherDuck-feel solo bundle is the door. (gtm)
11. **Same binary, solo → multi-tenant SaaS, no fork** — commercially smart; no rug-pull; clean open-core arc. (gtm)
12. **Small-core discipline + auditability** — a 5-10k LOC core has a tractable security audit surface most platforms don't. (security)
13. **Manifest-in-image** — closes manifest/image drift confusion; good substrate for signing (when signing exists). (security)
14. **NATS JetStream choice** — mature substrate with native authn/authz/accounts; tech allows isolation v3 just hasn't specified using. (security)

---

## What this means for the roadmap

Two big shifts the team thinks the project should make.

### Shift 1: Phase 0 is bigger than locking the contract triple

ADR-001 / ADR-002 frame Phase 0 as "spec the proto files + plugin.yaml schema." The review consensus is that Phase 0 must also include:

1. **Trace context + resource limits + plugin auth + capability enforcement + state-gateway isolation** in the contracts — anything wire-breaking to retrofit.
2. **Conformance suite scaffolding** per axis — the test set is part of the axis contract.
3. **Two reference implementations** of each critical axis (engine, format, catalog, state-backend) before declaring v1.
4. **Author-facing prose** alongside each proto — semantics, invariants, error taxonomy, idempotency expectations. The proto signature alone is necessary but wildly insufficient.
5. **Per-RPC latency benchmark** of the contract — the open-thread question that could invalidate the model if amplification is >10ms per format.Resolve.

This roughly doubles Phase 0 scope (~2-3 months → ~4-6 months) but the alternative is a frozen contract with a wire-breaking flaw at v0.x.

### Shift 2: GTM cannot be downstream of architecture

The architecture-as-product trap is real and named in the gtm review. The roadmap currently has zero items in the "distribution motion" column. The team's recommendation:

1. **Reposition** the README + vision tagline around anti-lock-in / cost-ownership, *not* "extensibility." Lock the message before the first external blog post.
2. **Port v2's `demo-loader`** to the front door as the first-five-minutes wow — make "0 → full warehouse with quality tests in 60s" the demo, not a plugin.
3. **Plan migration paths off the incumbent stack** (dbt → RAT, Airflow → RAT) — not just v2 → v3 (which has no users).
4. **Hand-pick 3-5 design partners** in year 1 and support them obsessively. This is unglamorous founder work; it's the job.
5. **Demote "16+ axes" from marketing headline** to internal architecture detail. The 5 plugins on the small-team critical path must be flawless before spreading.
6. **Stop treating community plugins as a traction lever.** Plan to write the next ~30 first-party plugins yourself. Plugins are a lagging adoption indicator, never leading.
7. **Add one GTM anti-goal to vision.md:** *"We will not ship a new plugin axis in year 1 until 100 real users run the core daily."* Force the roadmap to be pulled by users, not pushed by architecture.

---

## Proposed ADRs to add

Numbered prospectively. Each addresses a Critical or Important finding above.

| # | Title | Addresses | Priority |
|---|---|---|---|
| ADR-003 | Default solo bundle composition | gap (existing Q12) | Phase 0 |
| ADR-004 | Wire contract: trace context + correlation IDs | C1 | Phase 0 (blocking) |
| ADR-005 | Plugin-to-core authentication | C2 (Q13) | Phase 0 (blocking) |
| ADR-006 | State-gateway isolation + per-plugin namespacing | C3 | Phase 0 (blocking) |
| ADR-007 | Resource asks / limits in manifest contract | C4 | Phase 0 (blocking) |
| ADR-008 | Capability enforcement at runtime | C5 | Phase 0 (blocking) |
| ADR-009 | Conformance suite obligations per axis | C6 | Phase 0 (blocking) |
| ADR-010 | Tenancy as structural isolation | C7 | Phase 0 (blocking) |
| ADR-011 | Plugin supply-chain trust (Sigstore + manifest signing) | C8 | Pre-GA |
| ADR-012 | Two-reference-implementation rule for contract freeze | C9 | Phase 0 (process) |
| ADR-013 | API gateway hardening + listener split | C10 | Phase 0 (blocking) |
| ADR-014 | Core-native observability + SLOs | I1 | Pre-GA |
| ADR-015 | Upgrade safety: version skew + preflight + reversible migrations | I2 | Pre-GA |
| ADR-016 | Backup/restore + GitOps desired-state | I3 | Pre-GA |
| ADR-017 | Plugin scaffolding + local dev loop | I4, I5 | Pre-GA |
| ADR-018 | Event-bus authn/authz | I6 | Pre-GA |
| ADR-019 | Desired-state RBAC + admission control | I7 | Pre-GA |
| ADR-020 | Mandatory audit trail | I8 | Pre-GA |
| ADR-021 | Deployment-runtime minimum isolation profile | I9 | Pre-GA |
| ADR-022 | Reconciler robustness (backoff, jitter, fairness) | I10 | Pre-GA |
| ADR-023 | Plugin publish + signing pipeline | I11 | Pre-GA |
| ADR-024 | Plugin deprecation + compatibility governance | I12 | Pre-GA |
| ADR-025 | Secret-handling contract | I13 | Pre-GA |
| ADR-026 | GTM positioning + message canon | I15 | Now |
| ADR-027 | Incumbent-stack migration path strategy | I16 | Pre-GA |
| ADR-028 | First-five-minutes wow + front-door demo | I17 | Pre-GA |

**26 ADRs to write.** That's a lot — but each is small, each is addressing a real concern surfaced by independent expert review, and ~13 of them are wire-breaking-to-retrofit work that genuinely belongs in Phase 0.

---

## The honest read

The architecture is **good in the ways that are easy to be good** (axis decomposition, contract shape, core minimalism, K8s patterns) and **deferring in the ways that are hard** (trust, conformance, distribution, the cross-cutting concerns that no plugin owns).

The team's strongest recurring observation: **the project knows its failure modes** (the docs name "another OSGi", "worse Snowflake", "ecosystem stillborn" out loud) **and the proposed mitigations don't escape them.** "Be too good to fork", "let architecture be a quiet credibility moat", "the middle path doesn't exist — architecture decides" are all variations of *building it and hoping they come*. The architecture is necessary; it is not sufficient.

The synthesis recommendation is **not** "redesign anything." The architecture is sound. The recommendation is:

1. **Expand Phase 0 from "lock the contracts" to "lock the contracts *with* the cross-cutting concerns (trace, auth, resource, isolation, conformance) baked in"** — because retrofitting them is wire-breaking.
2. **Port v2's solved patterns** (ADR-017 / 019 / 020 + the plugin corpus) as v3 contracts — this is institutional memory that's currently un-inherited.
3. **Acknowledge the GTM gap** in the vision doc with a concrete distribution motion — hand-to-hand first-100-users work, design partners, a benefit-led message, a front-door demo.
4. **Adopt the "two reference implementations before contract freeze" forcing function** for each critical axis.

Do those four things and the architecture's bet has a real shot. Skip them and the unflattering scenario in the gtm review (2.5k stars, 0 production references, 3 external contributors, ecosystem stillborn) is the modal outcome.

The good news that closes every review: **the 5-10k LOC core discipline makes all the fixes tractable.** There's a small, auditable place to put the trust boundary, the diagnostic substrate, the capability enforcement. Most platforms accumulate these as bolt-ons because their core was too big to extend cleanly; v3 hasn't built the core yet, so it can build them in.

That's the synthesis. Five independent perspectives, one converged direction.
