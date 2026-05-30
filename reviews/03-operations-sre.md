# Operations / SRE review — RAT v3

*Reviewer mandate: run RAT v3 in production for a 50-person data team on the "self-hosted team" topology (postgres + docker + S3 + Iceberg/Nessie + OIDC-Keycloak + plugins). Evaluate operability. Sharp, non-flattering.*

---

## Headline

**Not production-ready as designed — and the gap isn't code, it's that operability was deferred to "it's a plugin."** The architecture is genuinely elegant for the *happy path*, but every concern that makes a platform survivable at 3am — debugging across process boundaries, capacity signals, upgrade ordering, DR, incident runbooks — has been pushed onto the plugin boundary without anyone owning the cross-plugin story. The single biggest operational risk: **the failure domain is enormous and undiagnosable.** A 50-person team's pipeline "doesn't run," and the operator must trace causality across the reconciler, NATS JetStream, the state-backend, the scheduler plugin, the deployment-runtime plugin, the engine plugin, and the catalog plugin — six independently-versioned processes in (per ADR-001) any mix of languages — with no specified correlation ID, no specified trace context propagation, and the explicit admission (ADR-001 Negative #3) that "this is harder to debug than imperative orchestration" answered only by a `rat diagnose` tool that does not yet exist.

---

## What's strong operationally

Calibration — these are real, and they matter:

1. **Reconciliation-as-source-of-truth (overview.md §reconciliation; ideas/inbox event-bus entry).** The decision that "events are hints, the reconciler always re-reads state" is the single best operational call in the whole design. It means lost/duplicate/out-of-order events degrade gracefully instead of corrupting state. This is exactly the K8s lesson and they got it right. It makes the system *eventually correct* even when the bus is sick — which is the difference between "slow" and "down."

2. **Stateless core + leader-election/lease (ADR-002 D5).** Core replicas behind an LB with one lease-holder is a proven, boring HA pattern (K8s controller-manager). Failover ~15–20s is fine for a control plane. Boring is good here.

3. **Data plane bypasses the core (vision.md commitment #4; overview.md §communication).** Bytes never flow through the core. This means the control plane can be completely down and *in-flight* queries against S3 keep working; it also means the core is not a throughput chokepoint you have to capacity-plan against TB/s. Huge blast-radius reducer.

4. **Same binary across topologies (vision.md commitment #6).** From an ops standpoint this means your staging environment can actually resemble prod (same core, swap a couple plugins), and there is no "community edition vs enterprise edition" divergence where the thing you tested isn't the thing you run. Underrated operability win.

5. **NATS JetStream embeddable → external is a config flag (ADR-002 D2).** One messaging protocol from laptop to cluster means you don't re-learn the bus when you scale. JetStream gives durability + replay, which is the right substrate for an at-least-once event model.

---

## Gap analysis by concern

### Concern 1: Debugging
- **Coverage in current ADRs:** ADR-001 *names* the problem (Negative #3) and gestures at a `rat diagnose` tool "from day 1." The vision conversation repeats it (risk #4). The reconciler-as-source-of-truth model (inbox) helps reasoning.
- **Gap:** There is no specified diagnostic substrate. No correlation/trace ID is defined in the contract triple. The proto examples (`engine.proto`, `catalog.proto`) carry no request-context/trace field. A `pipeline_run_requested` event has no defined causality chain to the `pipeline_run_failed` event. With plugins in arbitrary languages (ADR-001), there is no enforced structured-log schema or trace propagation — so you *cannot* reconstruct "this run, across these 6 processes." `rat diagnose` is named but unspecified; nobody owns what it inspects or how a plugin exposes its internal state to it. This is the Airflow "task is stuck in `queued` and the scheduler logs say nothing" problem, except multiplied across a polyglot plugin mesh.
- **Severity:** **blocker**
- **Recommended addition:** Make a **trace/correlation context a mandatory field in every proto RPC and every event envelope** (W3C `traceparent`). Define a `Diagnostics` capability in the plugin manifest: every plugin MUST implement `GetHealth()` + `GetRunContext(run_id)` returning structured state. Specify `rat diagnose <run_id>` as a contract: it walks the reconciler's view, fans out the diagnostic RPC to every plugin that touched the run, and prints a single causal timeline. Write this ADR *before* the engine proto is frozen — retrofitting trace context into a frozen wire contract is a major-version break.

### Concern 2: Observability
- **Coverage in current ADRs:** `observability` is a plugin axis (stdout, prometheus, otel, datadog — ADR-001). Events are "for coordination + observability" (overview.md).
- **Gap:** Making observability *a plugin* is precisely the bolted-on anti-pattern. If a solo bundle ships `observability: stdout`, then the platform's own internal metrics (reconcile-loop latency, lease churn, event-bus lag, plugin RPC error rates, queue depths) only exist if an operator installs and wires a plugin. **The core's own SLIs are not first-class** — there is no specified `/metrics` on the core itself, no defined golden signals for the reconciler. Worse: with observability pluggable, two plugins may emit metrics with incompatible label schemes, so you can't build a single dashboard. There is no SLI/SLO story at all — nothing defines "what does 'RAT is healthy' mean" as a measurable.
- **Severity:** **blocker** (for team scale)
- **Recommended addition:** The core MUST expose its own Prometheus `/metrics` and OTel spans **natively**, independent of any observability plugin — reconcile loop duration, items reconciled, reconcile errors, leader-election transitions, event-bus publish/consume lag, per-plugin RPC latency/error rate (the core is the RPC client, so it can measure this for free). Define a **mandatory plugin metrics contract** (RED method: rate/errors/duration with a fixed label set). Ship a reference SLO doc: e.g. "reconcile staleness p99 < 30s," "pipeline scheduling latency p99 < interval+30s." The observability *plugin* should be about *export destination*, not about whether telemetry exists.

### Concern 3: Capacity planning
- **Coverage in current ADRs:** overview.md §scalability gives hand-wavy bands ("tens of pipelines/sec" for team, "thousands" for enterprise). ADR-002 D5 says leader+lease is "sufficient until >1k reconciliations/sec."
- **Gap:** No model for what *grows* with what. Key unknowns for a 50-person team: (a) the reconciler is O(declared pipelines × 1/interval) — at what pipeline count does a single-threaded leader loop blow its interval? (b) postgres state-backend: how many rows/sec of Put under N pipelines, and does `Watch` use LISTEN/NOTIFY or polling? (c) NATS JetStream retention: events accumulate — what's the disk sizing and retention policy? (d) each plugin is a process/container — memory per plugin × plugin count × planes. None of this is modeled. An operator cannot answer "how big a box, how much postgres, how much NATS disk" from any document.
- **Severity:** **important**
- **Recommended addition:** A capacity ADR with a concrete formula per resource: reconcile-loop cost vs. pipeline count (and the threshold where you need sharded reconcilers), postgres IOPS/connection-pool sizing vs. pipeline+user count, NATS JetStream stream sizing + retention defaults, and a per-plugin RSS baseline table. Ship `rat capacity-estimate` reading the declared plane/pipeline set. Until this exists, every deployment is a guess.

### Concern 4: Upgrade safety
- **Coverage in current ADRs:** ADR-002 D4 (major-version-only capability versioning, K8s-style; multiple majors coexist). ADR-002 D6 (manifest in-image, version-coupled to image).
- **Gap:** Versioning the *capability contract* is addressed; the *upgrade procedure* is not. Unanswered: (a) **ordering** — if core v2 expects `engine/v2` but the installed engine plugin only speaks `engine/v1`, what happens? Does the registry refuse the core upgrade, or does the core start and the engine silently 404? (b) **rolling vs big-bang** — with leader election you can roll core replicas, but is there a compatibility guarantee that core vN and vN+1 can co-exist behind the LB during a roll? Nothing states it. (c) **state-backend schema migrations** — the state schema is owned by... the core? the state plugin? Who runs migrations, and are they reversible? (d) **rollback** — if core vN+1 migrated the state schema and you roll back to vN, is the schema still readable? Undefined. (e) ADR-002 D6 *couples manifest version to image version*, which the ADR itself flags as a negative — so you can't fix a manifest bug without re-releasing the image. This is the "Airflow upgrade ate my DAGs" / "K8s API deprecation broke my CRDs" pain, with none of the guardrails (no `kubectl convert`, no deprecation policy, no compatibility skew matrix).
- **Severity:** **blocker**
- **Recommended addition:** An upgrade ADR specifying: a **version-skew policy** (core supports plugins N and N-1 of each capability major, like kubelet/apiserver skew); **preflight check** (`rat preflight upgrade` fails the upgrade if any installed plugin would be orphaned); state-schema migrations are **forward-and-backward compatible within a major** (expand/contract pattern, never destructive in a single release); and a documented rollback procedure with the explicit guarantee that vN+1 doesn't make state unreadable by vN.

### Concern 5: Backup + restore
- **Coverage in current ADRs:** Effectively none. State is "a plugin" (postgres/sqlite/dynamo); the design treats persistence as someone else's problem.
- **Gap:** There is no DR story whatsoever. The platform's *durable truth* is split across at least three places: (1) the state-backend (manifests, planes, pipeline definitions, run history, leases), (2) NATS JetStream (the event log — durable per D2, so it IS state), and (3) plugin-local config/secrets. **Nothing defines a consistent backup point across these.** If you `pg_dump` the state-backend at T1 and snapshot NATS at T2, you can restore into an inconsistent state (a run the state says completed, an event-log that replays it again). No RPO/RTO targets. No "how do I restore pipeline definitions after a fat-finger `DROP`." For a 50-person team, losing run history (lineage, audit) or pipeline definitions is a serious incident with no recovery procedure.
- **Severity:** **blocker**
- **Recommended addition:** A backup/restore ADR defining the **consistent backup set** (state-backend + JetStream streams + plugin configs) and a quiesce-or-snapshot procedure that captures them at a coherent point (e.g. pause the reconciler lease, checkpoint JetStream consumer offsets, dump state, resume). Set RPO/RTO targets (e.g. RPO 5min, RTO 1h for team scale). Ship `rat backup` / `rat restore` that orchestrates this across the relevant plugins via a defined `Backup` capability. Critically: **pipeline definitions and plane declarations should be exportable as plain YAML and GitOps-able** — the strongest DR is that desired state lives in git, not only in the state-backend.

### Concern 6: Incident response
- **Coverage in current ADRs:** None. ADR-001 mentions restart-on-healthcheck-failure in the reconcile loop.
- **Gap:** Zero runbooks. And several failure modes are *architecture-specific* and non-obvious: (a) **NATS down** — reconciler keeps running (good, it re-reads state) but no events flow, so observability/notifications go dark and event-driven plugins idle; does anyone alert that the bus is gone, or does it fail silent? (b) **state-backend down** — the core can't read desired state OR renew its lease → leader steps down → no replica can acquire → *whole control plane wedged*. The state-backend is a hard SPOF that the "everything is a plugin" framing hides. (c) **plugin crash-loop** — the reconciler "requests restart"; what's the backoff? Without one, a crash-looping engine plugin hammers the deployment-runtime (the K8s CrashLoopBackOff lesson — learned the hard way). (d) **lease thrashing** — if state-backend latency spikes, lease renewal flaps and leadership ping-pongs between replicas, and the reconcile loop never makes progress (etcd-slow → apiserver-flaps, a classic K8s outage shape). (e) **eviction storms** — if the deployment-runtime evicts plugins under memory pressure and the reconciler immediately respawns them, you get a thrash loop.
- **Severity:** **blocker**
- **Recommended addition:** A runbook set (see Failure Mode Catalog below) plus three design changes: **exponential backoff + crash-loop cap** on plugin restarts in the reconciler; **explicit lease-renewal jitter + a minimum-hold time** to prevent thrash; and **bus-liveness alerting** so a silent NATS is loud. Treat the state-backend as a tier-0 dependency in docs — its HA is *the platform's* HA, regardless of the "it's a plugin" framing.

### Concern 7: Multi-region
- **Coverage in current ADRs:** None. Scalability section assumes single-region; "core replicated behind LB" is implicitly one region.
- **Gap:** Leader-election-via-state-backend-CAS (D5) does not cross regions cheaply. If the state-backend is single-region postgres, a region loss = platform loss. If you go multi-region, the lease CAS now has cross-region latency on every renewal, and a partition causes split-brain risk (two leaders) unless the state-backend itself provides linearizable cross-region consensus (postgres doesn't, out of the box; dynamodb global tables are eventually consistent, which *breaks* the CAS lease assumption). NATS JetStream cross-region replication is its own non-trivial topology. None of this is acknowledged.
- **Severity:** **important** (not needed at 50-person single-region, but the design makes an implicit single-region assumption that will be expensive to undo)
- **Recommended addition:** Document that multi-region HA requires a state-backend with linearizable cross-region consensus (etcd/Spanner-class), and that the lease primitive's correctness depends on it. Decide now whether multi-region is "active control plane per region, data is global" (likely correct) vs. "one global control plane" (fragile). Even just writing the constraint down prevents a team from picking dynamodb-global-tables and getting silent double-leaders.

### Concern 8: Resource isolation (noisy neighbor)
- **Coverage in current ADRs:** Implicit — isolation is "per deployment-runtime" (inbox sandboxing entry: solo=in-process, team=container, enterprise=signed+netpol). Q15 defers the model.
- **Gap:** Nothing enforces resource limits at the platform layer. A runaway engine plugin that OOMs or pins CPU is contained only insofar as the *deployment-runtime plugin* set cgroup/container limits — and nothing in the contract *requires* the manifest to declare resource asks/limits or *requires* the runtime to enforce them. K8s shipped without good limits/QoS and spent years on OOM-killer tuning, `LimitRange`, and the eviction-priority ladder. RAT v3 hasn't faced this yet. Worse, the **reconcile loop itself is a shared resource**: one pipeline with a pathological reconcile (e.g. a plane that never goes healthy) can starve the single-leader loop, delaying *every other* pipeline. There's no fairness/priority story for the loop.
- **Severity:** **important**
- **Recommended addition:** Make `resources: {requests, limits}` a **mandatory manifest field** (K8s-shaped, as the README already promises "resource asks") and require deployment-runtime plugins to enforce them as a capability precondition. Define reconcile-loop fairness: per-pipeline work budget / timeout so one stuck plane can't starve the loop (the "one slow controller blocks the workqueue" lesson → bounded per-item processing + separate workqueues). Add a circuit-breaker: a pipeline that fails its reconcile N times goes to a `Degraded` state and stops consuming loop budget until manually resumed.

### Concern 9: Secret rotation
- **Coverage in current ADRs:** `secret-backend` is a plugin axis (env, Vault, AWS-SM, GCP-SM, sealed-secrets — ADR-001). Q13 (plugin-to-core auth) deferred.
- **Gap:** Having a secret-backend axis is necessary but not sufficient for *rotation*. Open questions with no answer: (a) when storage creds rotate, do in-flight engine↔storage sessions (which use *vended* credentials per `storage.VendCredentials`) get invalidated, or do they keep working until expiry? Credential vending is actually a *good* rotation primitive — but TTL/refresh semantics are unspecified. (b) **plugin-to-core auth tokens (Q13) have no rotation story at all** — if it's bearer tokens, rotating them across N running plugins without downtime is unsolved. (c) postgres password rotation requires coordinated state-backend reconnect — undefined. (d) env-backend (the solo default) literally cannot rotate without a restart. The `env` secret-backend being a default trains a bad habit.
- **Severity:** **important**
- **Recommended addition:** Specify credential **vending with TTL as the default pattern** (short-lived, auto-refreshed) rather than long-lived secrets — `storage.VendCredentials` already implies this; make the TTL/refresh contract explicit and apply the same shape to plugin-to-core auth (short-lived tokens, mTLS cert rotation à la SPIFFE/SPIRE). Document a zero-downtime rotation procedure per secret class. Flag `env` secret-backend as "dev only — cannot rotate."

### Concern 10: Logging
- **Coverage in current ADRs:** `observability: stdout` default; `audit-log` is a separate plugin axis (file/postgres/splunk/kafka). No logging spec.
- **Gap:** No log format, no correlation, no aggregation story. With plugins in arbitrary languages (ADR-001), absent an enforced schema you get N different log formats, none correlated to a run_id or trace_id. "stdout" as the default means logs vanish unless the deployment-runtime captures them — and *nothing specifies* that the runtime must capture and ship plugin stdout. This is the distributed-logging problem that took the industry a decade to standardize on (structured JSON + trace correlation + central aggregation). RAT v3 is starting from "everyone logs however they want to stdout."
- **Severity:** **important**
- **Recommended addition:** Mandate a **structured-log contract in the plugin spec**: JSON lines with required fields (`ts`, `level`, `plugin_id`, `trace_id`, `run_id`, `msg`). Require deployment-runtime plugins to capture and forward plugin stdout/stderr. Keep audit-log as a separate *semantic* stream (tamper-evident, compliance) but unify *operational* logs under one schema so they're greppable by `trace_id` across the whole mesh.

---

## Top 5 operational missing pieces

Ranked. Without these, RAT v3 cannot run in production at 50-person scale.

1. **Cross-plugin diagnostic substrate (trace context + `rat diagnose`).** The "why didn't my pipeline run" question is unanswerable across the reconciler + bus + 6 polyglot plugins without mandatory trace propagation in the wire contract. Must be designed *before the protos are frozen* — it's a wire-breaking retrofit otherwise. This is the difference between a debuggable platform and a black box.

2. **Native core observability + SLO definitions.** The core must emit its own golden-signal metrics (reconcile latency, lease churn, bus lag, per-plugin RPC error rate) independent of any observability plugin, with a published SLO doc. "Observability is a plugin" cannot mean "the platform has no telemetry until you install one."

3. **Upgrade safety model (version skew + preflight + reversible migrations).** A 50-person team will upgrade core and plugins on different cadences. Without a skew policy, a preflight orphan-check, and forward/backward-compatible state migrations, every upgrade is a coin-flip outage with no rollback.

4. **Backup/restore with a consistent backup set.** State-backend + JetStream + plugin config must be backed up at a coherent point with defined RPO/RTO, and pipeline/plane definitions must be exportable as git-managed YAML. Losing pipeline definitions or run history with no recovery is a business-ending incident for a data team.

5. **Incident runbooks + the design fixes they expose (state-backend SPOF, crash-loop backoff, lease-thrash guards, reconcile fairness).** The "everything is a plugin" framing hides that the state-backend is a hard tier-0 SPOF and that the single-leader reconcile loop is a shared resource that one bad pipeline can starve. These need both runbooks and the backoff/fairness/jitter mechanisms that K8s learned the hard way.

---

## Failure mode catalog

Top 10 ways this breaks in production. *What the operator sees* / *what the runbook must say.*

1. **State-backend (postgres) down or slow.**
   - *Sees:* All pipelines stop scheduling; UI errors; core logs "lease renewal failed"; leader steps down and no replica re-acquires. Total control-plane wedge. In-flight S3 queries keep working (data plane is independent — small mercy).
   - *Runbook:* This is tier-0. Restore/failover postgres first. Lease auto-reacquires once CAS works. Verify no split-brain (two leaders) occurred during the flap. Postgres HA (Patroni/RDS-multi-AZ) is mandatory at team scale — document it as *the* platform SPOF.

2. **NATS JetStream down.**
   - *Sees:* Reconciler still runs (re-reads state — degrades gracefully), but `pipeline_run_requested` events don't reach engine plugins → pipelines go "scheduled but never start." Notifications/observability go silent. Likely *fails silent* unless bus-liveness is alerted.
   - *Runbook:* Alert on bus liveness explicitly. Restore JetStream; replay durable streams. Verify consumer offsets didn't reset (double-execution risk). Confirm engine plugins re-subscribed.

3. **Engine plugin OOM crash-loop at 3am.**
   - *Sees:* Reconciler healthcheck fails → requests restart → restart → OOM → repeat. Deployment-runtime hammered. If no backoff, this generates restart storms and event spam. Pipelines on that plane all fail.
   - *Runbook:* Need exponential backoff + crash-loop cap (currently unspecified — **design gap**). Operator action: pin the plugin to a known-good version, raise the memory limit, or quarantine the plane. Without resource limits in the manifest, root-causing "what made it OOM" requires logs that may not have been captured.

4. **Lease thrashing under state-backend latency.**
   - *Sees:* Leadership ping-pongs between core replicas every few seconds; reconcile loop never completes a full pass; pipelines scheduled erratically or not at all. Looks like "RAT is up but doing nothing."
   - *Runbook:* Check state-backend latency/CAS contention. Need lease-renewal jitter + minimum-hold-time (**design gap**). Mitigate by reducing replica count to 1 temporarily to stop the thrash, fix the backend, scale back up.

5. **A single pathological pipeline starves the reconcile loop.**
   - *Sees:* One plane never reaches healthy (e.g. bad catalog config); its reconcile retries every loop iteration, consuming loop time; *other* pipelines' scheduling latency climbs platform-wide.
   - *Runbook:* Identify the stuck pipeline (needs per-pipeline reconcile metrics — **observability gap**). Move it to `Degraded`/paused. Long-term: per-item reconcile budget + circuit breaker (**design gap**).

6. **Plugin version skew after a partial upgrade.**
   - *Sees:* Core upgraded to expect `catalog/v2`; catalog plugin still speaks `v1`. Either registry refuses (best case, but is it specified?) or RPCs 404/error at runtime and pipelines fail with cryptic capability-not-found errors.
   - *Runbook:* `rat preflight` (**doesn't exist yet**) should have caught this. Roll core back or upgrade the plugin. Document the skew matrix.

7. **Event replay double-execution after JetStream recovery.**
   - *Sees:* After a bus restore/offset reset, `pipeline_run_requested` events replay; pipelines run twice. For non-idempotent strategies (append_only!) this duplicates data.
   - *Runbook:* Engine plugins must dedupe by run_id (idempotency key) — is this mandated? Verify strategy idempotency. The reconciler-as-truth model helps, but the *plugin* consuming the event must be idempotent — needs to be a contract requirement.

8. **Secret rotation breaks live sessions.**
   - *Sees:* Storage creds rotated in Vault; engine↔S3 sessions using vended creds start getting 403s mid-run; pipelines fail with auth errors that look like a storage outage.
   - *Runbook:* Check vended-credential TTL vs. rotation timing. Re-vend creds. Need explicit TTL/refresh contract (**gap**). `env` backend can't rotate at all without restart.

9. **Disk fills from JetStream retention + run history.**
   - *Sees:* NATS disk or postgres disk fills (durable event log + unbounded run history grow with pipeline count). Writes fail → reconciler can't persist → cascades to failure mode #1.
   - *Runbook:* No documented retention/compaction policy (**capacity gap**). Set JetStream stream limits + run-history TTL/archival up front. Monitor disk as a leading indicator.

10. **Core upgrade migrates state schema; rollback can't read it.**
    - *Sees:* Core vN+1 ran a destructive state migration; a problem forces rollback to vN; vN can't parse the new schema → control plane won't start. Now you're restoring from backup under pressure.
    - *Runbook:* This must be *prevented* by expand/contract migrations (**design gap**), not handled. Until then: take a state backup immediately before *every* upgrade and treat rollback as restore-from-backup.

---

## Compared to K8s / Temporal / Airflow operability

What those communities learned the hard way that RAT v3 is ignoring or hasn't hit yet:

- **K8s — CrashLoopBackOff exists for a reason.** Early K8s restarted failing pods immediately and melted nodes. RAT v3's reconcile loop "requests restart" with no specified backoff repeats that mistake. **Adopt exponential backoff + a crash-loop ceiling from day one.**

- **K8s — version skew policy is load-bearing.** kubelet/kube-apiserver skew (N-2/N) is documented and enforced precisely because mixed-version clusters during rolls are the normal case, not the exception. RAT v3's "multiple majors coexist" (D4) is the *capability* version, but there's no *component* skew policy. Without it, every rolling upgrade of a polyglot plugin mesh is undefined behavior.

- **K8s — etcd is the SPOF and everyone pretends it isn't until it isn't.** Slow etcd → apiserver timeouts → controller flapping → cluster-wide outage. RAT v3's state-backend is structurally identical, but the "it's just a plugin" framing actively *de-emphasizes* that it's tier-0. The most common K8s outage shape (etcd latency → leader flap) maps directly onto failure modes #1 and #4 here.

- **K8s — resource requests/limits + QoS classes were retrofitted painfully.** Noisy-neighbor and OOM-eviction ordering took years. RAT v3 promises "resource asks" in the README but doesn't make them mandatory in the manifest or require runtime enforcement. **Bake limits in before the manifest schema freezes** — adding a required field later is a breaking change.

- **Temporal — durable execution + idempotency is the whole game.** Temporal's value is exactly-once-effective semantics via event-sourcing + idempotent activities. RAT v3 has an at-least-once event bus (JetStream) and reconciler-as-truth, but pushes idempotency onto plugin authors *implicitly* (failure mode #7). Temporal learned that you cannot trust authors to be idempotent by default — you give them idempotency primitives (run IDs, dedupe). **Make run_id-keyed idempotency a contract requirement, not a hope.**

- **Temporal — visibility/observability is a first-class subsystem, not a sidecar.** Temporal ships its Web UI, visibility store, and per-workflow history as core. RAT v3 making observability a *plugin* inverts the lesson: the platforms that win on operability treat "see what's happening" as inseparable from "make it happen."

- **Airflow — "the scheduler is up but tasks are stuck in queued" is the canonical support ticket.** The cause is always a multi-component handoff (scheduler → executor → worker → DB) with no single pane of glass. RAT v3's `scheduled-but-never-started` (failure mode #2) is the *identical* shape, with *more* components and *more* languages. Airflow eventually invested heavily in task-instance state visibility and the "why isn't my task running" docs. RAT v3 needs `rat diagnose` to be that, and it must exist at launch, not "from day 1" as an aspiration.

- **Airflow — DAG/version coupling and upgrade pain.** Airflow upgrades that changed serialization or DB schema repeatedly broke deployments; the community built migration tooling reactively, painfully. RAT v3's ADR-002 D6 (manifest coupled to image version) and the absence of a migration story (D7 — "build it reactively") is choosing to relive this. "Reactively" is how Airflow got its reputation.

- **All three — GitOps desired state is the operability escape hatch.** The single biggest thing that made K8s operable was that desired state is declarative text you can version, diff, and re-apply. RAT v3's reconciliation model is *built for this* but the docs treat pipeline/plane definitions as living in the state-backend. **Lean all the way in: desired state should be git-first, the state-backend a cache.** It solves half the DR story (#5) for free.

---

*Bottom line: the architecture's elegance is real and the reconciliation/data-plane-bypass choices are genuinely strong. But "everything is a plugin" has been allowed to mean "operability is someone else's plugin," and the cross-cutting concerns that no single plugin owns — diagnosis, telemetry, upgrade ordering, DR, backoff/fairness — are exactly the ones that decide whether a platform survives contact with production. These must be designed into the core contract (especially trace context and resource limits) before the protos freeze, because they are wire-breaking to retrofit.*
