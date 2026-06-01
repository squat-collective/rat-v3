# Board review — SRE / Operability lens (post-freeze, `rat/1`)

*Reviewer: `sre`. Mandate: run, observe, recover, upgrade — after the 18-axis freeze + 32
references + the composition. On-call posture: adversarial.*

The freeze closed several of my predecessor's blockers (reviews/03): **traceparent is now
MANDATORY** on every RPC (context.proto:75–76, C1), **`resources: {requests, limits}` is a
required manifest field** (plugin.v1.json:7,139–146, C4), the **error model is pinned**
(ERROR_MODEL.md), and **core health is declared native** (observability.proto:6–12). Real
progress. But the freeze locked the *control* surface while leaving the *run lifecycle* —
the part that pages you at 3am — under-specified. The findings below are what the frozen
contracts still don't handle when things break.

Tags: **[V2-REGRET]** wire/shape we'll regret · **[ADDITIVE]** fixable post-freeze but flag now ·
**[PROCESS]** operability gap, not a contract issue.

---

## Findings (ranked by operability severity)

### 1. [V2-REGRET] **[HIGH]** Crash-safety rests on an un-mandated branch-isolation convention; the write leg has no idempotency key
**Evidence:** scheduler.proto:29–36 pins **AT-LEAST-ONCE** WatchDue, dedup'd by
`(trigger_id, fired_at_unix_ms)` *only at the reconciler*. Ground-truthing with `architect`
splits the picture:
- The **publish** step IS idempotent under retry — `MergeBranch` carries an `idempotency_key`
  (catalog.proto:99) + `expected_into_snapshot` CAS guard (catalog.proto:93). A re-submitted
  merge is a no-op. Good.
- The **write** step is NOT — `strategy.Apply` (strategy.proto:31–42) and `WriteResult`
  (data.proto:74–84) carry **no idempotency key**; a crash mid-strategy + re-apply re-writes
  the target.
- The **only** thing giving convergence is the v2 branch model: write to an isolated run
  branch, then publish via the idempotent CAS `MergeBranch` — a half-written run branch is
  never published, so the next loop re-writes + re-merges safely. **That is the missing saga
  primitive — and it is a *convention of the pipeline model, not enforced by the frozen
  contract*.** `strategy.Apply`'s target is a `TableRef` whose `branch` is **optional**
  (data.proto:23): a strategy that writes straight to `main` gets no atomicity and the
  half-write is immediately visible. Nothing frozen forces run-branch isolation. This is the
  v2 lesson (branch-isolated runs, CLAUDE.md "what stays the same") left as etiquette instead
  of contract.
**Recommendation:** make run-branch isolation a **conformance obligation** for write-bearing
strategies (target MUST be a non-default branch published via `MergeBranch` with an
`idempotency_key`), OR add an `idempotency_key` to `Apply`/`WriteResult` directly (additive).
Without one of these, "exactly-once-effective" is an honour-system property a plugin author can
silently violate.

### 2. [ADDITIVE] **[HIGH]** `ArrowStream` has no termination/error signal — a consumer cannot tell partial-failure from done
**Evidence:** data.proto:53–71. The out-of-band bulk leg is Flight DoGet/DoPut with a ticket +
schema and **no trailer** carrying final row-count / checksum / status. A producer that dies
mid-transfer ends the stream the same way a producer that finished cleanly does. So
`format.Append`/`Merge` pulling from a consumer-hosted stream (harness.py:101–114) can commit a
**partial** dataset and return a `rows_affected` that looks complete. This is reviews/07 **R3**,
accepted as "additive later" — but every `v1` consumer frozen *now* is being written to *not*
check for a terminator that doesn't exist. On a flaky network this is silent data truncation,
the worst on-call class: no error, wrong data.
**Recommendation:** the terminator is additive (a final status frame / expected-row-count in the
stream metadata), but ship it early and add a conformance vector for "broken stream → consumer
MUST fail the write, not commit partial." Flag the gap loudly in storage/format `CONTRACT.md`
so reference authors don't assume EOF == success.

### 3. [PROCESS] **[HIGH]** Core-mediated hop does not enforce a deadline on the provider call — a hung provider pins the core
**Evidence:** `deadline_unix_ms` is explicitly a **soft hint** ("authoritative deadline is still
gRPC's", context.proto:62–64). The reference gateway *copies* the deadline number into the
downstream envelope but never derives a gRPC deadline from it: `Invoke` forwards `ctx` as-is
(gateway.go:96–104, 118) and **`InvokeServerStream` sets no deadline on the inner stream at all**
(gateway.go:124–151) — `RecvMsg` blocks indefinitely if the provider hangs. No frozen contract
obliges the core to bound the provider call by `deadline_unix_ms`. At scale a few hung providers
exhaust the gateway's goroutines/conns and the *single mediation point* becomes the outage. This
is the classic "one slow backend wedges the proxy" shape.
**Recommendation:** spec-text on the (already-frozen) types: "the core MUST apply
`min(channel deadline, deadline_unix_ms)` as the provider-call deadline, and abort the relay
(DEADLINE_EXCEEDED) when it elapses." Add a streaming-relay idle-timeout. Both are
enforcement-layer, no wire change — do it before any core lands.

### 4. [PROCESS] **[HIGH]** Health contract exists, but no crash-loop backoff — and the doc hides the real provider
**Evidence:** *Corrected after `architect` consult* — a frozen health contract **does** exist:
`rat://deployment-runtime/v1/healthcheck` → `Healthcheck` RPC (deployment_runtime.proto:35) +
`HealthStatus{HEALTHY,UNHEALTHY,…}` (deployment_runtime.proto:90–93), frozen at rat/1.3. So
overview.md:181's "verify healthcheck → request restart" *does* map to a real capability. Two
residual operability gaps stand: **(a)** overview.md:181 attributes the restart to a phantom
`plane-manager-plugin` — the actual provider is deployment-runtime, so the doc misleads an
operator tracing the loop; **(b)** there is **no backoff / crash-loop cap / restart jitter
anywhere** (reviews/03 #3 design gap, unaddressed) — "request restart" with no ceiling is the K8s
CrashLoopBackOff mistake re-made, and a crash-looping engine will hammer the deployment-runtime.
The reconciler is *designed, not built*, so this is an enforcement-layer obligation to pin before
the core lands.
**Recommendation:** fix overview.md's `plane-manager-plugin` → deployment-runtime. Specify
mandatory exponential backoff + crash-loop ceiling + lease-renewal jitter for the reconciler's
restart path (no wire change — reconciler behaviour).

### 5. [PROCESS] **[MED-HIGH]** The composition proves only the happy path — no recovery when a plugin dies mid-pipeline
**Evidence:** `strat.apply` runs `catalog.get-table → engine.query → format.overwrite` as a bare
sequential chain (harness.py:193–204) with no transaction, checkpoint, or compensation. For
`full_refresh` the terminal `format.overwrite` means a crash *after* the overwrite starts leaves
the target half-written, and the frozen strategy contract provides **no atomic-write / saga
primitive** to make the snapshot-swap crash-safe. The reconciler-as-truth model says "re-run next
loop" — but combined with finding #1 (no idempotency key) the re-run *double-applies* for append
strategies. reviews/07's own pointer ("note failure handling, or lack of") is accurate: the
harness `except` just records ERROR and moves on; nothing exercises mid-run death.
**Recommendation:** add a crash-mid-strategy case to the composition (kill a provider between
stages, assert the target is either fully-old or fully-new, never partial). The contract *can*
guarantee it **iff** the strategy uses run-branch isolation + idempotent `MergeBranch` (see
finding #1) — but the composition's fullrefresh path does a direct `format.overwrite`
(harness.py:204), exercising the *unsafe* path, not the safe one. Per `architect`, the
write→register→merge linkage's middle (how a written snapshot enters the catalog) is itself
GA-deferred — so the atomicity story rests on an un-contracted seam.

### 6. [PROCESS] **[MED]** Capability major-only versioning has no live-rollout (v1→v2) story
**Evidence:** ADR-002 D4 = major-only. A bump is a *new capability URI* (`rat://format/v1/merge`
vs `.../v2/...`), resolved by exact string (gateway.go:92–94 → `NotFound` if absent). To roll a
plugin v1→v2 without a flag day, the provider must advertise **both** capabilities while every
consumer migrates its `requires` — but nothing in the manifest schema or any `CONTRACT.md`
mandates or even documents dual-advertisement, and there is no preflight/skew check
(reviews/03 #6, blocker, unaddressed). So a partial upgrade yields runtime
`no provider for capability` errors and failed pipelines — not a refused upgrade.
**Recommendation:** document the dual-advertise rollout pattern + a `rat preflight` orphan check
as the supported upgrade procedure. Process/doc, not a wire change — but it has to exist before a
real operator upgrades anything.

### 7. [PROCESS] **[MED]** The +0.2ms perf claim is a single-goroutine localhost microbench with enforcement stubbed out
**Evidence:** README (bench/latency-go) is honest that it's single-goroutine, localhost TCP,
trivial RPCs. Two caveats it doesn't carry: (a) the bench gateway's "enforcement" is a
traceparent string-shape check + map lookup (gateway.go:82–104) — the **real** C2/C5 path calls
the identity plugin (`Authenticate`/`Authorize`), adding a network round-trip per Invoke that the
0.2ms omits; (b) the gateway is a *shared* serialization point for all control RPCs, so under
thousands of concurrent pipelines the hop cost becomes queueing latency, not a constant. The
"~ms, negligible" conclusion also assumes "a handful of control calls per run" — incremental/scd2
strategies doing per-key catalog/format calls multiply the hop count.
**What's sound:** the **data-plane-bypass** claim *is* load-bearing and correct — bytes move
out-of-band via `ArrowStream` (data.proto) and never touch the gateway, so the core is not a
throughput chokepoint. That architectural bet holds; it's the *control*-hop absolute number that's
under-measured.
**Recommendation:** re-run with concurrency + a real identity-plugin hop before quoting 0.2ms as
the production figure; keep the data-bypass claim as-is.

### 8. [PROCESS] **[MED]** "Core health is native" is asserted in prose, backed by no frozen artifact
**Evidence:** observability.proto:6–12 promises native `/metrics`, OTel spans, reconcile-loop
SLIs independent of any observability plugin ("`observability: none` must still leave the core
self-observable"). Exactly the right intent (closes reviews/03 I1/I2 *in principle*). But there is
**no pinned core-health surface** — no `/metrics` schema, no golden-signal list, no healthcheck
RPC, no SLO doc. It's native-by-assertion. Since the core isn't built and finding #4 shows the
reconciler has no health contract to call, "native" is currently a promise an operator can't
verify.
**Recommendation:** pin a minimal core SLI list (reconcile staleness, lease churn, bus lag,
per-plugin RPC error rate) + a `/metrics` contract now, while the core is still paper — it's free
to specify and expensive to retrofit consistency onto later.

### 9. [ADDITIVE] **[MED]** Streaming capabilities emit no terminal audit record — "started, but how did it end?" is unanswerable
**Evidence:** *Confirmed with `security`.* invoke.proto:53–55 + ADR-008 pin **one C8 audit record
per stream, at OPEN**. There is **no contractual terminal record**: a stream that dies mid-relay
after open produces only the open-time "allowed" record. For unary, the deny case is now audited
(S3 fixed), but the record is at the *enforcement decision*, not pinned at *completion* — so an
allowed unary whose provider then dies can leave an "allowed" record with no terminal-failure
record. For an on-call reconstructing a run, "this call started AND how it ended" is **not met for
streaming capabilities at v1** — exactly the long-lived calls (runtime.Execute, observability
Ingest, scheduler WatchDue) where mid-stream death is most likely.
**Recommendation (joint with `security`):** make it a C8 conformance obligation — "every call
emits one record at the enforcement decision, AND streams additionally emit a terminal close
record (outcome ∈ {SUCCESS, ERROR, DENIED})." Additive: `AuditOutcome` already has `ERROR`, no
wire change.

---

## What the freeze got right (operability wins, for calibration)
- **traceparent mandatory** (context.proto:75) — the single most important diagnosability
  retrofit from reviews/03 #1, now un-skippable. Big.
- **Error model pinned** (ERROR_MODEL.md) — two impls can no longer disagree on what
  `NOT_FOUND` vs `FAILED_PRECONDITION` means; an operator's runbook can key off codes.
- **`resources` mandatory in manifest** (plugin.v1.json:139) — noisy-neighbor reasoning is now
  declarable, closing reviews/03 #8's wire-break risk before freeze.
- **Data plane bypasses the core** — confirmed by the bench; control-plane outage doesn't stop
  in-flight S3 work. Best blast-radius decision in the design.

---

**Biggest concern:** the frozen run lifecycle is crash-safe *only by convention, not by
contract*. The pieces for exactly-once-effective exist — idempotent `MergeBranch` + CAS
(catalog.proto:99,93) and the v2 run-branch model — but the frozen `strategy.Apply`/`WriteResult`
permit a strategy to write straight to `main` with no idempotency key (strategy.proto:31,
data.proto:23/74), and an `ArrowStream` that can't signal partial failure (data.proto:53) means
even the branch write can silently truncate. Nothing frozen *forces* the safe path. So a
mid-pipeline crash or a duplicate fire **can silently produce wrong data** in any deployment whose
plugin authors didn't independently rediscover the branch-isolation discipline — the exact failure
no operator sees until the numbers are already corrupt. Make run-branch isolation (or a write-leg
idempotency key) a conformance obligation before that convention is load-bearing in production.
