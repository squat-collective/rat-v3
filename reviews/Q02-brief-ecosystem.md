# Q02 — reviewer brief (ECOSYSTEM / plugin-author focus)

> An ecosystem-tailored companion to the [main reviewer brief](Q02-external-review-brief.md). It front-loads the **adoption + plugin-author questions**; the main brief has the full architecture, the non-ecosystem questions, and the logistics. Read this if your lens is "will an ecosystem actually form around this?"
> **Confidentiality:** RAT v3 is unpublished and the contract freeze is **local/unpushed**. Please treat everything as confidential and don't redistribute.

## The ask, as an ecosystem builder

RAT v3's entire value proposition is the **plugin ecosystem** — the differentiator is *"fully pluggable everything; if a competitor can do X without a plugin, we did it wrong."* A platform like that lives or dies on whether third parties actually build, publish, and trust plugins. You've watched ecosystems reach critical mass (VSCode, K8s operators, dbt adapters, Backstage) and watched others stall (OSGi outside Eclipse, most "platforms"). **Would you build a plugin for this — would you bet your tool's distribution on it — and does this reach a self-sustaining ecosystem or die at cold-start?** All review so far has been internal ([reviews/02-plugin-ecosystem-builder.md](02-plugin-ecosystem-builder.md), [reviews/05-product-gtm.md](05-product-gtm.md)); we want your outside read on the adoption thesis we can't validate from inside.

## RAT v3 in one ecosystem-relevant paragraph

Every load-bearing concern is a **plugin** behind a **capability contract**: a plugin declares `provides`/`requires` capabilities as `rat://<axis>/v<major>/<capability>` URIs and is wired purely by **capability negotiation** — never coupled to a peer by name. A plugin is a gRPC service (the axis `.proto`) + a `plugin.yaml` manifest, in any language with gRPC. The platform's compatibility question — *"does this plugin work on MY deployment?"* — is answerable *because* of the capability model: a marketplace matches a listing's required/provided/**conformed** capabilities against what a deployment provides. (Full architecture: the main brief + `docs/vision.md`.)

## What's REAL vs PAPER (read before you judge the ecosystem)

The Q02 review is of the **architecture/strategy**; here's how much exists:

**Real (Phase 0–1):**
- **The contract triple is frozen** (`rat/1.5`): 18 axes, `.proto` + per-kind `plugin.yaml` JSON schemas + the `rat://` capability grammar.
- **30-plus reference plugins** across all 18 axes — and per [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md), **two technologically-divergent references per data-plane axis** before any freeze (e.g. DuckDB *and* DataFusion engines; local-process *and* k8s runtimes), each passing shared **golden-data conformance vectors**. So the contracts are not single-implementation artifacts.
- **Capability negotiation is enforced, not asserted (D4):** the core verifies `declared == conformed` via a signed attestation — a plugin can't claim a capability it didn't pass the golden vectors for. ("Capability declared" is no longer a lie — the thing [reviews/02](02-plugin-ecosystem-builder.md) flagged as the ecosystem's make-or-break.)
- **Generated SDKs** (Go + Python are exercised by the reference plugins) + a manifest schema + a conformance suite an author runs.

**Paper / partial (the ecosystem-load-bearing gaps — assume NOT solved):**
- **The ecosystem itself: zero third-party plugins.** All ~30 are first-party references. The **cold-start problem is unsolved** (it's the Phase-4 GTM motion).
- **The marketplace** is a frozen *contract* + one reference (`community-py`), not a running market; **manifest signing (C8)** for install-trust is deferred.
- **Author tooling / DX** is partial — a manifest-validation script + the schema + the conformance vectors exist; a polished `rat plugin init/validate/test` on-ramp + local dev loop do not.
- **Governance** of the `rat://` namespace + the process for the community to add new axes/capabilities is ADR-*described* but not *formalized*.

## The ecosystem-health surface

| surface | status | your job |
|---|---|---|
| capability contract (provides/requires/URIs) | ✅ frozen | is it the right author mental model + granularity? |
| conformance enforced (declared==conformed, D4) | ✅ real | does it make capability-trust real *enough* to auto-match? |
| reference plugins (30+, 2-per-axis) | ✅ real | are they a credible on-ramp an author copies? |
| author DX (scaffold / validate / test / dev-loop) | ⚠️ partial | is the bar inviting or daunting? |
| marketplace (compatibility oracle) | ⚠️ contract + 1 ref | sound + sufficient for discovery + trust? |
| supply-chain trust (conformance + signing) | ⚠️ signing deferred | enough to install a 3rd-party plugin safely *and* willingly? |
| governance / openness of `rat://` | ⚠️ paper | open enough to bet a tool's distribution on? |
| the ecosystem itself (3rd-party plugins) | ✗ zero | how does it cross cold-start? |

## Ecosystem questions (the heart)

**A — The cold-start problem (existential).** A plugin platform is worthless without plugins; authors won't invest without users; users won't come without plugins. RAT v3 has ~30 first-party references and **zero** third-party plugins. How does it cross to a self-sustaining ecosystem — what's the *wedge* (the one axis/use-case where an author has an undeniable reason to build first)? Where have you seen this work (and what made it work) vs stall?

**B — Plugin-author DX.** A conformant plugin = a gRPC service (the axis `.proto`) + a `plugin.yaml` + passing the golden-data vectors. Is that bar inviting for, say, a data-tool vendor adding a RAT plugin in a weekend — or daunting? Is the contract triple (`.proto` + manifest + `rat://` URIs) ergonomic, or is "capability-think" too abstract for most authors? Is polyglot (any gRPC language) real leverage or a support burden? What's missing from the on-ramp (scaffolding, `rat plugin init/validate/test`, a tight local dev loop, great docs)?

**C — Capability negotiation as the differentiator.** The bet: pluggability-via-capability-negotiation (no plugin-by-name coupling) is *the* moat. Is that how authors + operators will actually think, or too clever by half? With D4 making `declared == conformed` enforceable, can a deployment genuinely auto-answer "does this plugin work here?" Is the capability **granularity** right (too fine → manifest sprawl + brittle matches; too coarse → false "it'll work" matches)?

**D — Discovery & trust (marketplace).** The marketplace's one hard job is the compatibility oracle: match a listing's required/provided/conformed capabilities against the deployment's. Listings make those + a signature *mandatory*. Is that model sound + sufficient? Multiple coexisting marketplaces (community/curated/enterprise) — healthy competition or fragmentation? Is the supply-chain story (conformance + signing — signing currently deferred) enough to make installing a third-party plugin both *safe* and *attractive*?

**E — Versioning & compatibility (author side).** The wire is frozen (`rat/1`), evolving additively within a capability major. Can an author target `engine/v1` and trust it won't break under them? Version **skew** between an author's plugin and the user's *core* version — is there a guarantee an author can rely on (the kubelet/apiserver-skew analog, from the author's vantage)? ADR-002 D6 couples manifest version to image version — does that hurt authors (can't fix a manifest bug without re-releasing the image)?

**F — Governance & openness.** Who owns the `rat://<axis>/...` namespace? The design says the community can add new `kind:` axes + capabilities **without core changes** — is that real and author-friendly, or does one vendor effectively control the registry/marketplace/contract (the open-core rug-pull fear)? What governance would a serious third party need to *see* before betting its distribution on RAT?

**G — Incentives / value prop.** Concretely: why would a vendor build a RAT plugin instead of (or alongside) an Airflow provider / dbt adapter / Singer tap / K8s operator? What's the author's payoff — reach, less glue code, a genuinely better integration model — and is "everything is a plugin" a *reason to build* or an architecture the author is indifferent to?

## What ADR-003 + D4 already settled (please don't re-flag)

- **Capability negotiation is not a lie:** D4 enforces `declared == conformed` (signed attestation). Validate its *sufficiency*; don't re-flag it as unverified.
- **The contracts aren't single-impl artifacts:** ADR-003 required two divergent references per data-plane axis, passing shared vectors, *before* freeze. Critique the contract's *author ergonomics*, not its having-been-tested.

## Already acknowledged (don't flag as novel)

The zero-third-party cold-start (Phase-4 GTM, unsolved); manifest signing (C8) deferred; the marketplace is a contract + one reference, not a running market; author DX tooling is partial; `rat://` governance is not yet formalized; the GTM/distribution motion is Phase 4.

## Materials & reading order (ecosystem-relevant)

1. This brief + the ecosystem-health table.
2. [reviews/02-plugin-ecosystem-builder.md](02-plugin-ecosystem-builder.md) + [reviews/board/ecosystem.md](board/ecosystem.md) + [reviews/05-product-gtm.md](05-product-gtm.md) — the internal ecosystem/GTM reviews (challenge them).
3. `docs/vision.md` — the premise + the "if a competitor can do X without a plugin, we did it wrong" anti-goal; [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md) (the 18 plugin axes) · [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (two-references-before-freeze).
4. What an author actually touches: `contracts/schema/plugin.v1.json` (the manifest they write) · `contracts/proto/rat/marketplace/v1/` (`marketplace.proto` + `CONTRACT.md` — the compatibility oracle) · `contracts/conformance/` (the golden vectors they must pass) · `examples/**` (the reference plugins they'd copy — pick an axis and imagine authoring a third).
5. `roadmap/backlog.md` (the marketplace/signing/DX/governance items) + `roadmap/phases.md` (Phase 4 = GTM/ecosystem).

## Findings & logistics

Same format + logistics as the [main brief](Q02-external-review-brief.md#how-to-deliver-findings): per-finding {severity · area · finding · why-it-matters · suggested-direction}, plus a bottom line — *would you build a plugin for this / bet a tool's distribution on it, and what's the single thing that would most grow (or kill) the ecosystem?* A **Critical** = "the ecosystem will not form until this is resolved." A focused 1–2 day read is plenty; unpublished + confidential.
