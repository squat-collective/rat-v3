# Q02 — reviewer brief (SRE / operability focus)

> A reliability-tailored companion to the [main reviewer brief](Q02-external-review-brief.md). It front-loads the **failure domain + the operability questions**; the main brief has the full architecture, the non-SRE questions, and the logistics. Read this if your lens is operations.
> **Confidentiality:** RAT v3 is unpublished and the contract freeze is **local/unpushed**. Please treat everything as confidential and don't redistribute.

## The ask, as an SRE

**Would you carry the pager for this?** RAT v3 is a control plane that orchestrates N independently-versioned, polyglot plugin processes. Our own internal SRE review ([reviews/03-operations-sre.md](03-operations-sre.md)) was the harshest of the lot — its thesis was *"the gap isn't code, it's that operability was deferred to 'it's a plugin'."* We acted on the part that became a Phase-1 exit gate (reconciler crash-loop backoff/jitter + lease-thrash guard — see below), but **most of that review's recommendations are still paper.** We want you to pressure-test the *survivability* of the design at 3am and tell us what must be true before it runs in production / multi-tenant.

## RAT v3 in one SRE-relevant paragraph

A **stateless core** (N replicas behind an LB) runs a **leader-elected reconciler** that reads desired state, compares to actual, and drives convergence — **events are hints; the reconciler always re-reads state** (so a sick bus degrades to "slow", not "wrong"). Plugins are independent processes the core launches via a **deployment-runtime** and supervises by healthcheck. The **state-backend** holds the durable truth *and* the leader-election lease. Bulk data flows out-of-band, plugin↔plugin, **bypassing the core**. (Full architecture: the main brief + `docs/architecture/overview.md` §reconciliation / §scalability.)

## What's REAL vs PAPER (read before you model failures)

The Q02 review is of the **architecture/design** (overview.md + ADRs); the Phase-1 spike (sealed `rat/2.0`) shows how much is built. For your failure analysis:

**Real + enforced (Phase 1):**
- **Reconciliation-as-source-of-truth** — level-triggered convergence; events are hints (`core/reconciler`).
- **Crash-loop backoff + jitter + cap** — a failing plugin is restarted on an exponential, capped schedule and parked in `Degraded` (it can't hammer the runtime). *(sre#4 — directly from reviews/03.)*
- **Leader election + lease-thrash guard** — single-key CAS lease with a TTL margin + minimum-hold, so transient state-backend latency doesn't ping-pong leadership; failover only on genuine expiry (`core/lease`). *(sre#4.)*
- **Provider-call bounding (C3)** — the gateway bounds each call by `min(channel, deadline)` + a stream idle-timeout, so a hung provider can't pin the loop/gateway.
- **Forensic trail (C4)** — every enforcement decision (incl. denials) + a terminal stream-close record are audited. *(Caveat: records are **unsigned** in the spike — tamper-evidence is GA.)*
- **Trace/correlation in the wire** — W3C `traceparent` + `correlation_id` are mandatory on every RPC and the audit trail.

**Still paper (the SRE-load-bearing gaps — assume NOT built):**
- **Native core `/metrics` + OTel + an SLO doc** (sre#8, backlog) — the reconciler exposes an in-memory `Status()` hook but no endpoint; no published golden signals/SLOs.
- **`rat diagnose <run_id>`** — no cross-plugin causal-timeline tool.
- **Reconcile-loop fairness** — the crash-loop cap stops one *failing* plugin; a slow-but-healthy plane can still starve the single-leader loop (no per-pipeline budget).
- **Resource-limit enforcement** — `LaunchSpec` carries `requests`/`limits`, but the runtime doesn't map/enforce them yet (a runaway plugin can OOM the host).
- **Capacity model, upgrade/version-skew procedure, DR/backup, incident runbooks, bus-liveness alerting, multi-region HA** — none exist.
- **The event bus + the durable state-backend** are frozen *contracts*; the spike core built the reconciler + an in-memory lease store, not the bus or a durable desired-state store.

## The failure domain

| operability surface | status | your job |
|---|---|---|
| reconcile convergence (level-triggered) | ✅ real | is the model + its guarantees sound? |
| crash-loop backoff / lease-thrash guard | ✅ real (sre#4) | validate the design — don't re-flag as missing |
| state-backend (desired-state + lease) | ⚠️ tier-0 SPOF, partly built | is "documented tier-0" enough, or does the design need a degraded mode? |
| core self-telemetry + SLOs | ✗ paper | what golden signals + SLOs must ship before prod? |
| diagnosability across N polyglot plugins | ⚠️ trace context only | is `rat diagnose` a must-have? |
| capacity / single-leader loop scaling | ✗ paper | where does one loop blow its interval? shard when? |
| upgrade / version skew | ✗ paper | ordering, rolling, migrations, rollback |
| DR / backup consistency | ✗ paper | a consistent backup set across state + bus + plugins |
| reconcile fairness + resource limits | ⚠️ cap only | per-pipeline budget? runtime-enforced limits? |

## Failure-mode & operability questions (the heart)

**A — The tier-0 state-backend SPOF.** The "everything is a plugin" framing hides that the state-backend is a hard dependency: it holds desired state *and* the lease. If it's down/slow, the core can't read desired state *or* renew the lease → leader steps down → no replica re-acquires → **whole control plane wedged** (in-flight data-plane work survives — the data plane is independent). Is documenting it as tier-0 ("its HA *is* the platform's HA") enough, or does the design need a read-only degraded mode / lease-vs-state separation? Is sre#4's lease-thrash guard tuned right for *real* backend latency profiles (postgres, DynamoDB-strong-reads)?

**B — Diagnosability.** "Why didn't my pipeline run?" spans the reconciler + bus + N polyglot plugins. Mandatory `traceparent` + `correlation_id` are now in the wire. Enough to reconstruct a causal timeline, or is a `rat diagnose <run_id>` contract (fan-out a diagnostic RPC to every plugin that touched the run) a prod blocker? What's the minimum diagnosability bar for a 50-person team?

**C — Native observability + SLOs (paper).** Observability is a *plugin* — so the core's own signals (reconcile latency, lease churn, per-plugin RPC error rate, bus lag) exist only if someone installs one, and two plugins may emit incompatible metrics. The plan: the core emits `/metrics` + OTel **natively** + a mandatory plugin metrics contract (RED) + a published SLO doc. Right call? What's the minimal golden-signal set + SLOs to ship before multi-tenant?

**D — Capacity & the single-leader loop.** The reconciler is single-leader, O(declared pipelines × 1/interval). At what pipeline count does one loop blow its interval? The crash-loop cap parks *failing* plugins, but a *slow* plane still consumes loop budget — is the single loop + `Degraded` circuit-breaker enough, or are sharded reconcilers / per-pipeline budgets needed, and at what scale? What capacity formula (reconcile cost, state-backend IOPS, bus sizing, per-plugin RSS) does an operator need to size a deployment?

**E — Upgrade & version skew.** Independently-versioned polyglot plugins. The *contract* is versioned; the *procedure* isn't. core vN+1 expects `engine/v2` but the installed engine speaks v1 — refuse, or silent 404? Can vN and vN+1 core replicas coexist behind the LB during a rolling upgrade? State-schema migrations — who owns them, reversible, rollback-safe? Is a kubelet/apiserver-style skew policy + a `preflight` check the right model?

**F — DR / backup.** Durable truth is split across the state-backend + the event log (durable → it *is* state) + plugin-local config. Is there a consistent backup *point* across them (a `pg_dump` at T1 + a bus snapshot at T2 restore inconsistently)? RPO/RTO targets? Is "desired state is GitOps-able plain YAML" the right strongest-DR posture?

**G — Reconcile fairness & resource limits.** sre#4 stops a *crash-looping* plugin from hammering the runtime. Still open: a slow-but-healthy plane starving the single loop (per-pipeline work budget/timeout), and resource-limit enforcement (a runaway plugin OOMing the host). Must-haves before multi-tenant? Is "mandatory `resources{requests,limits}` + the deployment-runtime enforces them as a precondition" the right design?

**H — The failure-mode catalog.** Walk the architecture-specific modes and tell us which fail *loud*, which fail *silent*, which *wedge* the platform: bus (NATS) down (events dark — does anyone alert?); state-backend down (→ A); plugin crash-loop (✅ sre#4); lease thrash (✅ sre#4); eviction storm (runtime evicts under memory pressure + the reconciler respawns → thrash). Which need runbooks vs design changes?

## What sre#4 already settled (please don't re-flag as missing)

Crash-loop **backoff + jitter + cap** and the **lease-thrash guard** (TTL-margin + min-hold) + clean **failover** are built and tested (deterministically + end-to-end against a real crash-looping plugin). Validate the *design*; if you'd tune the knobs (backoff curve, TTL margin, cap) for real workloads, that's exactly the feedback we want.

## Already acknowledged (don't flag as novel)

Native `/metrics`/SLOs (sre#8); `rat diagnose`; reconcile fairness; resource-limit enforcement; capacity model; upgrade/DR/runbooks; multi-region; unsigned audit records; the bus + durable state-backend being contracts-not-yet-core. These are known and booked — we want your read on *priority and sufficiency*, not just their existence.

## Materials & reading order (SRE-relevant)

1. This brief + the failure-domain table.
2. [reviews/03-operations-sre.md](03-operations-sre.md) + [reviews/board/sre.md](board/sre.md) — the internal SRE review + its Failure Mode Catalog (challenge it; tell us what it missed and what it over-weighted).
3. `docs/architecture/overview.md` §reconciliation, §scalability, §HA (leader/lease, D5); [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md) (tier-0 + the six things).
4. The built reliability surface, in `core/`: `reconciler/` (convergence + backoff/jitter/cap + `Status()`) · `lease/` (CAS + thrash guard + failover) · `gateway/gateway.go` (C3 bounding, C4 audit).
5. `roadmap/backlog.md` (sre#4/#8 + the deferred operability items) + `roadmap/done.md` (sre#4).

## Findings & logistics

Same format + logistics as the [main brief](Q02-external-review-brief.md#how-to-deliver-findings): per-finding {severity · area · finding · why-it-matters · suggested-direction}, plus a bottom line — *would you run this in production / carry its pager, and what's the one operability gap you'd close first?* A **Critical** = "I would not run this in production until this is resolved." A focused 1–2 day read is plenty; unpublished + confidential.
