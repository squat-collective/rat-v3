# 08 — Post-freeze board review (5-agent adversarial team)

> **What this is.** The first adversarial review *after* the `rat/1`→`rat/1.4` freeze (18
> axes, 32 references, the cross-axis composition). Five specialist agents — `architect`,
> `security`, `ecosystem`, `sre`, `contracts` — reviewed in parallel as a team, **cross-
> consulting each other via direct messages** (the consults are noted inline; they changed
> several findings). Each wrote a full report under [`reviews/board/`](board/); this is the
> lead's synthesis. Severity/class tags: **[V2-REGRET]** = frozen wire flaw, needs a `v2`;
> **[ADDITIVE]** = real gap, closable with a new field/RPC/value; **[PROCESS]** = doc /
> conformance / enforcement, no wire change.
>
> Individual reports: [architect](board/architect.md) · [security](board/security.md) ·
> [ecosystem](board/ecosystem.md) · [sre](board/sre.md) · [contracts](board/contracts.md).

## Verdict

**The frozen wire is sound; the freeze *badge* over-promises.** Two findings the whole
board converged on, from opposite directions:

1. **The contracts froze at a good shape.** `contracts` (the proto oracle) found exactly
   **one true [V2-REGRET]** across all 18 axes, and it's medium-severity. Every other
   scary-sounding gap (engine type divergence, ArrowStream termination, catalog
   commit-linkage) is **additive** — the freeze left room. `architect` independently
   confirmed the six-thing-core discipline genuinely held and the strategy axis is the
   clean capability showcase as advertised. For a contract frozen before its core exists,
   that is a real achievement of the two-reference + composition gate.

2. **…but "all 18 axes frozen + `make conformance` 32/32" promises more than is true.**
   `security`, `ecosystem`, and `sre` *independently* landed on the same spine: the
   load-bearing layer — enforcement, crash-safety, and the **core itself** — is deferred or
   unbuilt, and several gaps are *masked* by the green badge. A plugin set can be 32/32-
   conformant and (security) enforce none of the three bytes-path trust boundaries,
   (ecosystem) lie in its manifest/marketplace listing with nothing to catch it, and (sre)
   silently corrupt data on a mid-pipeline crash. The contracts are clean; the *guarantee*
   they're wrapped in is not yet real.

These aren't in tension — they're different layers. **The wire is right. The system around
it is paper.** That's expected (the core is Phase 1) — the problem is that the frozen
artifacts and the conformance badge don't *say* so.

## The window: the freeze is still local

The `rat/1`→`rat/1.4` tags are **local and unpushed** — no external consumer has pinned to
them yet. That makes the single highest-value action available *only now*:

> **Absorb the one V2-regret (and the cheap additive crash-safety fields) into `rat/1`
> before publishing, instead of carrying them to a `v2`.** After the first external
> integration, finding #1 below is permanent.

---

## Findings by cluster

### A. The one true V2-regret — fix it while the freeze is local

**A1. `WriteResult.snapshot_id` keeps the empty-sentinel bug that its sibling was fixed to remove.** `[V2-REGRET]` · MED · `contracts` #1, evidence `common/v1/data.proto:74-84`.
The freeze gave `rows_affected` `optional` to split *absent=unknown* / *0=zero* / *n* (the API-13 fix) — but left the adjacent `string snapshot_id` as a bare sentinel that conflates "no version (unversioned format)" vs "cannot report a version." Under `buf.yaml breaking: use: FILE`, `string`→`optional string` is breaking, so presence can **never** be added in `v1`. **Recommendation:** because the freeze is still local, just make it `optional` now and re-cut `rat/1`. (If you'd rather not re-cut: `contracts`' conservative path is to accept the wart and add a *new* `optional string resulting_version = 3` later — additive — but the clean fix is free today.)

### B. The headline-feature hole — additive, but the pipeline can't close its loop on the wire

**B1. `CatalogService` has no `CreateTable` / commit-linkage RPC.** `[ADDITIVE]` · **HIGH** · `contracts` #2 + `architect` #2 (cross-confirmed), evidence `catalog/v1/catalog.proto:40-57,27-30`.
The catalog is `GetTable`/`CreateBranch`/`MergeBranch` only. A strategy can branch and merge but **cannot register a new output table, nor tell the catalog which snapshot `format.Write` produced on a branch**. The branch-isolated-pipeline *headline feature* therefore cannot complete its own create→write→**register**→merge loop on the frozen surface — `composition-v1.json` only passes because the harness fakes the linkage out-of-band. Adding an RPC is backward-compatible, so it's not a wire break — but "additive, GA-deferred" understates that **the catalog ships functionally incomplete for its stated purpose.** **Recommendation (both agents):** make commit-linkage + `RegisterTable` the **first** post-freeze additive (`v1.1`), not GA-distant; document *now* that write→register is admin/out-of-band.

> **Cross-consult (`architect`→`sre`):** `architect` amplified that this same seam carries the
> reconciler's *atomicity* story. `MergeBranch` is crash-safe (`idempotency_key` +
> `expected_into_snapshot` CAS, catalog.proto:92-100) — but the *write* leg has no
> idempotency key, so convergence under at-least-once rests **entirely on the branch-isolation
> convention**, and `strategy.Apply`'s target `TableRef.branch` is *optional* (data.proto:23) —
> the frozen contract **does not mandate** run-branch isolation. A strategy that writes
> straight to `main` has no atomicity; a crash leaves a visible half-write.

### C. Run-lifecycle is not crash-safe (sre's spine, amplified by security + architect)

All additive-to-fix, but every `v1` consumer frozen *now* is written against contracts that
can't express recovery:

- **C1. At-least-once scheduler + no effect-leg idempotency key = silent double-apply.** `[ADDITIVE]` · **HIGH** · `sre` #1, evidence `scheduler.proto:29-36` vs `data.proto:74` / `harness.py:197`. Dedup exists at the reconciler, never at the write. A duplicate fire → an `append` strategy writes twice. **Fix:** add `idempotency_key`/`run_id` to the strategy invoke + `WriteResult`; make "writes MUST be idempotent under a repeated key" a conformance obligation.
- **C2. `ArrowStream` has no termination/completeness signal.** `[ADDITIVE]` · MED-HIGH · `sre` #2 + `contracts` #5 (independent), evidence `data.proto:53-71` (R3). A producer that dies mid-transfer closes the stream the same way a clean finish does → `format.Append` commits a **partial** dataset and returns a complete-looking `rows_affected`. Silent truncation — "no error, wrong data." **Fix:** additive `expected_rows`/`expected_batches` (+ a "broken stream → consumer MUST fail the write" vector).
- **C3. The core-mediated hop enforces no deadline on the provider call.** `[PROCESS]` · **HIGH** · `sre` #3, evidence `context.proto:62-64` (`deadline_unix_ms` is a soft hint) + `gateway.go:124-151` (`InvokeServerStream` sets no inner-stream deadline). A hung provider blocks `RecvMsg` forever and, at scale, exhausts the single mediation point. **Fix (enforcement-layer, no wire change):** spec "core MUST apply `min(channel, deadline_unix_ms)` and abort `DEADLINE_EXCEEDED`" + a streaming idle-timeout.
- **C4. No terminal audit record.** `[ADDITIVE]` · MED · `security` #8 — **raised by `sre`'s audit-on-crash consult**. Streams audit "at open" (invoke.proto:53-55); a stream that dies mid-relay leaves only a "started/allowed" record, no terminal outcome — incident reconstruction is impossible. `AUDIT_OUTCOME_ERROR` already exists, so it's additive. **Fix:** pin "streams additionally emit a terminal close record with `outcome ∈ {SUCCESS,ERROR,DENIED}`."
- **C5. The composition proves only the happy path.** `[PROCESS]` · MED-HIGH · `sre` #5. `harness.py:193-204` is a bare sequential chain with no recovery; nothing exercises mid-run death. **Fix:** add a crash-mid-strategy case (kill a provider between stages; assert the target is fully-old or fully-new, never partial) — if the contract can't guarantee that, the strategy axis needs a commit/abort shape, which is *harder* to add post-freeze than a field.

### D. The conformance/enforcement honesty gap (security + ecosystem converge)

The badge says "conformant"; these three trust boundaries are honor-system and untested:

- **D1. I9 isolation is a gate + self-asserted attestation, not verified enforcement.** `[PROCESS]`/`[ADDITIVE]` · **HIGH** · `security` #1, evidence `local-process-py/store.py:32-41,69` + `deploymentruntime-v1.json`. `local-process-py` reports `read_only_root_fs:true` "honored" while `Popen`-ing a bare subprocess that enforces nothing, and the vector only checks **3 of 5** profile bools. A runtime can pass 20/20 and isolate nothing — the exact trust boundary the "install 3rd-party plugins" bet leans on. **Fix:** full-profile vector + a *real* enforcing runtime (podman, not dry-run) + an additive structured `IsolationAttestation` (don't stuff the receipt in a free-form `detail` string).
- **D2. The `ArrowStream` ticket is the sole gate on the core-bypassing bytes leg, and its security is prose-only + unconformanced.** `[ADDITIVE]`/`[PROCESS]` · **HIGH** · `security` #2 — **confirmed with `contracts`** (TTL/single-use/binding pinned nowhere in the wire or vectors). Two impls can be 32/32-conformant while issuing guessable, non-expiring, replayable tickets → cross-tenant bulk-read with the core out of the loop. The opaque `bytes` *shape* is fine to freeze (not a V2-regret); the *guarantee* isn't real. **Fix:** a Flight conformance vector asserting TTL expiry + single-use + cross-tenant rejection.
- **D3. Storage `VendCredentials` scoping (R2) is tested against a JSON stand-in, not the real cred.** `[ADDITIVE]` · MED · `security` #5. A plugin can mint over-broad real creds and still pass, because the harness sees only the receipt it chose to emit. **This is the *second* honor-system trust point on the same bytes path** (with D2) — together the bulk plane's cross-tenant isolation is, at v1, entirely impl-asserted. **Fix:** an integration vector that vends a *real* scoped cred against local-fs/minio and proves an out-of-prefix read is refused.
- **D4. "declared == conformed" has no enforcer.** `[PROCESS]` · **HIGH** · `ecosystem` #1, evidence `marketplace/community-py/store.py:45-49` hardcodes `conformed=provided`; zero linkage between the harness PASS and the manifest/listing. A plugin claims any capability in its manifest and marketplace listing and nothing catches it — because the component meant to check (the core) doesn't exist. **Fix:** a signed **conformance attestation** (axis + vector-hash + result + signer) the marketplace verifies; `conformed_capabilities` *derived from* it, not free text.
- **D5. Frozen artifacts describe core enforcement in the present tense; the core doesn't exist.** `[PROCESS]` · **HIGH** · `ecosystem` #2, evidence `plugin.v1.json:88,98` ("a CHECKED gate", "what the gateway enforces") + `state/inmemory-go/gateway_test.go:1` (`// THROWAWAY STUB`). An outside author cannot tell **designed** from **working**. **Fix (cheapest high-value item in the review):** a one-line status banner on `plugin.v1.json` + every `CONTRACT.md` — "Enforcement described here is the contract the core MUST implement; the core is not yet built (Phase 1)."

### E. Process / docs (cheap, do soon)

- **E1.** Only **6 of 18** axes have a `CONTRACT.md`; no `rat plugin validate` CLI (INVALID-examples is aspirational MD). The other 12 axis authors are on their own. `ecosystem` #4.
- **E2.** Manifest schema is the **only** unfrozen artifact **and** the only thing an author hand-writes, **and** per-kind schemas don't exist — an author can't finalize or fully validate a manifest. `ecosystem` #3. **Fix:** freeze `plugin.v1.json` + ship the 18 per-kind schemas in one stroke (the protos are frozen, so required-capability sets are derivable).
- **E3.** Round-1 reference toys encode stand-ins (in-proc Arrow, ignored `QueryRequest.tables`) a newcomer copies wrongly — `composition/README.md:80-84` is the smoking gun. `ecosystem` #5. **Fix:** label them `WIRE-CONTRACT ONLY — NOT A STARTER TEMPLATE`; point authors at the real ref.
- **E4.** `overview.md` drift: it commands a **non-existent `plane-manager-plugin`** (rename `deployment-runtime`) which also dents the "core never tells anyone to do anything" thesis; tier-0 (state/deployment-runtime/bus aren't hot-swappable) is honest in the rules file but **not** in the front-door doc. `architect` #4, #5.
- **E5.** `ERROR_MODEL.md` omits `CANCELLED`/`ABORTED` — but the ADR-008 streaming variants surface them on client-cancel, so a conformant gateway propagating `CANCELLED` is literally non-conformant per the closed-set rule. Also `TableRef.branch` vs per-RPC `branch` precedence is undefined, and BidiStream's **empty-first-frame** is unpinned (the symmetric twin of the S2 fix). `contracts` #7, #8. All spec-only.
- **E6.** Engine output-type stability is the *user's* responsibility in v1 (the SUM→CAST divergence) — state it honestly; don't let "swap anything, code unchanged" imply user SQL is portable. `architect` #3 + `contracts` #3.
- **E7.** The mandated **temptation counter** (CLAUDE.md #2) is not actually kept — discipline is observed ad hoc, not measured. `architect` #6. Add the ledger even at count 0.
- **E8.** mTLS trust-root is prose-only and **structurally inexpressible in proto3** — make C2 a named *deployment-conformance* item ("multi-tenant requires mutual auth; core refuses multi-tenant on an unauthenticated transport"). `security` #3 (sharpened with `architect`). Also: the audit **keyring trust-root / distribution / revocation** is unspecified though the entire audit + assertion trust collapses to it (`security` #4); and the audit chain stops forge/reorder but **tail-drop** detection depends on the core-local copy + watermark, not the sink (state it).

### F. Accept as documented v1 residual

- **F1.** `SubjectAssertion` confused-deputy (R1) — bounded by C5 `requires`; tightening needs an additive `bound_capability` field; safe to defer. `security` #6 — which also **confirmed the M4 tenant cross-check is genuinely fixed** (the "tenant unsigned" framing from earlier passes is closed).
- **F2.** `WriteResult` conflates insert/update/delete for a real MERGE; `TableRef` collapses branch/snapshot/as-of into one string — both enrichable additively when a real backend needs it. `contracts` #4, #6.
- **F3.** Secret anti-enumeration is airtight at the response layer but silent on timing side-channels — cheap additive doc note. `security` #7.

---

## Cross-agent dynamics (the team part)

The agents genuinely changed each other's findings via direct messages:

- **`sre` → `security`** ("is an audit record guaranteed when a plugin crashes mid-call?") produced **C4** (no terminal audit record) — a finding neither would have filed alone; it's a security *and* ops issue.
- **`architect` → `sre`** corrected a draft `sre` claim that "the health contract doesn't exist": `deployment-runtime.Healthcheck` **does** exist (instance liveness). The *precise* residual survives: there's no **plugin-level readiness/health probe the reconciler drives for pipeline health** — instance-alive ≠ semantically-ready. (Recorded accurately in `sre` #4.)
- **`security` ↔ `contracts`** confirmed the `ArrowStream` ticket's TTL/single-use/binding is pinned **nowhere** in the wire or vectors — turning a vague worry into the concrete D2.
- **`ecosystem` → `contracts`** surfaced the untyped `options` bytes-bag (D-adjacent, `ecosystem` #8) as "the wart not visible in the proto text."
- **The headline convergence** — `architect`, `contracts`, and `sre` independently nominated the **catalog commit-linkage** (B1) as their biggest or near-biggest concern, from three different lenses (coherence, wire, atomicity). When three specialists who didn't coordinate their *conclusions* land on the same seam, that's the strongest signal in the review.

The one real **disagreement** — `architect`'s "regret-light, discipline held" vs `ecosystem`/`security`'s "the badge over-promises" — resolves cleanly: they're grading **different layers**. The contracts are clean (architect/contracts); the trust + enforcement + crash-safety layer wrapped around them is unbuilt (ecosystem/security/sre). Both are true, and stating both is the honest picture.

---

## Prioritized actions

**Now — while the freeze is local (pre-publish window):**
1. **A1** — make `WriteResult.snapshot_id` `optional`; re-cut `rat/1`. *The only thing that's free now and impossible after publish.*
2. **D5 / E4 / E1** — the honesty pass: status banner ("core not built; this is the contract it MUST implement") on `plugin.v1.json` + every `CONTRACT.md`; fix the `plane-manager`→`deployment-runtime` + tier-0 drift in `overview.md`. Pure docs; removes the most misleading promises.
3. Optionally absorb the cheap additive crash-safety fields into the re-cut (**C1** idempotency_key, **C2** ArrowStream completeness) while the surface is fresh.

**`v1.1` additive (no break, prioritized):**
4. **B1** catalog `RegisterTable` + commit-linkage RPC — *first*, the headline feature needs it.
5. **C1/C2/C4** idempotency key · ArrowStream completeness/terminator · terminal audit record.
6. **E2** freeze the manifest schema + ship the 18 per-kind schemas.
7. Enrichments: `IsolationAttestation` (D1), conformance-attestation message (D4), `health/v1` probe (sre #4), `WriteResult` merge breakdown + `TableRef` snapshot/as-of (F2), `bound_capability` (F1).

**Enforcement-layer / conformance (some need the core; specify now):**
8. **C3** provider-call deadline + streaming idle-timeout. **D1/D2/D3** full-isolation vector + real enforcing runtime, ArrowStream-ticket vector, real-cred storage vector. **D4** attestation enforcement. **sre #4** crash-loop backoff. **sre #8** pin a core SLI list + `/metrics` contract while the core is still paper.

**Process / spec (cheap):**
9. **E3** label round-1 refs as non-templates. **E5** ERROR_MODEL `CANCELLED`/`ABORTED` + `branch` precedence + bidi empty-first-frame. **E6** engine output-type honesty. **E7** temptation ledger. **E8** mTLS deployment-conformance + audit keyring trust-root doc. **F3** secret timing note.

## Bottom line

The freeze did its job: **one** medium V2-regret across 18 axes is an excellent result, the
core discipline held, and the strategy axis is the real showcase. The board's collective
warning is not about the wire — it's about the **gap between the frozen badge and the
unbuilt reality**. Two moves capture most of the value: **fix `snapshot_id` and re-cut
`rat/1` now** (the closing window), and **stop describing the unbuilt core's enforcement in
the present tense** (the honesty banner). Then build the core (Phase 1) — where C1–C5 and
D1–D5 stop being review findings and become acceptance tests.

## Related
- [reviews/00-synthesis.md](00-synthesis.md) — the pre-build 5-perspective review this board echoes.
- [reviews/07-freeze-review.md](07-freeze-review.md) — the freeze review; this board re-examined its residuals R1–R3 post-freeze.
- [board/](board/) — the five full specialist reports.
