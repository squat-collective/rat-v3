# Vision — RAT v3

## The thesis

> **A data platform is a minimal control plane that orchestrates self-describing plugins.**

The core does six things — registry, identity gateway, state gateway, event bus, reconciler, API gateway. Everything else is a plugin. Not "extensible via plugins for the experimental stuff" — actually a plugin. Including state backend. Including auth. Including the scheduler. Including the UI. Including tenancy. Including billing.

If you can name a concern in a data platform that *isn't* a plugin in RAT v3, we made a mistake.

## Why this design

The data tooling landscape today is split into two failure modes:

1. **Vertically-integrated, vendor-locked platforms** (Snowflake, Databricks, BigQuery). Beautiful UX. Excellent defaults. You can't take your data and run it on other compute without months of migration. The platform owns you.
2. **Lego-pieces ecosystems** (dbt + Airbyte + Iceberg + Trino + Airflow + a wiki). Decoupled in theory. In practice, the integration tax is enormous, the UX is fragmented, and there's no "platform" — there are six tools and a coordination problem.

RAT v3 picks a third path: **a thin coordinator with thick decoupling at every axis**. State is yours. Compute is yours. Storage is yours. Catalog is yours. Even the UI is yours — pick the one you want or build your own. The platform never makes choices for you that you didn't sign up for.

## What this enables

**Solo data devs:**
`chmod +x ./rat`. Single 30MB binary. SQLite state, embedded DuckDB, local-FS storage, web UI on localhost:8080. No docker, no cloud, no commitments. *The whole platform fits on a USB stick.* If the project grows, swap plugins one at a time — same binary, same conversation, no migration.

**Teams (3-100 people):**
Docker compose or k8s deployment. Choose engine per workload, format per layer, catalog per protocol family. The portal is a real IDE. Quality tests run pre-merge. Branching on every run. *No lock-in, ever.* When a team member joins, they don't need to learn a new stack — they pick the plugins that match what they already know.

**Enterprise / mid-market:**
Multi-tenant via a tenancy plugin. SSO via an identity plugin. Audit via an audit plugin. Billing via a billing plugin. Deploy across hybrid cloud via a deployment-runtime plugin. *Compliance is a configuration, not a fork.* The same OSS codebase that the solo dev runs is what the enterprise runs.

**Platform builders (the long bet):**
Build your own data platform on top of RAT v3. Pick the plugins you need, write the ones you don't have, ship it as your product. RAT v3 is the substrate.

## Why now

Three things converged that made the moment right:

1. **The Iceberg/Delta/DuckLake era proved formats are commoditizable.** Five years ago "the catalog" was Hive Metastore and nothing else. Today there are four catalog protocol families and a dozen implementations. The format war is over; the integration war isn't.
2. **DuckDB + Polars + Datafusion proved that "compute is a library."** You don't need Spark for analytical work anymore; a single-node engine fits 90% of workloads. This makes "mix engine per workload" actually viable.
3. **Plugin manifests are well-understood (OSGi, K8s, VSCode, Cargo, npm).** We're not inventing the contract pattern — we're applying a 30-year-mature pattern to a domain (data orchestration) that hasn't adopted it.

Five years from now, *every* data platform will have something like this. The only question is whether we build it.

## What we're rejecting

These are choices other platforms make that we explicitly don't:

- **A "blessed" engine.** Databricks blessed Spark, Snowflake blessed its own. We bless nothing. DuckDB ships in the default bundle because it's the right default-for-solo — that's a *bundle* choice, not a *platform* choice.
- **A "blessed" catalog.** Same logic. Nessie is in the default bundle today; Lakekeeper, Unity, Glue are equally first-class.
- **A "blessed" storage.** S3 is the default because it's ubiquitous. R2, GCS, Azure blobs, IPFS — all peers.
- **A "blessed" UI.** The web portal is one of N. Slack-bot, CLI, VS Code extension, custom enterprise UI — all peers consuming the same API.
- **A "blessed" deployment runtime.** Docker for solo+team, k8s for scale, lambda/fargate for fully-managed. Plugin axis.
- **A "blessed" auth.** Anonymous for solo, OIDC for teams, SAML for enterprise, custom for special cases. Plugin axis.
- **A "blessed" tenancy model.** None for solo, single-org for teams, hierarchical for enterprise. Plugin axis.

## What we're committing to

These are the things we will *not* compromise on, no matter how convenient:

1. **The six-thing core stays six things.** When someone proposes adding a seventh, we either prove it can be a plugin (and reject the proposal) or write an ADR explaining why this is the rare exception. Expect this to happen ~once per year if we're disciplined.
2. **Every load-bearing concern has a plugin axis.** If a feature can't be expressed as a plugin contract, we don't ship it. Better to defer than to bake in.
3. **Contracts are open-set strings + versioned manifests.** No enums. No closed protocols. No language-specific SDKs that other languages must replicate. The contract is the proto + the manifest schema; everything else is implementation.
4. **Data plane bypasses the control plane.** Bytes never flow through the core. Engines, formats, storage talk directly via descriptors. Core handles orchestration only.
5. **Reconciliation, not orchestration.** Operators declare desired state; the core drives convergence; plugins react. No imperative "do this now" calls from core to plugins. K8s pattern.
6. **The same binary runs every deployment topology.** Solo, team, hybrid, full-cloud — all the same `rat` binary, different plugin compositions. If a deployment topology requires a different core, we broke the model.

## Success criteria (10-year outlook)

We'll know this worked if, in 2036:

- A solo data dev can install RAT v3 in under 60 seconds and have a working pipeline by minute 5.
- A community plugin marketplace has 100+ plugins from 50+ authors, with the manifest model preventing breakage across versions.
- At least 3 commercial data platforms are built *on top of* RAT v3 as their orchestration substrate.
- The core has stayed under 15k LOC despite the ecosystem 10x'ing around it.
- "What is RAT?" has a one-sentence answer: *the orchestrator that doesn't make choices for you.*

We'll know we failed if:

- The core has crept past 30k LOC because we kept "just adding one thing."
- Adoption is concentrated in the default bundle and nobody writes plugins (= we built a worse Snowflake).
- The plugin ecosystem fragments because the manifest schema is too permissive (= we built another OSGi).
- A competitor ships a smaller, cleaner version and eats our lunch (= we hesitated too long).

## The bet

The architectural depth in v3 doesn't pay off for the *first* year. It pays off in years 2-5 when other platforms add their N+1 vendor integration and we add a plugin. By year 3, the gap is structural. By year 5, it's a moat.

This is the kind of bet that's only worth making if you're prepared to spend 12 months on foundations before shipping a useful product. **The architecture is the product** — for a longer-than-comfortable runway.
