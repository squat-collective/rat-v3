---
date: 2026-05-30
participants: [Tom, Claude Opus 4.7]
topics: [vision, plugin-axes, decoupling, manifest-based-contracts, deployment-topologies, k8s-pattern]
key_decisions:
  - Founding decision: the platform is a 6-thing core + everything-else-as-plugin. [ADR-001](../architecture/adrs/001-everything-is-a-plugin.md)
  - State backend, auth, scheduler, deployment-runtime, tenancy, billing, UI — all plugin axes.
  - 16-19 axes at v1, open-set thereafter.
  - Contract triple = proto files + plugin.yaml manifests + URI-shaped capability strings.
  - Reconciliation model (K8s pattern), not imperative orchestration.
  - Same binary for solo / team / hybrid / full-cloud — composition differs, core doesn't.
  - K8s-shaped LCD for deployment-runtime contract; docker default; k8s as drop-in swap.
key_questions_opened:
  - Q01 — Core language: Rust or Go?
  - Q02 — Event bus default: NATS / Redis Streams / Kafka / build-our-own?
  - Q03 — Manifest validation: standalone JSON Schema or generated from proto?
  - Q04 — Capability versioning: SemVer ranges or flags?
  - Q05 — Reconciler durability: leader+lease or active-active with optimistic concurrency?
  - Q06 — Plugin manifest source-of-truth: in-image / separate registry / operator-side?
  - Q07 — v2-to-v3 migration: bridge plugins, hard cutover, or dual-API transitional core?
  - Q08 — Cross-plane queries: federation engine plugin or always materialize?
  - Q09 — Marketplace governance: pure community / curated / both?
  - Q10 — Licensing: Apache 2.0 / MIT / AGPL?
status: distilled
---

# The vision conversation — 2026-05-30

## Context

Tom and Claude had been working through RAT v2's decoupling roadmap — landing ADR-024 (storage/catalog/engine as plugin axes), drafting ADR-025 (on-demand decoupled planes), drafting ADR-026 (plugin capability manifests). Each ADR pushed v2 further toward "more decoupled, more pluggable." Reading them in sequence, a pattern emerged: every direction we moved, the answer was "make it a plugin."

Tom then asked: *what kind of platform is this for data teams and data solo devs?* — and after Claude's honest assessment (sweet-spot for data teams; overscoped for solo devs unless defaults stay magical; risk is complexity ceiling vs marketing reality) — Tom asked the bigger question:

> *"If you had to design this idea from scratch with more scalability (no mandatory language to use (contracts are the key), easier core architecture, and other ideas) what will you do? Really think from scratch and not from the current codebase, think about something really scalable as a data platform that could fit any team based on its extensibility and interoperability, that could be able to run fully self hosted as well as partially on cloud as well as fully cloud. Something where those kind of decisions are plugins too, where RAT simply becomes the core that orchestrates those plugins together. How will you design it?"*

This conversation is the response to that question. It went deep enough that Tom decided to spin up `~/sandbox/rat/` (this repo) to capture the design.

## The shape that emerged

### The premise: the core does six things

Strip down the question "what does a data platform control plane actually have to do itself?" The answer is small:

1. **Registry** — knows what plugins exist, indexes by `(kind, name, version)`.
2. **Identity gateway** — delegates validation to an auth plugin.
3. **State gateway** — delegates persistence to a state-backend plugin.
4. **Event bus** — async pub/sub for coordination + observability.
5. **Reconciler loop** — desired-state ↔ actual-state convergence (K8s pattern).
6. **API gateway** — gRPC + REST entry point.

These six are *irreducible* — each one is the chicken to a plugin's egg. Total core code budget: 5-10k LOC (comparable to etcd, NATS). Probably Rust or Go.

**Everything else is a plugin.** That's the founding rule.

### The 16-19 axes

Open-set `kind:` in the manifest. At v1 we ship:

**Data plane (7):** engine, runtime, format, strategy, catalog, storage, deployment-runtime
**Control plane (7):** state-backend, secret-backend, scheduler-backend, identity, tenancy, billing, observability, audit-log (8 actually — undercount in the conversation)
**Experience (3):** ui, notifications, marketplace (and ide-extension as a sub-kind)

Two key splits from v2's thinking:
- **Engine vs Runtime** — DuckDB/ClickHouse are *engines* (SQL → Arrow); PyArrow/Polars are *runtimes* (Arrow → Arrow ops). Strategies use runtime, not engine, for the merge/scd2/etc work — engines are invoked only for the source SQL transform. This finally makes strategies format-agnostic.
- **State-backend is a plugin axis** — postgres becomes the default, not the mandatory choice. Sqlite for solo, dynamodb for cloud-native, etcd for k8s-native.

### The contract triple

Three orthogonal contracts together define the platform's wire shape:

1. **`.proto` files** — gRPC service contracts per axis. Generated SDKs in Go, Python, Rust, TypeScript, Java published by core team. *Plugins in any language.*
2. **`plugin.yaml` manifest** — `plugin/v1` schema, JSON-Schema-validated, one shape for every plugin kind. `provides` / `requires` / `suggests` / `contributes`.
3. **URI-shaped capability strings** — `rat://format-capability/v1/merge`. Naturally namespaced, versioned, lookup-able. Open-set.

This means a Rust plugin written by a 3rd party can be installed into a self-hosted RAT and immediately interoperate with a Python plugin written by the core team. The contract is the only shared thing.

### Communication: sync gRPC + async event bus, dual-use

- **Sync gRPC** for request-response with strong typing (`format.Resolve(ref)`, `catalog.GetTable(ref)`).
- **Async event bus** for coordination + observability (`pipeline_run_started`, `plane_warmed`).

Both pluggable backends (gRPC over HTTP/2 vs Connect-over-HTTP/1.1; bus over NATS / Kafka / Redis Streams).

**Data plane bypasses the core for bytes.** Engine ↔ Storage = direct S3 traffic. Engine ↔ Format = small RPCs (file lists). Runner never sees data. Core never sees data. The core is a coordination point, not a chokepoint. **At runtime, 99% of data work never touches the core.**

### The reconciliation model

Kubernetes controller pattern, applied to data:

```
Operator declares: planes, pipelines, subscriptions.

Reconciler loop (every N seconds):
  - For each declared pipeline:
      - If scheduled to run now: check plane is healthy.
        If not: ask plane-manager to spawn missing axes.
        Emit `pipeline_run_requested` event.
  - For each subscription: check trigger → emit action event.
  - For each plugin process: verify healthcheck → restart if needed.

Plugins react to events:
  - Engine subscribes to `pipeline_run_requested` for its plane → runs work.
  - Observability subscribes to `*_completed` → pushes metrics.
  - Notifications subscribes to `*_failed` → alerts Slack.
```

**The core never tells anyone to do anything.** It maintains the truth of "what should be true" and lets plugins react. This is more scalable than imperative orchestration because plugins can be added/removed/replaced without core changes.

### Deployment topologies as plugin compositions

The same core, four very different deployments — each a different set of installed plugins:

**A. Embedded / solo (single binary, laptop)** — sqlite state, in-process scheduler, local-fs storage, embedded duckdb engine, web-portal UI. 30MB binary, boots <1s, no docker, no cloud. `chmod +x ./rat`.

**B. Self-hosted team** — postgres state, docker deployment-runtime, S3/MinIO storage, Iceberg+Nessie, OIDC auth, Prometheus observability. Today's RAT.

**C. Hybrid (on-prem control, cloud data)** — postgres on-prem, AWS-SM secrets, k8s deployment-runtime, S3/Glue/Spark-on-EKS, Datadog observability.

**D. Full cloud (SaaS-style)** — dynamodb state, AWS-SM, AWS-Fargate, Snowflake/BigQuery engine choice, Auth0, Intercom + Slack notifications, multi-tenant with hierarchical isolation, usage + seat billing.

**Same `rat` binary for all four.** Different plugin sets. The same OSS codebase serves solo devs and multi-tenant cloud SaaS. **No fork between freemium and enterprise.**

### The IDE as a plugin

Today RAT's portal is The UI. In the new design, it's just *one* `ui` plugin. Implications:
- CLI lover? Install `rat-ui-cli`.
- VS Code shop? Install `rat-ui-vscode-extension`.
- Slack-driven team? Install `rat-ui-slackbot`.
- All at once? Sure. They share the same core API.
- Custom UI for your org? Build one against the API contract.

**You don't have to like RAT's UI to use RAT.** Today's RAT requires loving the portal because there's no alternative.

### Tenancy, billing, auth — all plugin axes

The three things every "make this enterprise-ready" effort historically forks the codebase for. Plugins from day 1:
- **Single-tenant + anonymous + free**: install nothing. Default state. Solo + small teams.
- **Multi-tenant + OIDC + usage-billed**: install three plugins. Suddenly RAT is SaaS-ready.
- **Custom enterprise compliance**: write your own audit/tenancy/identity plugins. Core unchanged.

**Same codebase serves freemium-open-source-self-hosted AND multi-tenant-billed-SaaS.** Different plugin sets. No fork.

### Scalability rewritten

Three scales it solves simultaneously:
- **Solo** — single binary, in-process plugins, sqlite. 30MB.
- **Team (10-100 users)** — core + postgres + plugin containers. GB-TB tables, tens of pipelines/sec.
- **Enterprise (1000+ users, multi-tenant, hybrid)** — core replicated behind LB; state in dynamodb or sharded postgres; plugins span on-prem + cloud. PB tables, thousands of concurrent pipelines.

**Core itself never changes between scales.** Plugin composition changes.

**Core stays stateless** (state in state-backend plugin) → horizontally scalable. N core replicas behind a load balancer; reconciler loop uses optimistic concurrency on shared state (K8s controller pattern).

### Honest risks

Tom asked for the challenge, not just the validation. The risks Claude flagged:

1. **Six-thing-core discipline is hard.** Every quarter someone wants to add a 7th thing. Without discipline, the core creeps and the whole model breaks.
2. **Plugin ecosystem chicken-egg.** With 16 axes, you need plugins for every axis to ship a working RAT. Heavy upfront cost in first-party reference plugins.
3. **"Deploy is a plugin combination" pushes complexity to operators.** Mitigation: ship bundles (`rat-bundle-solo`, etc.) — curated defaults that hide composition.
4. **Reconciler model is harder to debug than imperative.** Need really good `rat diagnose` tooling from day 1.
5. **Event bus is now critical path.** If it breaks, nothing works. Defaults must scale to ~10k events/sec without operator tuning.
6. **Marketing surface gets harder.** "Most extensible data platform" is vague. Lead with deployment topologies (solo, team, cloud); let architecture be a quiet credibility moat.
7. **A real rewrite — not an evolution.** Current RAT can't incrementally become this. v2 continues in parallel; v3 is a 12-18 month bet.

### What Claude recommended doing first

If sitting down to start tomorrow:
- **Week 1-2:** spec the contracts. Just `.proto` + `plugin.yaml` schema. Peer-reviewed by people who've built plugin systems (OSGi, K8s, VSCode contributors).
- **Month 1:** build the core. ~8k LOC. The six things. No plugins yet.
- **Month 2:** reference plugins for solo deployment. Goal: `rat init && rat run my-pipeline.yaml` works.
- **Months 3-4:** reference plugins for self-hosted team. Match v2 capabilities.
- **Months 5-6:** hardening + migration tooling from v2.
- **Month 7+:** ecosystem moves (marketplace, hybrid/cloud topologies, multi-UI).

## Concrete artifacts produced

This conversation produced:
- The repo this lives in (`~/sandbox/rat/`).
- [`docs/vision.md`](../vision.md) — the thesis.
- [`docs/architecture/overview.md`](../architecture/overview.md) — the integrated architecture.
- [`docs/architecture/adrs/001-everything-is-a-plugin.md`](../architecture/adrs/001-everything-is-a-plugin.md) — the founding ADR.
- Multiple seed entries in [`ideas/inbox.md`](../../ideas/inbox.md).

Future work it foreshadows (each will become an ADR):
- Q01-Q10 in ADR-001's open questions — each becomes its own ADR when it's time to commit.
- Contract triple specs — proto files + manifest JSON Schema. Phase 0 of implementation.
- Migration ADR from v2 — Q07.
- License decision — Q10.

## Open threads

These came up but weren't deeply resolved; future conversations:

- **Should we make the core's reconciler driveable by external controllers?** Like K8s lets you write Operators that run alongside controllers. Could open the door to "RAT-managed by Crossplane" or similar. Worth investigating.
- **What's the right SDK story?** Generated SDKs in 5 languages is a lot to maintain. Alternative: ship a generator + a minimal example per language; let community own the rest. Trade-off between adoption velocity and maintenance burden.
- **Plugin signing + supply chain.** When operators install 3rd-party plugins, how do they trust them? Sigstore? Notary v2? In-marketplace review? The answer affects how aggressive we can be about the "easy install" story.
- **Performance benchmarks before we lock the contract.** A Phase 0 spike should measure the per-RPC overhead of the contract triple. If it's too high (>10ms per format.Resolve), the whole "always go through the registry" model is in trouble.
- **The "decorator-based multi-impl dispatch" temptation.** Tom raised this during the ADR-026 discussion. Claude pushed back: decorators are code-style sugar, not architecture. Worth a follow-up ADR to formalize the rejection so it doesn't creep back in.

## Tone notes

This conversation was *unusually* generative. Tom asked the right question at the right time — after multiple weeks of incremental ADR work on v2, he stepped back and asked "what if?". The answer that emerged surprised both of us in scope but felt obviously correct in shape.

A few key moments:
- Claude flagged the engine-vs-runtime split as an under-appreciated distinction. Tom didn't push back; this became one of the load-bearing decisions.
- Tom proposed the decorator-for-multi-impl pattern. Claude challenged it firmly as code-style-not-architecture. Tom accepted the pushback. (Worth noting because it's the kind of moment where soft challenges get accepted as half-decisions; this one was clearly rejected.)
- Tom referenced "kinora-v2" as prior art for capability declarations. Claude didn't have full context but extrapolated from common manifest patterns (OSGi, VSCode, Cargo). This worked but it's a reminder: when Tom cites internal prior art, ask for the details.
- The vision ended with "would I build this? Yes, but with these specific bets." Tom's response — *spin up a sandbox and capture everything* — was the right move. Architecture conversations of this scope evaporate unless committed to disk.

This is the founding conversation. Future ADRs trace back here.
