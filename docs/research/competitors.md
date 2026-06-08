# Competitors — the data platform landscape

Not "rivals" — *peers in the space*. RAT v3 takes ideas from all of them and explicitly diverges where the pluggability premise demands it.

## Lock-in-by-design (vertically integrated)

### Snowflake / Databricks / BigQuery / Redshift
**What they do well:** UX, defaults, scale, ecosystem (libraries, partners).
**Why we diverge:** the moat IS the lock-in. They can't credibly become extensible-at-every-axis because their business model is "you pay us for the integrated thing."
**What we steal:** the IDE quality bar. Snowsight + Databricks Lakehouse Apps are good UIs; RAT v3's portal needs to compete.

## Open-source-but-opinionated

### dbt
**What they do well:** model-as-code, model-as-data-product semantics, the adapter pattern for warehouses.
**Why we diverge:** dbt is Python-only, single-runtime (`dbt-core`), and assumes you bring a warehouse. RAT v3 is multi-language, multi-runtime, brings everything.
**What we steal:** the semantics of "a pipeline produces a table on a branch with quality tests." That's our pipeline model too.
**Where dbt is weaker:** orchestration (you need Airflow), interactive query (Snowsight/UI is your warehouse vendor's problem), data plane integration (sources via Airbyte/Fivetran).

### Airflow / Prefect / Dagster
**What they do well:** scheduling, DAG management, operator ecosystem, observability.
**Why we diverge:** they're workflow engines, not data platforms. They orchestrate; they don't own the data tier.
**What we steal:** the operator + DAG pattern in the scheduler-backend plugin. Adapter is a thin gRPC bridge.
**Where they're weaker:** they have no opinion about catalogs, formats, engines, storage — that's a feature for them ("you bring those") but RAT v3 wants to *integrate* them, not punt.

### Trino / Presto / Apache Spark
**What they do well:** federated query, mature engines, ecosystem of connectors.
**Why we diverge:** they're engines, not control planes. Their connectors are how they integrate; the platform wrapped around them (e.g., Starburst, EMR) is what we'd compete with.
**What we steal:** the connector pattern internalizes to the engine plugin axis (each engine has its own native connector set; we don't try to unify at the engine level).

## Open-source platforms (closer peers)

### Apache Polaris / Project Nessie / Lakekeeper / Unity Catalog
**What they do well:** open catalog protocols + reference implementations.
**Why we diverge:** they're catalogs, not platforms. We're plugin axes that consume them.
**What we steal:** the iceberg-rest protocol family. Our `catalog` axis is shaped to consume whichever of these is best in a given deployment.

### Apache Iceberg / Delta Lake / Apache Hudi
**What they do well:** open table formats with rich semantics.
**Why we diverge:** they're formats, not platforms. Our `format` axis is shaped to expose any of them.
**What we steal:** the format spec evolution discipline. Iceberg's spec process is mature; we match that for our manifest schema.

### MotherDuck
**What they do well:** "the data warehouse should be where I am" — local DuckDB + cloud sync.
**Why we diverge:** they're a warehouse, we're an orchestrator.
**What we steal:** the "single-binary solo experience" thesis. RAT v3's solo bundle should feel like installing MotherDuck — drop, run, working.

## Adjacent (different problem, related architecture)

### Crossplane (k8s control plane composition)
**What they do well:** composing infrastructure with the K8s pattern.
**Why relevant:** our reconciler model is the same shape, applied to data instead of infra. Worth checking if Crossplane could *be* our deployment-runtime backend.

### Temporal (workflow orchestration)
**What they do well:** durable workflows with pluggable activities + persistence.
**Why relevant:** could be a scheduler-backend plugin. Our scheduler axis should be Temporal-compatible.

### NATS (messaging)
**What they do well:** small core, pluggable persistence, multi-language clients.
**Why relevant:** very likely *the* event-bus default we ship with.

### Pulumi (multi-language infra)
**What they do well:** programmable infra in any language via the same SDK pattern.
**Why relevant:** their multi-language SDK approach is what RAT v3 needs for plugin authors. Worth studying their generator.

## What we explicitly aren't

- **A SaaS-only product.** Self-hosted is a first-class topology.
- **A "blessed-vendor" platform.** No tier-1 vs tier-2 integrations.
- **A workflow engine pretending to be a platform.** We own data semantics (pipelines, runs, branches, quality), not just scheduling.
- **A warehouse pretending to be a platform.** We don't write SQL; we orchestrate engines.
- **A single-language ecosystem.** Plugins in any language.

## Where the bet lies

The platforms above are all on a Pareto frontier: pick deep integration OR pick decoupling, you can't have both. RAT v3 bets that **the manifest-based plugin pattern lets you have both at once** — deep integration through descriptors + capability negotiation; decoupling through axis independence + open contracts.

If the bet is wrong, we end up looking like another "extensible-but-not-actually-used" platform (the OSGi outcome — technically great, ecosystem stillborn).
If the bet is right, we look like K8s for data — universally adopted because it's the substrate everyone else builds on.

The middle path doesn't exist. Architecture decides.
