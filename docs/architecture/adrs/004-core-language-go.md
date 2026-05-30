# ADR-004: Core language locked — Go

**Status:** Accepted
**Date:** 2026-05-30
**Deciders:** Tom, Claude (architecture session)
**Relationship to ADR-002:** ratifies & locks [ADR-002](002-founding-tech-stack.md) D1 (does not supersede it)

---

## Context

[ADR-002](002-founding-tech-stack.md) D1 already selected **Go** as the core language, with rationale. But the *prose* around the project never caught up to that decision and kept treating the language as an open fork:

- `CLAUDE.md` describes the core as "5-10k LOC core (**Rust or Go**)".
- The `.claude/settings.json` allowlist was deliberately populated with **both** the Go *and* the Cargo/Rust toolchains "until the language is locked" (see [done.md](../../../roadmap/done.md), 2026-05-30 allowlist entry).
- Casual references kept calling it "the Rust vs Go fork."

So there was a gap between the ADR (Go, decided) and the operational reality (treated as open). With the project moving from docs-only into code — where the plugin SDK, reference-plugin dev loop, and conformance harness all need a concrete host language — that gap has to close. **This ADR exists to ratify D1, lock it explicitly, and align the prose/tooling to it**, so no future session re-opens a decision that was already made.

It also records one thing D1 left implicit: *how firmly* is Go locked, and under what condition would we revisit?

## Decision

**The RAT v3 core is written in Go. This is locked.** CLAUDE.md prose and the toolchain allowlist are aligned to Go (Cargo/Rust entries removed from the allowlist).

**Go is the committed default, not an irreversible bet.** Per ADR-002 D1's explicitly-accepted meta-principle — *"AI-assisted rewriting lowers the cost of language migration; bias toward velocity-friendly language now and re-language later if needed"* — the door to revisit stays open as a **Phase-0 validation checkpoint**: we build the registry + reconciler core in Go, and only if that implementation surfaces a *fundamental, unforeseen blocker* do we reconsider. That reconsideration would require a superseding ADR. Absent such a blocker (the expected case), Go is final. This is the "presumed winner, validate-in-practice" posture, not a coin still in the air.

### Rationale (grounded in ADR-002 D1, which this locks)

1. **Ecosystem alignment.** Every distributed control plane RAT v3 models — etcd, NATS, K8s, Temporal, Crossplane — is Go. Building "K8s for data" in the language of K8s means the prior art is readable as source and the tier-0 dependency (NATS JetStream, ADR-002 D4) is first-class, not FFI'd.
2. **Mature gRPC tooling.** `grpc-go` is the reference implementation; `grpc-gateway` gives the REST projection (ADR-002 D2) natively.
3. **Faster MVP + larger contributor pool** aligned with the cloud-native ecosystem.
4. **Plugin SDK ergonomics.** No lifetimes/borrow-checker friction → a lower barrier for third-party plugin authors, which is the core bet of [ADR-001](001-everything-is-a-plugin.md).

## Consequences

### Positive
- The gap between ADR-002 D1 and the project's prose/tooling is closed; no future session re-litigates the language.
- The plugin SDK, reference-plugin dev loop, and conformance harness ([ADR-003](003-two-references-before-contract-freeze.md)) now have a concrete host language.
- Prior art (K8s controller-runtime, etcd, NATS) is source-readable in our own language.

### Negative / costs — accepted
- **GC pauses + ~10MB runtime overhead** (already noted in ADR-002 Consequences #1). Mitigation: tuning first; the re-language escape hatch (D1 meta-principle) is the backstop if a profiling pass ever demands no-GC. The core is a reconciler/event-router, not a data-plane hot loop, so this ceiling is comfortably high.
- **The SDK-ergonomics claim is now committed, not yet proven** — the first external plugin author is the real test (Phase-1 validation).
- **Rust's predictability/binary-size edge is forgone** for the core. Note this binds *only the core*: contracts are language-neutral (ADR-002 D2/D3), so a third-party engine/storage plugin authored in Rust remains first-class.

## Alternatives considered

### Rust
A genuine finalist (ADR-002 D1 "Rejected" column), not a strawman. Wins on GC-free predictability, binary size, raw performance. Rejected for the core because those advantages target a data-plane hot loop the core doesn't have, while its borrow-checker friction raises the SDK barrier exactly where we need it lowest, and the ecosystem we embed in (K8s/etcd/NATS) is Go-native. Rust stays welcome for *plugins* — the contracts don't care.

### Leave the language open through Phase 0
Rejected. This is what the prose was effectively doing, and it blocks the SDK, dev loop, and conformance harness from starting. D1 had already closed it; keeping the prose ambiguous only invited a re-litigation that wastes Phase-0 cycles.

### Run a Rust-vs-Go prototype spike to break the tie
Considered and rejected — and worth recording because an earlier draft of this ADR wrongly assumed such a spike was the agreed tiebreaker. **ADR-002 specifies no spike.** The tie was already broken in D1 on the rationale above. Building two throwaway prototypes to re-confirm a settled decision is process for its own sake. The *implementation itself* (Phase 0, in Go) is the validation checkpoint — see Decision.

## Related

- [ADR-002](002-founding-tech-stack.md) D1 — the original Go selection this ADR locks; D2 (gRPC → grpc-go), D4 (NATS, Go-native).
- [ADR-001](001-everything-is-a-plugin.md) — the plugin bet whose SDK ergonomics this serves.
- [ADR-003](003-two-references-before-contract-freeze.md) — conformance harness, now with a concrete host language.
- [roadmap/done.md](../../../roadmap/done.md) — the both-toolchains allowlist entry this decision narrows to Go.
