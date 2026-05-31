# 07 — Freeze review (sub-phase 0h): the `rat/1` go/no-go

> **Mandate.** Phase 0 sub-phase **0h** = the final adversarial pass over the now-complete
> contract + reference + conformance surface, then the decision to advance the data-plane
> axis contracts `v1-preview` → `v1` (tag `rat/1`). A `v1` tag is a no-breaking-changes
> commitment — this review is the last gate before that promise is made.
>
> **Method.** Three independent reviewers swept the surface from distinct angles
> (contract-coherence, security/enforcement, freeze-readiness/integration); every blocker
> they raised was then ground-truthed against the actual proto/vector/reference files before
> being accepted or downgraded here. Evidence base: `make conformance` = **20/20 PASS**,
> `make lint` + `make build` clean (2026-05-31).
>
> **Verdict (TL;DR): NO-GO for an *unconditional* freeze.** The structural contract is
> materially freeze-ready and the 15 prior blockers (reviews/06) are resolved — but this pass
> found **4 must-fix items** (wire-shape or un-retrofittable) and **4 should-fix items** (cheap
> spec-text), and the **ADR-003 cross-axis composition gate is not actually met** (only the
> per-axis gate is). Clear the punch-list + decide the cross-axis path, then freeze. Details below.

---

## Part A — prior freeze-blockers (reviews/06): RESOLVED ✅

The 15 freeze-blockers + the AUTH-2 open decision from [reviews/06](06-proto-contract-review.md)
are all addressed (remediation log: [roadmap/done.md](../roadmap/done.md), commits `322126c`→`84e8035`):

- **C-1 keystone** (forgeable `subject`) → three-principal `Identity` + signed `SubjectAssertion`
  ([context.proto](../contracts/proto/rat/common/v1/context.proto)).
- **C-4** linearizable CAS / ordered Watch → stated conformance obligation, gated by
  `state-v1.json` (verified: sqlite-py `BEGIN IMMEDIATE`).
- **C-5** capability⇄method binding → `(capability)` annotation + `core/v1/invoke.proto`.
- **C-6** missing invoke mechanism → `CapabilityInvokeService` (ADR-005) + streaming variants (ADR-008).
- **AUTH-3** "gRPC/Flight-style" unpinned → `ArrowTransport`/`ArrowStreamRole` pinned + a real
  Arrow Flight reference (parquet-py).
- **API-1d/SEC-8** secret anti-enumeration + `debug_redact`; **API-13** `-1` sentinel → proto3 `optional`.
- The structural tail (#1–#9 + #10a) is landed; the *additive/GA-safe* tail (#10b manifest
  artifact/digest, #9f doc-pins) is the only deferred remainder and is **post-freeze-safe** (purely additive).

The keystone fixes hold up under this pass. The findings below are **new** — things the
reviews/06 pass did not reach because the references + cross-cutting protos + CONTRACT.md docs
that surfaced them did not exist yet.

---

## Part B — the final adversarial pass: new findings

Graded by freeze impact. **MUST-FIX** = changes the frozen wire shape, or is un-retrofittable
once `rat/1` ships. **SHOULD-FIX** = cheap spec-text, fix now while it's free. **ACCEPTED
RESIDUAL** = real but a defensible design choice; document and move on.

### MUST-FIX before `rat/1`

**M1 — The error-model convention is referenced everywhere but pinned nowhere.**
[invoke.proto:99](../contracts/proto/rat/core/v1/invoke.proto) cites "the error-model convention
(reviews/06 C-5, freeze-blocker #9)" and every `CONTRACT.md` leans on specific gRPC codes
(`INVALID_ARGUMENT` / `NOT_FOUND` / `FAILED_PRECONDITION` / `PERMISSION_DENIED`) — but **no frozen
artifact defines which code means what across axes.** The mapping lives only as scattered prose +
`expect.code` in vectors. Post-freeze, two impls of one axis can return different codes for the same
failure and both "conform." *Fix:* a `common/v1` error-model doc (or a pinned comment block) that
enumerates the canonical code-per-failure-class, referenced by every axis. This is the
highest-leverage gap — it spans all six axes.

**M2 — "Resource absent" is modeled three contradictory ways, with no governing rule.**
`secret.Resolve` uses a `found` bool ([secret.proto:43](../contracts/proto/rat/secret/v1/secret.proto));
`state.Get` uses a `found` bool; `catalog.GetTable` uses **no** `found` field and relies on a
`NOT_FOUND` status ([catalog.proto:66](../contracts/proto/rat/catalog/v1/catalog.proto), `catalog-v1.json`).
The secret divergence is *principled* (anti-enumeration — collapse not-found and forbidden), and
state-read-miss vs catalog-table-missing are arguably different. But **the meta-rule is unwritten**,
so the inconsistency reads as incoherence and a future axis author has no guidance. *Fix:* fold the
rule into M1 ("missing resource → `found:bool` when absence is a normal control-flow outcome or a
security-sensitive enumeration concern; → `NOT_FOUND` status otherwise"), and make catalog's choice
deliberate. Once frozen, catalog cannot gain a `found` field without a wire break.

**M3 — Signatures carry no `key_id` / algorithm identifier → key rotation is painful.**
Both `AuditRecord.signature` ([audit.proto:60](../contracts/proto/rat/common/v1/audit.proto)) and
`SubjectAssertion.signature` ([context.proto:147](../contracts/proto/rat/common/v1/context.proto)) are
bare `bytes`; Ed25519 is pinned in prose only, and no `key_id` identifies *which* core key signed.
A new field is technically additive, but without it in `v1` a verifier facing a rotated key cannot
tell which key to use for historical records, and algorithm agility is impossible. Adding a `key_id`
(and an `alg` enum) now is cheap and makes rotation a non-event. *Fix:* add `key_id` to both signed
envelopes pre-freeze. (The hash-chain canonical-serialization spec must then explicitly include the
new field for future records — additive, safe.)

**M4 — The `SubjectAssertion` verification contract omits the bare-mirror cross-check.**
The signature covers `(principal, tenant, bound_correlation_id, expires_unix_ms)`
([context.proto:144](../contracts/proto/rat/common/v1/context.proto)) — so `tenant` **is** signed,
correcting the reviewer's "tenant unsigned" framing. But the VERIFICATION CONTRACT (steps 1–3) never
mandates that the **bare `Identity.tenant` mirror equals the signed tenant**, nor that
`Identity.subject.principal` equals the signed principal. A consuming hop that reads the convenient
bare `Identity.tenant` (as the stub gateway does, [gateway_test.go:104](../examples/state/inmemory-go/gateway_test.go))
instead of the verified value trusts an unchecked field. *Fix:* add step 4 to the verification
contract — "the bare `Identity.tenant`/`subject.principal` mirrors MUST equal the
signature-covered values or the request is rejected" — and state explicitly that `caller_plugin`
(re-derived per hop, single-hop trust) and `tenant` rest on **authenticated transport (C2)**; without
mTLS/token on the core→plugin channel they are forgeable. This is spec-text on a frozen type, so do
it now.

### SHOULD-FIX (cheap, do now while free)

**S1 — `engine-v1.json` mandates `snapshot_id_set:true` on `CREATE TABLE`, but `WriteResult.snapshot_id`
is documented "if the format is versioned (else empty)"** ([data.proto:79](../contracts/proto/rat/common/v1/data.proto)
vs [engine-v1.json:9](../contracts/conformance/engine-v1.json)). The comment is too format-centric — an
engine writing through a versioned table legitimately returns a version id. *Fix:* reword the
`snapshot_id` comment to "resulting version id of the written table state, empty if none," so the
golden vector and the type agree before the vector becomes immutable.

**S2 — `InvokeBidiStream`: a non-empty `capability` on a non-first frame is "ignored," not "rejected."**
[invoke.proto:131](../contracts/proto/rat/core/v1/invoke.proto) says the core "ignores it after open."
"Ignore" leaves a conformant relay free to silently tolerate a client trying to switch capability
mid-stream. *Fix:* pin "non-empty `capability` on a non-first frame → stream aborted with
`INVALID_ARGUMENT`." Comment-only; the wire shape is unaffected.

**S3 — Audit-on-deny is intended but not pinned as a conformance obligation; the reference omits it.**
`AUDIT_OUTCOME_DENIED` exists ([audit.proto:26](../contracts/proto/rat/common/v1/audit.proto)) and
the coverage doc says audit covers "auth decisions," but the stub gateway appends to its audit log
*after* the deny/traceparent early-returns ([gateway_test.go:94–116](../examples/state/inmemory-go/gateway_test.go)),
so denied calls produce no record. The wire supports it; the **obligation** isn't stated. *Fix:*
make "every enforcement decision — including DENIED — emits exactly one audit record" an explicit C8
conformance obligation (and a future gateway-conformance vector). Wire-safe; do now.

**S4 — `runtime-v1.json` carries a now-false claim in frozen golden data.** Its `_comment` says the
gateway "`Invoke` is unary-only and cannot mediate a server-streaming capability" — but
`InvokeServerStream` shipped (ADR-008). *Fix:* correct the comment so the golden file doesn't mislead
every future reader.

### ACCEPTED RESIDUAL (document, don't block)

- **R1 — `SubjectAssertion` is bound to the *operation* (`correlation_id`), not the *hop/capability*.**
  Within one operation any plugin holding the assertion can present it to any capability *it already
  `requires`* under the user's authority — a bounded confused-deputy. It's bounded precisely because
  C5 only lets a plugin call capabilities its manifest declares, so the blast radius is the operation's
  declared capability set, not "anything." Tightening to per-hop binding is a larger design change;
  **accept for `v1`, document as a known property**, revisit if a capability needs user-presence proof
  finer than per-operation.
- **R2 — Storage `VendCredentials` tenant-scoping is honour-system inside the storage plugin.** The
  core can't inspect an opaque STS blob (ADR-005's one acknowledged direct-dial bearer exception), so
  C7 for the bytes leg reduces to a per-impl property the conformance vectors test via a stand-in
  "scope receipt." Inherent to the exception; documented in storage `CONTRACT.md`. Accept.
- **R3 — Additive niceties** surfaced (watch `caught-up` bookmark, `Event.schema_version`, `ArrowStream`
  termination signal, `MergeBranchResponse` no-op-vs-replay disambiguation, `TableRef.branch` vs the
  per-RPC `branch` precedence). All are **additive post-freeze** (new fields/values) → backlog, not
  freeze-gating.

---

## Part C — ADR-003 gate status: per-axis MET, cross-axis NOT met

[ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) is binding: no
data-plane contract freezes without two independent references that pass conformance **and are run
against each other on golden data**, where "run against each other" is defined to include a
**cross-combination** (Engine A + Format B + Catalog A + Storage A, substituting one axis impl at a time).

| Requirement | Status |
|---|---|
| Two independent references per data-plane axis | ✅ all 6 (two language paths + a divergent real backend) |
| Each passes the axis's shared golden vectors | ✅ `make conformance` 20/20 |
| Real, technologically-divergent second impl (round 2) | ✅ sqlite, local-fs, subprocess, DuckDB+DataFusion, Parquet+Delta, pyarrow.flight |
| **Cross-axis composition on golden data** (the ADR-003 substitution matrix) | ❌ **not done** — conformance is per-axis only |
| Strategy axis references | ❌ **zero** — the axis that *composes* engine+format+catalog+storage has no reference at all |

**This is the real freeze-readiness gap.** Per-axis the contracts are thoroughly validated, and the
cross-axis *coupling types* (`TableRef`, `ArrowStream`) are partly exercised (parquet-py produces real
Arrow Flight; duckdb-py consumes real Arrow) — so the risk a composition test uncovers a contract flaw
is **low**. But "low risk" is not "ADR-003 satisfied." A strict reading blocks the freeze until a
composed pipeline (strategy → engine → format → catalog → storage, substituting one impl per axis) runs
on golden data — which also requires the first strategy-axis reference.

---

## Decision

**Do not tag `rat/1` yet.** Tagging now would either (a) violate ADR-003's cross-axis clause, or
(b) freeze wire shapes (M1–M4) we've identified as regret-prone — both contradicting the project's
"honest tradeoff documentation" principle. The contract is *close* — this is a punch-list, not a redesign.

**Recommended path to freeze:**
1. **0h-remediation** — clear M1–M4 (wire/spec) + S1–S4 (cheap text). Mostly additive or comment-only;
   one focused commit-cluster, each `buf`-clean. Record R1–R3 as accepted residuals in backlog.
2. **Cross-axis gate** — choose:
   - **(a) strict ADR-003** — build the first strategy reference + a cross-axis composition test on
     golden data, *then* freeze. Highest rigor; most work.
   - **(b) conditional freeze** — tag `rat/1` after step 1, recording the cross-axis composition +
     strategy reference as the single documented residual gate (risk assessed low, tracked to closure).
     Faster; accepts a small, named risk.
3. **Tag** — advance the six data-plane axes `v1-preview` → `v1`; update each `CONTRACT.md` status line +
   the proto `Status:` comments; record the freeze (ADR + roadmap).

The cross-axis choice is a genuine fork (rigor vs. velocity) and is the user's call.

## Related

- [reviews/06](06-proto-contract-review.md) — the prior pass (15 blockers, now resolved)
- [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) — the freeze gate
- [cross-cutting-coverage.md](../docs/architecture/cross-cutting-coverage.md) — the 0c coverage audit
- [roadmap/current.md](../roadmap/current.md) — freeze status
