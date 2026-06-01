# ADR-015: Phase-1 commitment gate CLEARED — commit to the full core build

## Status: Accepted (2026-06-01)

## Context

[ADR-013](013-phase-1-spike-and-commitment-gate.md) entered Phase 1 as a time-boxed
contract-de-risking spike and **deferred the 12–18mo commitment gate to the spike's exit
report** — recording that a deliberate "yes, commit" was a valid outcome, and that the
strategic v2-vs-v3 question (**Q01**) + external review (**Q02**) were owed before it.

The spike is complete ([reviews/10](../../../reviews/10-phase-1-spike-exit.md)): a real registry +
capability-invoke gateway enforce **C5** from declared manifests, the cross-axis pipeline runs
through it, **C1** crash-mid-strategy recovers, **C3** bounds a hung provider, and **D2** carries
a real ArrowStream ticket — all `go test`-green, and `make breaking` confirms **no frozen
contract was touched**. The frozen wire held; **no freeze-reopen.** The engineering risk the
board flagged ([reviews/09](../../../reviews/09-phase-1-gate-review.md): "green certifies *shapes*,
not *obligations*") is materially reduced on the load-bearing surface.

Tom has made the deferred call: **commit to the full core build.**

## Decision

**The Phase-1 commitment gate is CLEARED. The exploratory posture ends; RAT v3 commits to
building the full Phase-1 core** (the six things + cross-cutting enforcement, [ADR-001](001-everything-is-a-plugin.md)),
with the spike's `core/` as the seed.

### Rationale (Q01 — why a from-scratch v3 over evolving v2)

Per the founding premise ([vision.md](../../../docs/vision.md) + the project [CLAUDE.md](../../../CLAUDE.md)):
v2 carries baked-in assumptions — postgres-mandatory, ratd-as-orchestrator, portal-as-only-UI —
that cannot be incrementally evolved into the *everything-is-a-plugin* thesis; v3 is the parallel
from-scratch design built on that premise. The spike supplies the evidence the commitment was
waiting on: the from-scratch contracts are not just elegant on paper — a real enforcer builds
against them cleanly, with the hardest crash-safety + enforcement cases passing and the freeze
intact. *Why now:* delaying the core only ages the contracts; building proves them.

### Scope of the commitment

This clears the **Phase-0 → Phase-1** gate (the full core build). It is **not** a commitment to
skip the later user-pull gates: [phases.md](../../../roadmap/phases.md) **Gate B** (≥10 real solo
users before Phase 3), **Gate C/D**, and ADR-013 **Q02** (external peer review) remain in force —
the project stays honest about its modal failure mode (architecture without users).

## Consequences

**Positive.**
- Momentum on a de-risked foundation: the contracts are proven buildable-against, and `core/`
  already seeds the registry + gateway + the C5/C1/C3/D2 enforcement spine.
- The decision is recorded, not drifting — ADR-013's "acknowledged, not cleared" is resolved.

**Negative — accepted.**
1. **The runway is now being spent.** The ~3-month core build is real, irreversible engineering
   effort; if the later user-pull gates aren't met, it risks the "beautiful architecture, no
   users" outcome the project's own synthesis names as the modal failure. Mitigation: Gate B/C/D
   stay hard; Q02 external review still owed.
2. **Q02 (external review) remains open** — the spike was self-built + self-verified (no external
   human review yet). Schedule it *during* the core build, not after.

**Neutral.** Phase 1's roadmap status moves spike → committed full build; the acceptance criteria
are unchanged in substance (the spike did C5/C1/C3/D2 on a fake-provider surface; the full build
extends them to real providers + D3/D4/C4/sre#4).

## Definition of done (full Phase-1)

The board's exit criteria, now the committed build's bar: **C5** (spike ✅ — extend to real
providers) · **C4** terminal audit incl. denials · **C3** (spike ✅ — add the no-deadline
idle-timeout backstop) · **D1** a real *enforcing* deployment-runtime (podman, not in-process) ·
**D2** (spike ✅ — wire into the real bulk leg) · **D3** storage-cred isolation · **D4**
conformance-attestation *enforced* · **C1** (spike ✅ — against real backends) · **sre#4**
reconciler crash-loop backoff / jitter / lease-thrash.

## Alternatives considered

1. **Continue exploratory** (re-eval at the next milestone). Rejected *now*: the engineering
   uncertainty that justified the exploratory posture is exactly what the spike resolved;
   continuing to hedge spends runway without capturing the decision. It stays the fallback if
   Q02 / Gate-B signals turn negative.
2. **Redirect to v2.** Rejected: no spike finding indicated a v3 contract flaw; the from-scratch
   thesis stands and is now evidence-backed.

## Related

- [ADR-013](013-phase-1-spike-and-commitment-gate.md) — deferred this gate to the spike report
  (this ADR is the recorded "yes").
- [reviews/10](../../../reviews/10-phase-1-spike-exit.md) — the spike evidence this rests on ·
  [reviews/09](../../../reviews/09-phase-1-gate-review.md) — the board's proceed-with-conditions.
- [ADR-001](001-everything-is-a-plugin.md) — the six things / the full Phase-1 scope ·
  [ADR-014](014-spike-core-registry-and-invoke-gateway.md) — the spike core that seeds the build.
- [phases.md](../../../roadmap/phases.md) Phase 1 + the decision gates · [vision.md](../../../docs/vision.md) — the v3-over-v2 premise (Q01).
