# Context-carriage conformance (PU-2, ADR-017)

The **keystone** context-carriage contract — `common/v1/context.proto` + the ADR-007
gateway-stamping rules — is the carrier for C1/C2/C3/C5/C7/C8 and the **most-irreversible
frozen surface** (a mistake here is a `v2` on the universal envelope). The Q02 dry-run
([architect F1](../../../reviews/11-q02-architect.md), maintainer-conceded) found it had the
**weakest** conformance of the whole freeze: the ADR-003 two-reference rule was applied to
the data axes it was bundled into (ADR-009) but **not** to the envelope itself — it was
exercised by exactly one implementation (the spike Go gateway).

**PU-2 closes that gap.** This suite applies the [ADR-003](../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)
forcing function to the keystone: **two technologically-divergent reference implementations
cross-run shared golden vectors.** Agreement on every vector is the signal that the contract
is unambiguous and implementable from the prose alone — not just self-consistent in one impl.

## What's here

| file | role |
|---|---|
| [`context-carriage-v1.json`](context-carriage-v1.json) | the portable golden vectors (12 cases) |
| [`go/`](go/) | reference impl #1 — **Go**, clean-room, stdlib only |
| [`py/`](py/) | reference impl #2 — **Python**, technologically divergent (`cryptography` for ed25519) |

Both are clean-room references of the contract in `context.proto`'s prose; neither shares
code with the other or with `core/gateway`. Run them with **`make context-carriage`** (or
[`scripts/context-carriage.sh`](../../../scripts/context-carriage.sh)) — both must pass.

## The contract under test (per hop)

1. **C1** — `traceparent` present + well-formed and `correlation_id` present, else **reject**.
2. **Re-stamp** — `trace` propagated **verbatim**; `identity.caller_plugin` **re-derived** from
   this hop's authenticated channel and **never** propagated from the inbound envelope (the C3
   cross-plugin-namespace guarantee); `tenant` server-stamped (propagated); deadline propagated.
3. **Subject** (if present) — verify the core's ed25519 `SubjectAssertion`: `bound_correlation_id`
   == inbound `correlation_id` (anti-stockpile); `now <= expires`; and the signature, reconstructed
   **from the bare mirrors** (`SubjectAssertion.principal` + `Identity.tenant`) — which is the **M4**
   cross-check (a bare value the signature doesn't cover fails verification).

## Scope

The suite conforms the stamping **logic** — re-stamp-vs-propagate, caller re-derivation, the
M4 bare-mirror cross-check, and subject verification (the architect's named failure modes).
The proto **wire-bytes** (`rat-callmeta-bin` canonicalization) are owned by the shared,
now-connectionless codegen ([ADR-018](../../../docs/architecture/adrs/018-connectionless-codegen-local-plugins.md))
— identical across languages by construction — so the references work on the logical fields.

> Status: this is the PU-2 deliverable of [ADR-017](../../../docs/architecture/adrs/017-pre-unfreeze-contract-amendment-gate.md)
> — required before the freeze leaves local/unpushed.
