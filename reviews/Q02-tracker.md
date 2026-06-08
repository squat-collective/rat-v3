# Q02 — review tracker

> Live status of the Q02 external review. Update the table as reviewers are recruited and findings land. Each reviewer's findings go in their own `reviews/11-q02-<name>.md` (template below); the synthesis at the bottom feeds the **Q01** (v2-vs-v3) call.

> **⚠️ Provenance (2026-06-02) — a SIMULATED dry-run has been run; the real review is still owed.**
> A 5-agent deliberating panel (4 lens-reviewers + a defending maintainer — **AI personas, not humans**) ran the Q02 brief end-to-end. Its findings are in `reviews/11-q02-*.md` and are synthesized below. **This dry-run does NOT discharge the Q02 gate.** It is a strong *internal* pass (weight it like reviews/00–08), and it earned its keep — it surfaced a concrete **pre-publish punch-list** and exercised the kit — but it shares the project's own (Claude-shaped) blind spots, which is the exact thing Q02 exists to escape. **Real external human reviewers are still required before the freeze leaves local/unpushed.** The recruitment table stays live.

## Reviewers — real external (recruitment still owed)

| lens | reviewer | brief sent | status | findings doc |
|---|---|---|---|---|
| architect/contracts | _(tbd)_ | [architect](archive/Q02-brief-architect.md) | **not started** | |
| security | _(tbd)_ | [security](archive/Q02-brief-security.md) | **not started** | |
| SRE/operability | _(tbd)_ | [SRE](archive/Q02-brief-sre.md) | **not started** | |
| ecosystem _(optional)_ | _(tbd)_ | [ecosystem](archive/Q02-brief-ecosystem.md) | **not started** | |

Status flow: `not started` → `reached out` → `accepted` → `reviewing` → `findings in` → `synthesized`.
Target: **architect + security** at minimum; + SRE comfortable. Sourcing: [Q02-reviewer-shortlist.md](archive/Q02-reviewer-shortlist.md). The dry-run synthesis below is a **baseline for real reviewers to react to / falsify**, not a replacement.

## Simulated dry-run panel — findings in (2026-06-02)

| lens | persona (simulated) | findings | doc |
|---|---|---|---|
| architect/contracts | versioned-extension-contract scars (CRD/apimachinery, LSP, OSGi) | 9 (3 High, 4 Med-H/Med, 1 Low, +F9 joint) | [11-q02-architect.md](11-q02-architect.md) |
| security | control-plane threat-modeler (runc/gVisor, STS, capability authz, Sigstore) | 7 (3 High, 1 Med-H, 2 Med, 1 Low-M) | [11-q02-security.md](11-q02-security.md) |
| SRE/operability | on-call for K8s controller-managers + Temporal at scale | 7 (1 Critical, 4 High, 1 Med-H, 1 Med) | [11-q02-sre.md](11-q02-sre.md) |
| ecosystem | DevRel/integrations lead (Terraform/dbt/Singer/Backstage) | 7 (1 Critical, 4 High, 1 Med, 1 Low) | [11-q02-ecosystem.md](11-q02-ecosystem.md) |
| maintainer _(defender)_ | RAT v3 core author — defends/concedes | 13 Q&A → **12 conceded, 1 mixed, 0 bluffs** | [11-q02-maintainer-defense-log.md](11-q02-maintainer-defense-log.md) |

## Findings-doc template

Copy to `reviews/11-q02-<reviewer-or-lens>.md` when a review comes back:

```markdown
# Q02 external review — <reviewer / lens>

Reviewer: <name / role>  ·  Lens: <architect|security|sre|ecosystem>  ·  Date: <YYYY-MM-DD>

## Bottom line
<their one-paragraph verdict: would you make/bet on this, and the single thing to fix first>

## Findings
### <title>
- Severity: Critical | High | Medium | Low | Nit
- Area: premise | contracts | core | data-plane | operability | ecosystem | prior-art
- Finding: <what's wrong / risky / missing>
- Why it matters: <consequence; ideally a concrete failure scenario>
- Suggested direction: <optional>
- Our response: <triage — accept / dispute / defer; link an ADR or backlog item>
```

---

# Synthesis — SIMULATED dry-run (2026-06-02)

> Source: the 5 `reviews/11-q02-*.md` docs above. **Provisional** (AI personas). Feeds Q01 and the freeze-unpush decision *as a strong internal signal*, not as the external review the gate requires.

## Bottom line (the one read that feeds Q01)

**GO / adjust-before-unfreeze — not "reconsider the bet."** All four lenses would bet on the foundation. The security lens independently **validated the sealed enforcement spine** — "C5 authz derived from manifests + audited (C4), D4 conformance *verified* not declared, podman D1 enforces a real kernel-level floor — real, not theater." The SRE's headline — **"the wire is right; the run-lifecycle code around it is where the 3am risk lives"** — re-confirms the reviews/09 dissent ("green certifies shapes, not obligations") with line-level evidence. **The frozen wire produced no demand for a hard breaking change.** What it produced is a **pre-publish punch-list of soft/additive items** — every one of them cheap *now*, while the freeze is local, and expensive *after* it publishes. That is precisely the window ADR-013 kept the freeze local to protect.

## Critical / must-resolve-before-unfreeze (the pre-publish punch-list)

| # | Finding | Lens(es) | Sev | Freeze impact | Fix while local |
|---|---|---|---|---|---|
| **PU-1** | **Bytes-leg has no producer-channel-auth obligation** — the Arrow leg bypasses the core, so the planned C2 fix can't reach it; the reference trusts raw HTTP headers (`bulkleg_test.go:39`). A leaked/replayed ticket presented with the right headers succeeds. | security F1 **+** architect F9 (2 lenses) | **High** (Critical untrusted-multitenant) | **SOFT freeze-reopen** — needs a normative conformance MUST in `data.proto` `ArrowStream` prose + a wrong-channel/right-header vector. No message change. | Add the MUST + vector before any third-party producer ships trusting headers. *Rated #1 by the maintainer.* |
| **PU-2** | **Keystone envelope is the least-tested part of the most-irreversible wire** — `common/v1/context.proto` + gateway stamping (C1/C2/C3/C7) got data-axis *billing* in the freeze (ADR-009) without data-axis *rigor*: ADR-007 §Neutral concedes the golden vectors don't assert on context carriage; one impl exercises it, not the two-reference cross-run ADR-003 mandates. | architect F1 (maintainer A3, co-highest) | **High** | Not a field change — but it **qualifies ADR-015's "freeze validated" claim**: the spike validated the axes it exercised, *not* the keystone. | Add a context-carriage conformance suite + a 2nd independent gateway before treating the envelope as frozen-with-confidence. |
| **PU-3** | **Conformance attestation has no expiry / revocation / multi-authority scoping** — `Conforms` is static set-membership; a conformed-then-CVE'd plugin is "conformed forever," one leaked authority key = full trust. | security F5 (maintainer S3) | **Med** (supply-chain load-bearing) | **SOFT freeze-reopen** — additive `expires_at` + revocation ref on the attestation/marketplace shape; awkward to add post-major. | Add the fields now; design revocation + scoped/threshold authorities. |
| **PU-4** | **Tenancy: `DECISION_KIND_SHARING` is decidable but un-actionable** on flat-string primitives — no delegation/grant shape in `state`/`storage` to carry an *allowed* cross-tenant share; hierarchical tenancy is inexpressible. | architect F5 (maintainer A2) | **Med-High** | **SOFT freeze-reopen / decide-now scope call** — maintainer: actioned-sharing + hierarchy are "effectively v2." | **Decide now:** declare v1 **isolation-only** (mark SHARING advisory-not-enforced — cheap) *or* add the delegation primitive to `rat/1` before publish (retrofitting it later is the expensive v2). |

**Decide-the-additive-now seams** (not flagged freeze-reopen, but the additive door closes at publish): **architect F2 / maintainer A1** — within-method semantic-field skew has no negotiation handle (the ADR-012 crash-safety fields shipped as plain fields → a version-skewed consumer silently double-applies; decide capability-URI-per-behavior *or* an additive `min_revision`); **architect F7** — `Event` identity is unsigned in-body through a pluggable bus while `AuditRecord` is core-signed (mirror the signature additively before subscribers trust it); **security F6** — `vend-credentials` can't express read-vs-write intent (split into additive URIs).

## Multi-tenant availability cluster (core-impl — none freeze-reopen; the "wire is right, code is paper" theme)

> Jointly framed by security + SRE: **"availability is a tenancy boundary the spike does not isolate."** All fixes are core code against an already-adequate frozen wire.

| # | Finding | Lens | Sev | Note |
|---|---|---|---|---|
| **AV-1** | **`core/lease` has no error channel** — `Renew/Acquire` return `bool`; a backend blip is indistinguishable from lease-lost → every replica steps down + bumps the fencing term on *every* transient blip. (Blast-radius narrowed: active-active gateway keeps serving; only reconcile wedges.) | sre F1 (maintainer R1) | **🔴 Critical** | **Close first** — free now; a breaking refactor once a durable backend binds the `bool` interface. Verified test-gap: no failover test injects an erroring `Renew`. Category nuance: **impl, not wire** — `state.PutOutcome.UNKNOWN` already models unreachable-backend; the Go interface flattens it. |
| **AV-2** | **Both "enforcing" runtimes drop the frozen `LaunchSpec` resource limits** — no `--memory/--cpus/--pids-limit` in `podman.go`/`localprocess.go` → runaway/fork-bomb OOMs the shared host. | sre F7 **+** security F4 (maintainer R4) | **High** | Fields already frozen (`deployment_runtime.proto:68-69`); pure impl gap. Multi-tenant gate, not GA nicety. |
| **AV-3** | **Reconcile loop serializes under one mutex with unbounded runtime RPCs** — one hung `Healthcheck` pins all tenants *and* blinds `Status()` (shares the lock). | sre F2 | **High** | The gateway got C3 deadline-bounding; the reconciler's own RPCs did not. |
| **AV-4** | **`Degraded` is a terminal black-hole** — no `Reset/Resume`, no event/metric on transition; `reconciler_test.go:118` *codifies* "no activity after Degraded." A transient upstream blip kills a plugin until a human restarts the core, silently. | sre F3 (maintainer R2) | **High** | Right shape = capped-infinite-retry + on-transition event (K8s never gives up permanently). |
| **AV-5** | **Seccomp isn't in the enforced I9 minimum** — `checkI9Minimum` gates 3 booleans; `seccomp_profile: "unconfined"` passes straight through. | security F3 | **Med-High** | One-line gate fix; the unfiltered syscall surface is what escape-CVEs need. |
| **AV-6** | **Arrow ticket: in-memory single-use + un-rotatable symmetric key** — replay reopens on producer restart / second replica; no `key_id` (vs `Attestation.KeyID`). | security F2 (maintainer S2) | **High** | Bytes-leg adjacent to PU-1. Only short TTL bounds damage today. |
| **AV-7** | Writable mounts lack `noexec`; `/data` is `0o777`. | security F7 | **Low-Med** | Defense-in-depth; already noted in-code as a production fix. |

## Tier-0, observability, selection, discipline

- **T-1 (sre F4, High):** state-backend SPOF has **no degraded mode** + the "always re-read state" guarantee is **unexercised** (the spike reconciler reads a *fixed* slice; the real read path is unbuilt). Honestly labeled tier-0, but the *mitigation is thin* and the bootstrap-seat **recovery** leg is under-specified.
- **O-1 (sre F5, Med-High):** native `/metrics`+OTel is paper, and the core emits **nothing on state-transition edges** → a stuck pipeline is invisible; prerequisite for AV-4's alerting.
- **O-2 (sre F6, Med, already-tracked):** no backup/restore consistency + no upgrade-skew policy. Priority pull-forward: **upgrade-skew first** (partial upgrades are the *normal* case); go git-first desired-state for half the DR win free.
- **P-1 (architect F6 + ecosystem F3, Med):** capability negotiation resolves *what*, never *which* — the **plane/pipeline/binding desired-state language** (where provider *selection* happens) is load-bearing but **unspecified and unfrozen**, outside the contract triple. Name it as a first-class contract artifact.
- **K-2 (architect F3, High-process):** the **freeze gate is structurally blind to omission** — ADR-010/B1 (the late-found catalog gap) is the existence proof, not the last case. Run an explicit omission-audit before each *real-backend* reference lands.
- **D-1 (architect F4, Med-High-discipline):** **"the core does six things" is unfalsifiable** — the cross-cutting enforcement layer is unbudgeted core; the temptation ledger counts new *nouns* but not enforcement-layer accretion (the K8s apiserver lesson). Add an enforcement-obligation count to the ledger.

## Ecosystem cold-start (GTM/impl — none freeze-reopen)

- **EC-1 (ecosystem F1, 🔴 Critical-cold-start):** **no walkable `git clone → running third-party plugin` path** — `examples/**` ship *no* `plugin.yaml`, the 2 detached `contracts/examples/` manifests don't match runnable dirs, and ADR-006 D2's promised `examples/README.md` **doesn't exist** (a concrete doc-drift regression). Fix is P1, not Phase 4: co-locate real manifests + ship the README + pull `rat plugin validate` forward.
- **EC-2 (ecosystem F2, High):** no inner dev loop, and the frozen manifest has **no `image`/`command`/`entrypoint`** field though ADR-016 builds the `LaunchSpec` "from the manifest." Decide if launch metadata is a manifest field (**additive — decide now**) or operator-config.
- **EC-3 (ecosystem F4, High):** the conformance-trust chain is **broken in the middle** — core-load D4 *is* built (credit; do not re-flag), but the **issuance pipeline + marketplace install-check are paper** (`conformed` self-asserted; `plugin.v1.json:6` banner vs `verified.go` drift). Build runner→signer→publish as P1; render marketplace `conformed` as *unverified* until then.
- **EC-4 (ecosystem F5, High-GTM):** **no cold-start wedge** — the celebrated properties are *maintainer* benefits, not *acquisition* levers (confirms reviews/05). Name one wedge axis (`format`/`catalog` on the Iceberg/Delta tailwind), seed it hand-to-hand with design partners.
- **EC-5 (ecosystem F6, Med):** **governance** — one vendor controls the contract, registry, marketplace, *and* the single conformance authority keyring → open-core rug-pull + single-keyholder trust-root risk. Publish a governance/relicense pledge before recruiting authors.
- **EC-6 (ecosystem F7, Low) + architect F8 (Low):** two version axes + manifest↔image coupling friction; and `overview.md`'s manifest example contradicts the frozen schema (doc fix).

## Cross-reviewer agreement / disagreement

**Agreement (high-confidence — multiple independent lenses):**
- **Bytes-leg identity** is the sharpest frozen-surface gap — security **and** architect filed it independently (PU-1).
- **Multi-tenant availability** is unisolated — security **and** SRE co-filed the resource-limit + reconcile-stall cluster.
- **Conformance/attestation governance is immature** — security (revocation/expiry) **and** ecosystem (issuance/marketplace) converge.
- **Provider-selection language is unfrozen** — architect **and** ecosystem (P-1).

**Disagreement / sharpening (not averaged away):**
- **Lease finding category:** SRE rates it Critical-impl; architect/maintainer note the *wire* is adequate (`state.PutOutcome.UNKNOWN` already models unreachable-backend) — so it's a `core/lease` **Go-interface** gap, not a wire gap. No conflict on severity or freeze-status; a precision correction.
- **D4 scope:** ecosystem initially read `conformed` as "self-asserted everywhere"; the maintainer **self-corrected** — core-load attestation verification (`NewVerified`) *is* real; only issuance + marketplace are paper. The corrected scope is in EC-3.

## Freeze-reopen verdict (per ADR-013)

- **Hard freeze-reopen (forces a breaking wire change / a v2 plan): 0.** No reviewer demanded changing a frozen message shape; the spike's "the wire is buildable-against" conclusion held under adversarial scrutiny.
- **Soft freeze-reopen (additive / contract-surface, best done pre-publish): 3** — **PU-1** (bytes-leg auth MUST + vector), **PU-3** (attestation expiry/revocation), **PU-4** (tenancy scope-or-delegate). Plus PU-2 (keystone conformance debt) as a pre-unfreeze *confidence* item, and 3 decide-the-additive-now seams.
- **Per ADR-013, none forces a v2 *if acted on while the freeze is local*** — which is the whole reason the freeze was kept local. **Therefore: the freeze must NOT leave local/unpushed until (a) the PU-1..4 punch-list is resolved and (b) real external human review runs.**

## Net read on the bet (feeds Q01) + resulting actions

**The bet's foundation is evidence-stronger after this pass, with one honest qualifier.** A four-lens adversarial panel + a concede-don't-bluff maintainer found **no reason to reconsider** the from-scratch v3 premise, **validated the sealed enforcement spine**, and surfaced **zero hard wire regrets** — only an additive punch-list catchable in the preserved local window. *Qualifier for Q01 honesty:* ADR-015's "the freeze is validated" should be narrowed to **"validated on the data axes the spike exercised"** — the keystone cross-cutting envelope (the most irreversible surface) had the *weakest* conformance (PU-2), and the freeze gate is structurally blind to omission (K-2). That is not a reason to stop; it is the **#1 pre-unfreeze action** and a process lesson to bank.

**Resulting actions:**
1. **A pre-unfreeze punch-list ADR** (PU-1..4 + the 3 decide-now seams) — the additive changes to land *before* the freeze is published. ← the single highest-leverage next artifact.
2. **Recruit the real external reviewers** (architect + security minimum) — hand them this synthesis as a baseline to falsify, not as the answer. They remain required to discharge Q02.
3. **A multi-tenant-availability hardening track** (AV-1 first — it's free today) — gates any real multi-tenant use; not a freeze concern.
4. **An ecosystem-on-ramp P1** (EC-1..3) — pulled forward from Phase 4 because cold-start can't wait for architecture-complete.
5. Backlog updated with the deduped findings (grouped by the sections above); the maintainer's "net-new vs already-tracked" split in [11-q02-maintainer-defense-log.md](11-q02-maintainer-defense-log.md) is the authoritative triage source.
