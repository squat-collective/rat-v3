# Q02 — reviewer brief (ARCHITECT / contracts focus)

> A foundations-tailored companion to the [main reviewer brief](Q02-external-review-brief.md). It front-loads the **premise, the core boundary, and the frozen-wire questions**; the main brief has the full context, the non-architectural questions, and the logistics. Read this if your lens is "is the foundation sound?"
> **Confidentiality:** RAT v3 is unpublished and the contract freeze is **local/unpushed**. Please treat everything as confidential and don't redistribute.

## The ask, as an architect

Two things are now hard to undo, which is exactly why we want you. **The premise is committed** (RAT v3 is a 12–18-month bet on "the platform is a minimal control plane orchestrating self-describing plugins"), and **the wire is frozen** (`rat/1` — a contract mistake found later is a `v2` break, not a patch). Our internal architecture + proto reviews ([reviews/01-adversarial-architect.md](01-adversarial-architect.md), [reviews/06-proto-contract-review.md](06-proto-contract-review.md), [07](07-freeze-review.md), [08](08-post-freeze-board-review.md)) were thorough but self-generated — they share our blind spots. **Find the structural flaw in the premise, the wrong line in the core boundary, or the frozen-wire shape we'll regret** — before we build a year on top of it.

## RAT v3 in one architecture-relevant paragraph

The core does **six things** — registry · identity gateway · state gateway · event bus · reconciler · API gateway — and *everything else is a plugin* (18 axes: engine, format, catalog, storage, deployment-runtime, state-backend, scheduler, identity, tenancy, …). Plugins are wired by **capability negotiation**: a plugin declares `provides`/`requires` as `rat://<axis>/v<major>/<capability>` URIs and is never coupled to a peer by name. The contract is a **triple**: `.proto` (services) + `plugin.yaml` (manifest) + the capability URIs. The control plane is **reconciliation-based** (desired→actual; events are hints; optimistic concurrency — the K8s controller pattern); **bulk data bypasses the core** (control RPCs carry refs + small metadata, never bytes). (Full architecture: `docs/architecture/overview.md`.)

## What's SETTLED vs OPEN for an architect

- **Frozen (`rat/1.5`) — regret is expensive.** 18 axes, `.proto` + per-kind manifest schemas + the capability grammar. Evolves **additively** within a capability major; a break needs a new major. Per [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md), **two technologically-divergent references per data-plane axis** passed shared golden vectors *before* freeze, and the proto reviews ([06](06-proto-contract-review.md)–[08](08-post-freeze-board-review.md)) found + fixed a set of freeze-blockers. So the wire is tested-by-≥2-impls; your job is the *subtle* regret those missed.
- **Committed — but this review is the gate to challenge it.** The premise ("everything is a plugin / six-thing core") is the bet; the Phase-1 core (`rat/2.0`) proves it's *buildable*, but not that it's *right*. Q02 is the last cheap moment to say "the premise is wrong because…".
- **Resolved a particular way — validate it.** The internal synthesis ([reviews/00](00-synthesis.md)) found that "everything is a plugin" has a blind spot for genuinely cross-cutting concerns (trace, auth, isolation, audit, observability, conformance) and put them in the **core's enforcement layer** rather than a plugin or a 7th core thing (C1–C10). Tell us if that resolution is sound.

## The architecture surface

| surface | the claim | your job |
|---|---|---|
| the premise | a data platform *is* a minimal control plane + plugins | sound, or a structural blind spot? |
| six-thing core | minimal **and** complete | is any of the six two things? anything mis-placed? |
| tier-0 (state-backend, deployment-runtime, bus) | "plugins, selected at boot, not hot-swappable" | bootstrap sound? is "plugin" honest? |
| the contract triple | `.proto` + `plugin.yaml` + `rat://` URIs | right surface + right unit of composition? |
| the frozen wire | additive-only within a major | which shape forces a v2? |
| capability model | provides/requires + negotiation; `declared==conformed` (D4) | algebra sound + complete? |
| cross-cutting layer (C1–C10) | core *enforcement layer*, not a plugin/7th thing | right home + clean layering? |
| reconciliation + data-plane split | K8s-for-data; bytes bypass the core | sound for *data*? split clean? |

## Architecture & contract questions (the heart)

**A — The premise.** Is "everything is a plugin; the core does six things" a sound organizing principle for a *data* platform — or does it have a structural blind spot we haven't named? Is there a load-bearing concern that is **neither** one of the six **nor** cleanly a plugin? (The synthesis found the cross-cutting set; did it find them all?)

**B — Core minimality + completeness.** Is the six-thing core minimal *and* complete? Is any of the six secretly two things — e.g., the "API gateway" does identity + capability authz (C5) + audit (C4) + deadline-bounding (C3) + routing; is that **one** thing? Is anything kept in the core that should be a plugin, or pushed to a plugin that should be core? (We keep a "temptation ledger" — currently 0 — to guard core-creep; is it catching the right things?)

**C — Tier-0 honesty.** state-backend + deployment-runtime + bus are "plugins selected at boot, not hot-swapped" ([reviews/01](01-adversarial-architect.md) Finding 6). Is the bootstrap/chicken-and-egg sound — how does the *first* deployment-runtime come up without being launched through a deployment-runtime? Is calling these "plugins" architecturally honest, or marketing over three hard dependencies — and does the distinction change how they must be built/operated?

**D — The contract triple + the unit of composition.** Is `.proto` + `plugin.yaml` + `rat://<axis>/v<major>/<capability>` the right surface — sufficient, not over/under-specified? Is the **capability** the right unit of composition (vs a coarser "service" or finer "method")? Is the URI grammar future-proof (community-added axes, capability evolution)? Is `requires` — the basis of C5 authz — expressive enough (no conditional/optional requires, no capability version *ranges*)?

**E — Frozen-wire regret (the expensive one).** Additive-only within a major; a break = a new major. **Which message/field/enum will we regret?** Scrutinize the load-bearing handoffs:
- `common/v1/data.proto` — `ArrowStream` (transport/role/ticket/`expected_rows`): complete for non-Flight transports + streaming-unknown-count? `WriteResult` (proto3-`optional` presence replacing a `-1` sentinel): right?
- `common/v1/context.proto` — `RequestContext` carried **in metadata**, not a field (ADR-007), with `reserved 1` on every message: sound, or fragile out-of-band coupling?
- catalog commit-linkage added **additively post-freeze** (ADR-010, `RegisterTable`/`CommitTable`): did the additive path leave a seam a clean design wouldn't have?
- the error model: `GetTable` has **no `found` field** by design (unknown table = `NOT_FOUND` error, not a bool) while `state.Get` *does* use `found` — is that distinction principled or inconsistent?

**F — Capability-model semantics.** Capability negotiation: registry wires by capability, never peer name; C5 = `caller.requires ∧ provider.provides`; D4 enforces `declared == conformed`. Is the **algebra** sound + complete? Provider selection when two plugins provide the same capability (the spike *refuses* the ambiguity — right, or is a selection policy needed)? Composition (does `requires X` transitively pull X's `requires`)? Granularity (format split `Write` into `Append`/`Merge`/`Overwrite` so authz is method-level — is the granularity right *everywhere*)?

**G — The cross-cutting enforcement layer.** The C1–C10 concerns (mandatory `traceparent`; plugin↔core auth; per-plugin state isolation; capability enforcement; mandatory audit *even with no audit-log plugin*; native observability) live in the core's **enforcement layer**, not a plugin. Is that the right home for *each*, or does any belong in a plugin or constitute a 7th core thing? Is the **layering** clean — e.g., the `AuditRecord` type lives in `common/v1`, not the `audit-log` axis, so the core's audit emission doesn't depend on an axis contract: right call, or a layering smell?

**H — Reconciliation + the control/data split.** The control plane is reconciliation (desired→actual, events-are-hints, optimistic concurrency); bulk data bypasses the core (RPCs carry refs, never bytes). Is reconciliation the right model for *data* orchestration specifically (vs generic workloads)? Is the control/data split clean, or does the bytes-bypass leak a responsibility the core should own?

## What the freeze process already settled (please don't re-derive)

ADR-003 forced **two divergent references per data-plane axis**, passing shared golden vectors, *before* any freeze; [reviews/06](06-proto-contract-review.md)–[08](08-post-freeze-board-review.md) found + fixed the per-axis freeze-blockers (the capability annotations, the `ArrowStream` transport/role pinning, the `Write`→`Append/Merge/Overwrite` split, the `rows_affected` sentinel, the error model). So the contracts aren't single-impl artifacts and the *obvious* blockers were caught — spend your time on the **premise** and the **subtle** regret, not on re-finding those.

## Already acknowledged (don't flag as novel)

C2 channel auth deferred (identity from the envelope, not yet the authenticated channel); the bus + a durable state-backend are frozen *contracts* with spike/partial core integration; audit-record signing deferred; the temptation ledger is the explicit core-creep guard. Known + booked.

## Materials & reading order (architect-relevant)

1. This brief + the architecture-surface table.
2. [reviews/01-adversarial-architect.md](01-adversarial-architect.md) + [reviews/06-proto-contract-review.md](06-proto-contract-review.md) + [07](07-freeze-review.md) + [08](08-post-freeze-board-review.md) + [reviews/00-synthesis.md](00-synthesis.md) (C1–C10) — the internal architecture/contract reviews (challenge them).
3. `docs/vision.md` (premise + anti-goals) → `docs/architecture/overview.md` (the six things, reconciliation, the data-plane bypass).
4. The decisions: [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md) (premise) · [002](../docs/architecture/adrs/002-founding-tech-stack.md) · [003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (two-refs) · [005](../docs/architecture/adrs/005-capability-invocation-model.md) (capability invocation) · [007](../docs/architecture/adrs/007-call-context-transport.md) (call-context transport) · [009](../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md) (freeze) · [010](../docs/architecture/adrs/010-catalog-commit-linkage.md) (additive commit-linkage) · [011](../docs/architecture/adrs/011-manifest-schema-freeze-and-per-kind-layer.md) (manifest).
5. The frozen wire itself: `contracts/proto/rat/common/v1/**` (the cross-cutting types — read these closely) + 2–3 axis protos + `contracts/schema/plugin.v1.json` (the manifest grammar).
6. The temptation ledger + decisions in `roadmap/done.md`.

## Findings & logistics

Same format + logistics as the [main brief](Q02-external-review-brief.md#how-to-deliver-findings): per-finding {severity · area · finding · why-it-matters · suggested-direction}, plus a bottom line — *is the foundation sound enough to bet 12–18 months on, and what's the one structural flaw or frozen-wire regret you'd fix before broad commitment?* A **Critical** = "I would not build on this foundation until resolved" (and if it's a wire issue found later, it's a `v2` break). A focused 1–2 day read is plenty; unpublished + confidential.
