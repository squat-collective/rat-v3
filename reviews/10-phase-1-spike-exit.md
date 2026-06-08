# Reviews 10 — Phase-1 spike exit report (contract-de-risking)

**Date:** 2026-06-01
**Mandate ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md)):** before committing the full ~3-month / 12–15k-LOC core build, stand up a minimal *real* core (registry + capability enforcer), drive the real pipeline through it, and try to break a frozen contract **while the freeze is still local** — to test whether the engineering risk the board flagged ([reviews/09](09-phase-1-gate-review.md): "green certifies *shapes*, not *obligations*") is real, and to feed Tom's deferred 12–18mo commitment-gate decision.

## What the spike built (all `go test`-green, containerized `golang:1.25`)

`core/` — a new Go module ([ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md)); two of the six things + the cross-cutting probes:

- **`manifest`** — loads the frozen `plugin.v1.json` shape; validates the capability-URI grammar.
- **`registry`** — indexes manifests; `Authorize(caller, cap)` = `caller.requires ∧ provider.provides` — the C5 decision *derived from declared manifests*.
- **`gateway`** — the `core/v1 CapabilityInvokeService` (`Invoke` + `InvokeServerStream`): enforces that C5 decision per call, **audits every decision (C4)**, **bounds the provider call by `min(channel, deadline_unix_ms)` (C3)**, relays opaque frames, re-stamps identity + propagates traceparent (ADR-007).
- **`composition`** — the real pipeline (catalog `get-table` → format `overwrite` → catalog `commit-table`) driven through the gateway, a manifest per plugin.
- **`arrowticket`** — a reference TTL / single-use / bound Arrow ticket (D2).
- **CI:** `make core-test` (also folded into `verify`) + `make breaking` (buf-breaking vs the sealed `main`).

## Findings — did the frozen wire hold? **Yes.**

| Probe | Result | Verdict |
|---|---|---|
| **C5** capability enforcement | Allowed only when declared; undeclared cap, unknown caller, and a mid-pipeline `merge` all `PERMISSION_DENIED`; every decision audited. Enforced from real manifests — not a stub allowlist. | ✅ wire sufficient |
| **C1** crash-mid-strategy | A strategy that crashes after the write, before commit, recovers on an at-least-once re-run: the replayed `overwrite` is a no-op (`already_applied`) → no double-write, exactly-once commit. | ✅ existing fields suffice; **no commit/abort shape needed** |
| **C3** provider deadline | The gateway bounds a hung provider by the soft deadline (`deadline_unix_ms`); a 2s-slow provider returns `DeadlineExceeded` in ~150ms. | ✅ wire field sufficient |
| **D2** ArrowStream ticket | `bytes ticket` carries an HMAC-signed, TTL'd, single-use, `{stream,caller,tenant}`-bound credential; replay / expiry / cross-binding / tamper all rejected. | ✅ field sufficient |
| **buf breaking** vs `main` | No breaking changes — the spike added `core/` + docs only; the frozen contracts are untouched. | ✅ |

**No freeze-reopen was triggered.** The board's worry — that a real enforcer would reveal a wire-shape gap expensive to fix post-publish — did not materialize on the exercised surface. The hardest case probed (crash between a write and its commit) is recoverable with the existing `idempotency_key`/`already_applied` model (ADR-012); multi-output all-or-nothing atomicity is the catalog **branch+merge** primitive's job (CreateBranch + MergeBranch), not a strategy-level gap.

## Honest scope — what the spike did NOT prove

The spike validated the **enforcement spine + crash-safety** against a Go-fake provider surface. It deliberately did *not* cover the following — these are the FULL Phase-1 build, **not** freeze risks:

- **D1** real process isolation (a podman deployment-runtime, not in-process providers).
- **D3** storage-cred scoping; **D4** conformance-attestation *enforcement*; **C4** terminal stream-close audit record.
- **sre#4** reconciler crash-loop backoff / jitter / lease-thrash.
- Real-backend equivalence (DuckDB/Parquet) *through the Go gateway* — already proven for the wire shapes by the Python `plugins/composition`; not re-run here.
- A streaming idle-timeout for a hung provider with **no** deadline set (the deadline-bound covers the deadline-set case; an idle-timeout backstop is an implementation follow-on, not a wire question).

## Recommendation (for Tom's commitment-gate decision — ADR-013)

**Engineering risk is materially reduced.** The contracts the board flagged as "shapes, not obligations" now have a *real enforcer* exercising the load-bearing ones (C5, C1, C3, D2) — all green, no wire regret, freeze intact (buf-breaking clean). The spike answered its question: **the frozen wire is buildable-against; proceeding to the full core does not rest on an unproven contract.**

**The strategic call remains yours**, unchanged by the spike (it was never an engineering question): the 12–18mo runway + GTM commitment, and the v2-vs-v3 opportunity cost (ADR-013 **Q01**) + external review (**Q02**). The spike bought evidence that the *contracts* are sound; it does not answer whether a from-scratch v3 is a better bet than evolving the shipping v2.

Three coherent next moves:
1. **Commit** to the full core build (the enforcement spine + C5/C1/C3/D2 are real; extend to D1/D3/D4/C4/sre#4).
2. **Continue exploratory**, re-evaluating at the next milestone (e.g. D1 real-isolation).
3. **Redirect** to v2 if the strategic v2-vs-v3 answer doesn't favour v3.

(1) and (2) are both well-supported by the spike; (3) is not indicated by any technical finding.

## Related

- [ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) — the spike mandate + the deferred gate · [ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md) — the spike-core shape.
- [reviews/09](09-phase-1-gate-review.md) — the gate review · [reviews/08](08-post-freeze-board-review.md) — the findings the spike de-risked.
- `core/` — the code · [roadmap/done.md](../roadmap/done.md) — the increments.
