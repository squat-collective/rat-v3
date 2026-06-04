# Prior art

Patterns we're learning from. We're not inventing the contract-manifest-registry pattern; we're applying a mature pattern from adjacent ecosystems to data orchestration.

## Each entry follows

```markdown
# <System name>

## The claim
What this system does well (or what we're stealing).

## How they do it
The mechanism. Concrete enough to compare to our design.

## What we adopt
The specific pattern we're applying to RAT v3.

## What we don't adopt
The bits that don't fit (and why).

## References
Links to docs / source / talks.
```

## Written up

- ✅ [**Package managers — the local catalog problem**](package-manager-local-catalogs.md) — Docker, containerd, npm, pip, Cargo, apt, Homebrew, focused on how each answers "what do I have, locally" fast + offline. Prior art for the local marketplace ([ADR-028](../../docs/architecture/adrs/028-local-marketplace-cached-index.md)).

## Systems on the list (write-ups coming)

### Plugin / extension models
- **OSGi (Java)** — `MANIFEST.MF`, `Provide-Capability`, `Require-Capability`. Real-world test of the manifest pattern at enterprise scale.
- **VSCode** — `package.json` extension manifest, `contributes`, `extensionDependencies`, `engines.vscode`. Cleanest modern adoption of the pattern.
- **K8s + CRDs** — extending an orchestration platform through declarations. The pattern of "describe what you want, controllers reconcile."
- **Cargo features** — Rust's compile-time capability flags. Lesson: even at compile time, capability flags work.
- **npm peerDependencies** — JS ecosystem's lesson: peer deps are about negotiation, not strict resolution.
- **Chrome Manifest v3** — browser refuses to load unsatisfied manifests. Strict enforcement model.

### Coordination / orchestration
- **Kubernetes** — controllers + CRDs + the API server pattern. Our reconciler model is literally K8s for data.
- **etcd** — small core (~50k LOC) doing one thing (consensus + watch). Inspiration for "core stays small."
- **NATS** — pub/sub with pluggable durability. Inspiration for the event bus axis.
- **Temporal** — workflow orchestration with pluggable persistence + activity workers. Adjacent design space.

### Data ecosystem (what we're competing with / learning from)
- **dbt** — model-as-data-product, adapter pattern for warehouses. Closest peer in semantics; very different in architecture (no runtime).
- **Airflow** — workflow scheduling, plugin model. Lesson: don't be a Python monolith.
- **Databricks** — vertically integrated platform. The thing we explicitly aren't.
- **Snowflake Native Apps** — apps that run inside the warehouse. Interesting model; locked to one engine.
- **Estuary Flow** — schema-first streaming + batch. Recent entrant; check what they do well.
- **Materialize** — streaming SQL with pluggable sources. Schema design relevant.

### UI / extensibility
- **Eclipse RCP** — extension points pattern (predecessor of much of VSCode).
- **WebExtensions API** — cross-browser extension contract. Cross-vendor agreement worked.

## How to use this directory

Each entry stands alone. When designing a feature in RAT v3, read the relevant entries first. Cite them in ADRs. This is how we avoid re-inventing 30 years of plugin-system mistakes.
