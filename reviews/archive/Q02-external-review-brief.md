# Q02 — External peer review: reviewer brief

> **Status:** the ask for the Q02 external review owed by [ADR-013](../../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) §Open-questions and [reviews/09](../09-phase-1-gate-review.md) (the gate review's dissent: *zero* external human review so far).
> **Confidentiality:** RAT v3 is unpublished and the contract freeze is **local/unpushed**. Please treat everything here as confidential and do not redistribute.
> **Audience:** an external practitioner who has *built* a plugin platform or a control plane (see "Who we're looking for").

## The ask, in one paragraph

RAT v3 is a from-scratch reimagining of a data platform built on one premise: **a data platform is a minimal control plane that orchestrates self-describing plugins.** We've frozen the contracts (Phase 0, tag `rat/1.5`) and built + sealed the core (Phase 1, tag `rat/2.0`) that enforces them against real launched plugins. **Every review so far has been internal** — adversarial, multi-perspective, but all self-generated, so it shares our blind spots. Before committing to the 12–18-month bet of taking this to feature parity, we want **genuinely independent expert eyes** on the architecture and the frozen contracts. Tell us where the premise is wrong, where the contracts will force a painful v2, and where we're cheerfully repeating a mistake your platform already paid for.

## What RAT v3 is (the 60-second version)

- **A six-thing core** (target 5–10k LOC, Go): registry · identity gateway · state gateway · event bus · reconciler · API gateway. *Everything else is a plugin.*
- **16+ plugin axes** — state-backend, deployment-runtime, engine, format, catalog, storage, scheduler, identity, tenancy, billing, observability, ui, … Each plugin is self-describing.
- **One contract triple:** `.proto` (services) + `plugin.yaml` (manifest: `kind`, `provides`/`requires` capabilities) + `rat://<axis>/v<major>/<capability>` URI strings. Capability *negotiation*, never plugin-to-plugin coupling by name.
- **A reconciliation model** ("K8s for data"): operators declare desired state; the core drives convergence; the deployment-runtime plugin launches/heals plugin processes; events are hints, the reconciler always re-reads state.
- **One binary, many topologies:** `chmod +x ./rat` (solo) → multi-tenant cloud SaaS, same core, different plugin sets.
- **Anti-goals:** not a K8s replacement (sits on top), not an ORM/query-language/warehouse (engines bring their own), not scope-creep-friendly (adding to the core is presumed wrong until an ADR proves it can't be a plugin).

Read `CLAUDE.md` + `docs/vision.md` for the full premise and anti-goals.

## Where we are

- **Phase 0 — contracts frozen** (`rat/1.5`): 18 axes, `.proto` + per-kind manifest schemas, two independent reference implementations per data-plane axis before freeze ([ADR-003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)), golden-data conformance vectors.
- **Phase 1 — the core, sealed** (`rat/2.0`): a spike grew into a real control plane — manifest-driven registry, a capability-invoke gateway (capability authz + audit + deadline/idle bounding), two deployment-runtimes (local-process + a podman runtime that enforces the full kernel-level isolation profile), a supervisor, a reconciler with leader-election, conformance-attestation verification, and the Arrow bytes-leg ticket. Nine board-defined exit criteria (C1, C3, C4, C5, D1, D2, D3, D4, sre#4) are all green **against real launched plugins**, and the frozen wire held throughout (`make breaking` clean). See [reviews/10](../10-phase-1-spike-exit.md) and `roadmap/done.md`.

## Who we're looking for

People who have *operated or built* one of: an extension/plugin platform (OSGi, VSCode, Eclipse, Backstage), a control plane / orchestrator (Kubernetes, Nomad, Temporal, Crossplane), a messaging/eventing substrate (NATS, Kafka), or a package/dependency ecosystem (Cargo, npm). We specifically want scars, not enthusiasm.

## Scope

**In scope** — the *architecture and contracts*, the load-bearing bets, and the blind spots internal review can't self-check:
- `docs/vision.md`, `docs/architecture/overview.md`, and ADRs 001–016.
- The frozen contracts under `contracts/proto/**` (+ the `CONTRACT.md` author guides).
- The Phase-1 core design under `core/**` (design-level; a glaring code issue is welcome, but this is not a line-by-line code audit).
- The internal review corpus (so you can challenge *its* conclusions).

**Out of scope** — a security code-audit of the spike implementation; GTM/pricing execution (a later phase); the existing v2 codebase; anything requiring the work to be published (it isn't, by design, yet).

## What internal review already covered (please don't just re-derive this)

A five-perspective adversarial review + board reviews are in `reviews/` (start with [reviews/00-synthesis.md](../00-synthesis.md)):

| file | lens | the load-bearing thing it surfaced |
|---|---|---|
| [01](../01-adversarial-architect.md) | architect | "tier-0" plugins (state-backend / deployment-runtime / bus) are bootstrap-critical, not hot-swappable |
| [02](../02-plugin-ecosystem-builder.md) | ecosystem | capability negotiation is a lie unless *conformance* is enforced, not declared |
| [03](../03-operations-sre.md) | SRE | the failure domain is huge + undiagnosable; the state-backend is a hidden tier-0 SPOF; crash-loop/lease-thrash hazards |
| [04](../04-security-reviewer.md) | security | credential vending + the bytes-leg ticket are the real trust boundaries |
| [05](../05-product-gtm.md) | product/GTM | the ecosystem cold-start problem |
| [00](../00-synthesis.md) | synthesis | C1–C10 cross-cutting concerns the core's *enforcement layer* must own (trace propagation, per-plugin isolation, mandatory audit, native observability) |

We acted on these (they shaped the frozen contracts + the Phase-1 exit criteria). **What we cannot do is verify we found *all* of them, or that our fixes are right.** That's the gap you're filling.

## The questions we most want answered

Grouped; pick what your experience speaks to. Sharp disagreement is the deliverable.

**1 — The founding premise.**
- Is "everything is a plugin (the core does six things)" a sound organizing principle, or does it have a structural blind spot we haven't named? (We found cross-cutting concerns + tier-0; what did we miss?)
- Is the six-thing core actually *minimal and complete*? Is any of the six secretly two things? Is anything load-bearing wrongly pushed out to a plugin?
- Tier-0 (state-backend, deployment-runtime, bus) are "plugins selected at boot." Is the bootstrap/chicken-and-egg story sound — or is calling them plugins marketing over a hard dependency?

**2 — The contract triple + the frozen wire.**
- Is `.proto` + `plugin.yaml` + `rat://` capability URIs the right contract surface — sufficient, not over/under-specified?
- The wire is **frozen** (no breaking changes without a major). Knowing that, what field or message shape will we regret — i.e., force a v2 break? (We did two-references-before-freeze; where did that *not* save us?)
- Conformance/attestation: the core verifies `declared == conformed` via a signed attestation (so a plugin can't claim a capability it didn't pass golden vectors for). Does this actually close the "capability negotiation is a lie" hole, or is there a way around it?

**3 — The data plane.**
- Bulk data **bypasses the core** (Arrow Flight; the `ArrowStream` ticket — HMAC-signed, TTL'd, single-use, `{stream,caller,tenant}`-bound — is the *only* gate). Is that enough when the core can't mediate the bytes? What's the attack/abuse surface?
- Credential vending (storage plugin issues short-TTL, tenant+prefix-scoped creds) is a C7 tenancy boundary. Holes?

**4 — Operability (our SRE review's hardest area).**
- Debuggability across *N polyglot plugin processes*: "why didn't my pipeline run?" Is mandatory W3C trace-context + correlation propagation enough, or is this still a 3am black box?
- The observability-as-a-plugin paradox: the core emits its *own* `/metrics` + OTel natively (independent of any observability plugin). Right call?
- Upgrade/version-skew across independently-versioned plugins; DR/backup consistency across state-backend + event log + plugin state — where will this bite?

**5 — Ecosystem & the bet.**
- The cold-start problem: will third parties actually *write* plugins? What makes the author value-proposition real (or not)?
- Is the marketplace + conformance + signing trust model enough to make "install many 3rd-party plugins" safe *and* attractive?

**6 — Prior art.**
- Where does RAT v3 repeat a known mistake of OSGi / K8s / VSCode / Temporal / Airflow? Where does it diverge from them, and is the divergence justified or naïve?

**7 — (Optional, not the primary ask.)** From your vantage, is a *from-scratch* v3 worth it over evolving an already-shipping v2 with the same lessons? (This is our own Q01 — a strategic call we own — but an outside read is welcome.)

## Already acknowledged (please don't flag these as novel)

These are known, documented, and deferred — not blind spots:
- **C2 channel auth** — the spike derives caller/tenant from the call envelope; the full core re-derives it from the *authenticated channel* (mTLS/token). Deferred.
- **Write-leg idempotency vs a real backend** — proven against the catalog's durable ledger; a real *idempotent format* reference doesn't exist yet.
- **Metadata-egress** — blocked via the container netns today; an explicit cloud egress-drop + a structured `IsolationAttestation` message are GA items.
- **Audit signing** — the spike's audit records are unsigned; the core's first real signing (ed25519, in conformance attestation) is the seed for signing the audit chain at GA.

## Materials & suggested reading order

1. `CLAUDE.md` — the premise + working discipline (10 principles).
2. `docs/vision.md` — the *why* + anti-goals.
3. `docs/architecture/overview.md` — the *what* (the six things, the reconciliation model, the data-plane bypass).
4. ADRs [001](../../docs/architecture/adrs/001-everything-is-a-plugin.md) (everything-is-a-plugin), [002](../../docs/architecture/adrs/002-founding-tech-stack.md) (tech stack), [003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (two-references-before-freeze).
5. [reviews/00-synthesis.md](../00-synthesis.md) — what internal review concluded (challenge it).
6. `contracts/proto/**` — the frozen wire (skim `common/v1`, `catalog/v1`, `format/v1`, `deploymentruntime/v1`, `storage/v1`).
7. `core/README.md` + `core/**` — the Phase-1 core.
8. `roadmap/current.md` + `roadmap/done.md` — exactly where we are and how we got here.

## How to deliver findings

A markdown doc (we'll file it as `reviews/11-q02-external-<name>.md`). For each finding:

```
### <short title>
- Severity: Critical | High | Medium | Low | Nit
- Area: premise | contracts | core | data-plane | operability | ecosystem | prior-art
- Finding: <what's wrong / risky / missing>
- Why it matters: <the consequence, ideally with a concrete failure scenario>
- Suggested direction: <optional — we value the problem even without a fix>
```

Plus a one-paragraph **bottom line**: *would you make this bet, and what's the single thing you'd change before committing?* A "Critical" should mean "I would not proceed until this is resolved."

## Logistics

- **Effort:** a focused 1–2 day read is plenty; go deeper only where your scars pull you. We'd rather have three sharp findings than thirty shallow ones.
- **Engagement:** async is fine; a follow-up call to walk the findings is welcome.
- **Confidentiality:** as above — unpublished, freeze local/unpushed; please don't redistribute.

## Related

- [ADR-013](../../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) (Q01/Q02) · [reviews/09](../09-phase-1-gate-review.md) (the dissent that owes this review) · [reviews/00-synthesis.md](../00-synthesis.md) (internal review to challenge) · [reviews/10](../10-phase-1-spike-exit.md) (the spike exit report).
