# ADR-017: Pre-unfreeze contract-amendment gate (the Q02 dry-run punch-list)

## Status: Proposed (2026-06-02)

> Proposed, not Accepted, on purpose: this ADR ratifies a set of *directions* (one of
> which — PU-4 — is a genuine product-scoping fork for Tom), and it rests partly on a
> **simulated** review. It flips to Accepted once the per-item directions are ratified
> and the **real** Q02 external human review (ADR-013 Q02) either confirms or extends it.

## Context

The RAT v3 freeze (`rat/2.0`: the 18-axis data + cross-cutting wire frozen since `rat/1`,
plus the sealed core) is deliberately **LOCAL/unpushed** — a standing condition from
[ADR-013](013-phase-1-spike-and-commitment-gate.md) (§3) carried through
[ADR-015](015-phase-1-commitment-gate-cleared.md). The whole point of keeping it local is
that a regret found **before** publication is *additive* (cheap), while the same regret
found **after** is a `v2` break (expensive). That window is still open.

The **Q02 simulated dry-run** (2026-06-02 — a 5-lens adversarial panel; synthesis in
[reviews/Q02-tracker.md](../../../reviews/Q02-tracker.md), findings in
[reviews/11-q02-*.md](../../../reviews/)) stress-tested the frozen wire and found **zero
hard freeze-reopen** (no demand to change a frozen message shape) but a concrete set of
**additive / contract-surface regrets** that are cheap now and expensive after publish,
plus one conformance-debt item that *qualifies the "freeze validated" claim*
[ADR-015](015-phase-1-commitment-gate-cleared.md) rests on.

This ADR converts that synthesis into a **decision**: the explicit gate the freeze must
pass before it ever leaves local/unpushed. It is the dry-run's own #1 recommended artifact.

**Honesty caveat (load-bearing).** The dry-run used AI personas, which share the project's
own blind spots — exactly what the real Q02 exists to escape ([reviews/09](../../../reviews/09-phase-1-gate-review.md)
dissent: "zero external human review"). So this gate is **provisional**: the real external
review remains owed and may add to or reprioritize it.

## Decision

**The freeze does not leave local/unpushed until BOTH (a) the punch-list below is resolved
AND (b) the real Q02 external human review has run.** The punch-list (severities + evidence
in the per-lens docs; the maintainer's net-new triage in
[11-q02-maintainer-defense-log.md](../../../reviews/11-q02-maintainer-defense-log.md) is
authoritative):

### 1. PU-1 — Bytes-leg producer channel-authentication MUST *(soft freeze-reopen; 2 lenses; maintainer's #1)*
Add a **normative conformance MUST** to `common/v1/data.proto`'s `ArrowStream` prose + a
conformance vector: *a conformant ArrowStream producer MUST verify that the presenting
channel's authenticated identity equals the ticket-bound `{caller_plugin, tenant}`;
transport/app-layer headers are insufficient.* Ship channel-auth (Arrow Flight over mTLS,
or a token the core vends alongside the ticket) as the **SDK default** so third parties
can't get it wrong by omission. **Wire impact: none** (contract-surface MUST + vector; no
message change). Rationale: the bytes leg bypasses the core by design, so C2 cannot reach
it; the reference trusts raw `X-RAT-*` headers (`bulkleg_test.go:39`) → a leaked/replayed
ticket presented with the right headers succeeds. This is the single most important finding.

### 2. PU-2 — Keystone context-envelope conformance *(pre-unfreeze confidence; no wire change)*
Add a **context-carriage conformance suite** (vectors asserting: missing `traceparent` →
reject; `caller_plugin` re-derived not propagated; stream open-stamp semantics; the M4
bare-mirror cross-check) and require a **second independent gateway** to cross-pass it
before `common/v1/context.proto` + the gateway-stamping contract are treated as
frozen-with-confidence. Rationale: the most-irreversible surface (the carrier for
C1/C2/C3/C7) has the **weakest** conformance — [ADR-007](007-call-context-transport.md)
§Neutral concedes the golden vectors don't assert on context carriage, and one impl
exercises it, not the two-reference cross-run [ADR-003](003-two-references-before-contract-freeze.md)
mandates for the data axes it was bundled into. **This narrows ADR-015's claim to "the
freeze is validated *on the data axes the spike exercised*."** No field changes — but it
gates publication *confidence*, so it is in the gate.

### 3. PU-3 — Attestation lifecycle: expiry + revocation + scoped authorities *(soft freeze-reopen)*
Add `expires_at` and a revocation reference to the conformance-attestation / marketplace
shape (additive fields), and design revocation + scoped/threshold signing authorities (no
single key is a forge-oracle). Rationale: `Conforms` is static set-membership = "conformed
forever," and a single `Authority` keyring means one leaked key = full trust. **Wire impact:
additive fields** (awkward to add post-major, hence pre-publish).

### 4. PU-4 — Tenancy scope: isolation-only vs sharing-capable *(soft freeze-reopen — DECISION NEEDED, see Q01)*
**Recommended (needs Tom's ratification):** declare **v1 tenancy isolation-only** — mark
`DECISION_KIND_SHARING` as *advisory-not-enforced* so the axis stops advertising an
un-actionable verb — and defer actioned cross-tenant sharing + hierarchical tenancy to a
future `v2` delegation primitive (its own ADR). Rationale: `DECISION_KIND_SHARING` is today
*decidable but un-actionable* on flat-string keys (no delegation/grant shape in `state`/
`storage`); the cheap, honest path is to scope v1 to the isolation it actually enforces,
and no user is pulling for cross-tenant sharing yet (Gate B unmet). **The alternative** —
making v1 sharing-capable — requires adding the delegation primitive to `rat/1` **now**,
because retrofitting cross-tenant semantics onto the namespace post-publish is the expensive
`v2`. This is the one genuine fork in the punch-list.

### 5. Decide-the-additive-now seams *(the additive door closes at publish)*
- **5a — Semantic-field-skew negotiation** (architect F2 / maintainer A1): the
  [ADR-012](012-crash-safety-additive-fields.md) crash-safety fields (`already_applied`,
  `expected_rows/batches`) shipped as plain additive fields with **no negotiation handle**
  → a version-skewed idempotency consumer silently double-applies (proto3 makes the field
  absent = `false` on an old provider). **Decide now:** a documented
  capability-URI-per-behavior convention **and/or** an additive `requires[].min_revision`
  discriminator the gateway checks. *Recommend: document the convention + add `min_revision`
  additively.*
- **5b — `Event` signing** (architect F7): mirror the signed-and-hash-chained `AuditRecord`
  on `Event` (core signature + `key_id` over canonical bytes), additively, **before**
  subscribers trust the currently-unsigned in-body `context.tenant` delivered by a
  *pluggable* bus transport.
- **5c — `vend-credentials` read/write split** (security F6): split into read/write
  capability URIs (additive) so C5 can authorize *mode*, not just the capability — least
  privilege is currently inexpressible.

### What is deliberately NOT in this gate
The **multi-tenant-availability cluster** (AV-1..7), **tier-0 / observability** (T-1,
O-1/O-2), **provider-selection language** (P-1), **discipline** (K-2, D-1), and **ecosystem
on-ramp** (EC-1..6) are **core-impl / GTM, not frozen-wire** — they gate *real multi-tenant
use* and *adoption*, not the *freeze-publish* decision. They live in
[backlog.md](../../../roadmap/backlog.md). Note **AV-1** (`core/lease` has no error channel)
is "fix first" because it's free today and a breaking refactor once a durable backend binds
the `bool` interface — but it is a **Go-interface** change, not a wire change, so it is
*urgent Phase-1 hardening*, not a publish-gate item. Conflating the two gates is exactly the
mistake this section prevents.

## Consequences

**Positive.** The freeze publishes only after its *known* additive regrets are absorbed
while they are still additive — the precise value the local-freeze discipline was preserving
([ADR-013](013-phase-1-spike-and-commitment-gate.md) §3). ADR-015's confidence claim is
honestly qualified (PU-2) and then repaired. The gate is small and bounded — 4 PU items +
3 seams, all additive or conformance.

**Negative — accepted.** (1) Real pre-publish engineering: PU-1/PU-3/5a–5c are additive
proto + conformance work, and PU-2 is a *second gateway implementation* — non-trivial.
(2) The gate rests partly on a **simulated** review; the real Q02 may extend it, so
publication is twice-gated (punch-list + real review) and slower. (3) PU-4 forces a
product-scoping decision *now* (recommended: isolation-only) that closes off effortless v1
cross-tenant sharing.

**Neutral.** None of PU-1..3 or 5a–5c breaks a frozen message; they are additive /
contract-surface, consistent with the `rat/1.x` additive-door precedent
([ADR-010](010-catalog-commit-linkage.md), [ADR-012](012-crash-safety-additive-fields.md)).

## Open questions

- **Q01** — PU-4: is v1 tenancy **isolation-only** (recommended) or **sharing-capable**?
  Tom's call; the answer decides whether a delegation primitive must land in `rat/1` now or
  becomes a documented `v2`.
- **Q02** — Does the **real** Q02 external review confirm this gate, or add/reprioritize
  items? The gate is provisional until it runs.
- **Q03** — Sequencing: do the PU items land as **one** coordinated `rat/2.x` amendment cut,
  or incrementally? *(Recommend one coordinated pre-publish cut, then re-seal.)*

## Alternatives considered

1. **Publish the freeze now; fix regrets as `v2`s later.** Rejected: discards the cheap-fix
   window the freeze was kept local to preserve — PU-1 (a contract MUST) and PU-3 (additive
   attestation fields) become breaking or awkward post-publish, and third-party producers
   will already have shipped trusting headers.
2. **One ADR per PU item.** Rejected: they share a single concept — *the pre-unfreeze gate*.
   Per-item ADRs come only if an item needs a deep standalone decision (PU-4 may spawn a
   tenancy-delegation ADR *if* sharing-capable is chosen).
3. **Treat the dry-run as sufficient; skip the real Q02.** Rejected: simulated personas
   share the project's blind spots; outside human eyes are the entire point of Q02
   ([reviews/09](../../../reviews/09-phase-1-gate-review.md) dissent).
4. **Fold the availability cluster (AV-1..7) into this gate.** Rejected: those are
   core-impl, not frozen-wire; gating *freeze-publish* on *operability* conflates two
   distinct gates (publish-readiness vs multi-tenant-production-readiness).

## Migration

This is the bridge from **sealed-but-local `rat/2.0`** to **publishable `rat/2.x`**:

1. **Ratify** the per-item directions — especially **PU-4 (Q01)**.
2. **Land** PU-1 + PU-3 + 5a–5c as one additive proto + conformance cut; land **PU-2**'s
   second-gateway conformance suite. `make breaking` must stay clean (all additive).
3. **Run** the real Q02 external human review against the amended surface.
4. If confirmed, **re-seal** as `rat/2.x` — and *only then* consider unpush.

`AV-1` (lease error-channel) and the rest of the availability cluster proceed **in parallel**
as Phase-1 hardening, independent of this gate.

## Related

- [reviews/Q02-tracker.md](../../../reviews/Q02-tracker.md) — the dry-run synthesis this ADR
  operationalizes · [reviews/11-q02-*.md](../../../reviews/) — the per-lens findings ·
  [11-q02-maintainer-defense-log.md](../../../reviews/11-q02-maintainer-defense-log.md) —
  the authoritative net-new triage.
- [ADR-013](013-phase-1-spike-and-commitment-gate.md) — the local-freeze standing condition
  + the Q01/Q02 open questions · [ADR-015](015-phase-1-commitment-gate-cleared.md) — the
  "freeze validated" claim PU-2 qualifies.
- [ADR-009](009-data-plane-contract-freeze-v1.md) / [ADR-010](010-catalog-commit-linkage.md)
  / [ADR-012](012-crash-safety-additive-fields.md) — the freeze + the additive-door
  precedent this gate stays inside · [ADR-007](007-call-context-transport.md) — the keystone
  carriage move whose conformance gap is PU-2 · [ADR-003](003-two-references-before-contract-freeze.md)
  — the two-reference rule PU-2 extends to the envelope.
- [backlog.md](../../../roadmap/backlog.md) "Q02 simulated dry-run findings" — the non-gate
  (core-impl / GTM) items.
