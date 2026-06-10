# ADR-046: Native observability — core `/metrics` + durable audit, dependency-free

## Status: Accepted (2026-06-10)

## Context

The code-level review of `core/` found **gap #6**: the core was effectively blind in production.

- **No metrics.** Nothing exposed call rates, allow/deny counts, or plugin health. The first
  operational question — "are calls being denied? is a plugin down?" — was unanswerable without
  attaching a debugger or grepping logs.
- **Audit was not durable.** The gateway audits every C5 decision (good — it always has), but the
  daemon's sink wrote only to stdout / an in-memory slice. Restart the process and the decision trail
  is gone; nothing persisted.

This is the SRE review's standing point and the `plugin-architecture.md` "cross-cutting concerns"
list, which already says these belong in the **core's enforcement layer**, not the observability
plugin:

> *Mandatory audit emission (even when no audit-log plugin installed); native observability
> (`/metrics` + OTel spans independent of any observability plugin).*

So this gap is not new scope — it is implementing a property the founding rule already requires of the
existing six things. An observability-axis plugin layers *richer* telemetry on top; the core must be
observable on its own first.

## Decision

**Ship core-native, dependency-free observability: a `/metrics` Prometheus endpoint fed by the
gateway and reconciler, and a durable append-only audit log — both present without any plugin.**

### 1. A tiny in-house metrics registry (no dependency)

`core/metrics` is ~150 lines: cumulative **counters** (pushed as events happen) and **gauge funcs**
(collected at scrape — the pull model), rendered in Prometheus text-exposition format. No
`prometheus/client_golang`, no OTel SDK — the format is small and stable, and the six-thing-core
discipline says don't pull a library in for ~100 lines. (OTel spans + histograms are a deliberate
additive follow-on, not v1.)

### 2. The core feeds it through narrow, optional hooks

- **Gateway** gains an optional `OnCall(capability, outcome)` hook (nil-safe; no dependency on the
  metrics package). It fires once per authorization+selection decision with the outcome — `allow` |
  `permission_denied` | `selection_failed` | `invalid_trace` — driving
  `rat_gateway_calls_total{capability,outcome}`.
- **Plugin health** is a gauge func the daemon registers, pulling live state from the control plane at
  scrape: `rat_plugin_up{plugin,kind}` = 1 when Healthy else 0. Pull-model means no reconciler hook
  and no staleness.

### 3. Served at `/metrics`, opt-in by address

`RAT_METRICS_ADDR` (e.g. `:9090`) starts the endpoint. Blank → disabled. It is opt-in *by port*, not
by capability: the metrics always exist, but binding a fixed port by default would collide when many
daemons share a host (the unix-socket-per-project model, ADR-023). Drained cleanly on shutdown.

### 4. Durable, mandatory audit

The daemon's audit sink always tees to stdout (container logs) and, **when the project has a runtime
dir**, also appends JSONL to a durable `<.rat>/audit.jsonl` that survives restart. No project (raw
`rat serve --plane`) → stdout only. The gateway's mandatory emission is unchanged; this just stops the
trail from dying with the process. (The frozen `common/v1.AuditRecord` with the core signature + hash
chain remains the GA sink; this is the honest durable minimum.)

## Consequences

### Positive

- **The core is observable with zero plugins.** "Are calls denied / is a plugin down?" is answerable
  from `/metrics`; the decision trail persists across restart. The property `plugin-architecture.md`
  requires now actually holds.
- **No new dependency, no new core thing.** The gateway/reconciler emit signals about jobs they
  already do; metrics is an internal package, not a 7th responsibility. Counters/gauges are nil-safe,
  so an un-wired core never panics.
- **The metric names + audit JSONL shape are a stable surface** an operator/dashboard can build on,
  recorded here so they don't drift silently.
- **Composes with the prior fixes.** Outcomes include `selection_failed` (ADR-045) and the health
  gauge reads the gap-#4 status snapshot via the control plane.

### Negative / costs

- **Metrics are opt-in by port.** Default-off avoids host port collisions but means an operator must
  set `RAT_METRICS_ADDR`. A default-on auto-port (advertised via the daemon registry like the gateway
  callback) is a reasonable later refinement.
- **No spans/traces/histograms yet.** Counters + health gauges are the golden-signal 80%; latency
  histograms and OTel span export are additive follow-ons (the wire already carries `traceparent`).
- **Audit durability is a flat append file, not rotated/signed.** Good enough to not lose the trail;
  rotation + the signed hash-chain (`common/v1.AuditRecord`) are GA, on the audit-log axis.
- **In-house exposition code to maintain.** A small, stable surface — the tradeoff for zero deps.

## Alternatives considered

- **Pull in `prometheus/client_golang` + the OTel SDK.** The full-featured path, but a heavy
  dependency for what the core needs today (counters + a health gauge). Rejected for v1 on the
  minimalism discipline; revisit if/when histograms + span export land.
- **Leave metrics to the observability-axis plugin.** Rejected: `plugin-architecture.md` is explicit
  that native observability is a correctness condition of the core, not optional — a platform whose
  basic health depends on installing a plugin fails silently when you don't.
- **Push plugin states from the reconciler.** Rejected in favor of a scrape-time gauge func — the pull
  model avoids a reconciler hook and can't go stale.
- **Audit only to stdout (status quo).** Rejected: restart loses the trail; durability is the point.

## Related

- [`.claude/rules/plugin-architecture.md`](../../../.claude/rules/plugin-architecture.md) — "Cross-plugin concerns": native observability + mandatory audit as correctness conditions of the six.
- ADR-031 — durable `/data` mount (the same runtime-dir durability the audit file rides).
- ADR-044 — the reconciler status snapshot the `rat_plugin_up` gauge reads (via the control plane).
- ADR-045 — the `selection_failed` outcome the call counter records.
- reviews/03 (operations-sre) — the "stay observable / don't silently make critical things optional" thesis.
- Future: OTel span export + latency histograms; rotated, signed audit (`common/v1.AuditRecord`, audit-log axis).
