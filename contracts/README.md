# RAT v3 contracts (`rat/1`)

The **contract triple** — the entire surface a plugin author builds against:

1. **Manifest** — [`schema/plugin.v1.json`](schema/plugin.v1.json) (JSON Schema 2020-12). The operator/author-editable description of a plugin.
2. **Proto** — `proto/` (one `.proto` per axis; sub-phase 0b, not yet written). The gRPC wire contract.
3. **Capability URIs** — `rat://<axis>/<major>/<capability>`. The only coupling between plugins.

This directory is **Phase 0** work (see [`../roadmap/phases.md`](../roadmap/phases.md)). Nothing here is frozen until the `rat/1` freeze gate (sub-phase 0h) — until then, everything is draft and may change without ceremony.

## Layout

```
contracts/
├── README.md              # this file
├── buf.yaml               # buf module + lint(STANDARD) + breaking(FILE) config
├── buf.gen.yaml           # codegen config (Go wired; other SDKs in 0d/0e)
├── .gitignore             # excludes generated gen/ SDKs
├── schema/
│   ├── plugin.v1.json     # the manifest envelope schema (sub-phase 0a) ✅
│   └── README.md          # schema design notes + the per-kind decision
├── proto/                 # axis service contracts (sub-phase 0b)
│   └── rat/
│       ├── common/v1/
│       │   ├── context.proto   # C1: RequestContext (trace+identity+tenant) ✅
│       │   └── data.proto      # shared TableRef / ArrowStream / WriteResult ✅
│       ├── engine/v1/engine.proto      # Execute/Query/Preview ✅
│       ├── runtime/v1/runtime.proto    # Execute (streaming) ✅
│       ├── format/v1/format.proto      # Resolve/Write/Maintain ✅
│       ├── strategy/v1/strategy.proto  # Apply ✅
│       ├── catalog/v1/catalog.proto    # GetTable/CreateBranch/MergeBranch ✅
│       ├── storage/v1/storage.proto    # VendCredentials (C7 scope) ✅
│       ├── state/v1/state.proto        # Get/Put/List/Watch (tier-0, C3) ✅
│       ├── identity/v1/identity.proto  # Authenticate/Authorize (C2) ✅
│       └── tenancy/v1/tenancy.proto    # Decide (C7 structural) ✅
└── examples/
    ├── rat-strategy-scd2.plugin.yaml     # canonical valid manifest
    ├── rat-format-deltalake.plugin.yaml  # second valid manifest (signed)
    └── INVALID-examples.md               # negative test vectors
```

## Status

| Sub-phase | Artifact | Status |
|---|---|---|
| 0a | Manifest envelope schema (`plugin.v1.json`) | ✅ draft |
| 0a | Example + negative manifests | ✅ draft |
| 0b | 9 axis protos (format, runtime, strategy, engine, catalog, storage, state, identity, tenancy) | ✅ draft — buf lint + build + generate clean |
| 0b | Remaining ~11 axis protos | ⬜ not started |
| 0c | Cross-cutting protos (`common/v1/context.proto` ✅, audit envelope ⬜) | 🔶 context+data drafted |
| 0d–0e | 12 reference implementations | ⬜ not started |
| 0f | Conformance harness + `rat plugin validate` | ⬜ not started |
| 0g | Per-axis `CONTRACT.md` | ⬜ not started |
| 0h | `rat/1` freeze | ⬜ not started |

## Validating the contracts (container-only, per Tom's rule)

**Protos** — lint + compile via buf. The image needs a writable HOME and
`--userns=keep-id` so its cache dir is writable:

```bash
cd contracts
podman run --rm --userns=keep-id -e HOME=/tmp -e XDG_CACHE_HOME=/tmp/.cache \
  -v "$PWD:/workspace:Z" -w /workspace docker.io/bufbuild/buf:1.47.2 lint
podman run --rm --userns=keep-id -e HOME=/tmp -e XDG_CACHE_HOME=/tmp/.cache \
  -v "$PWD:/workspace:Z" -w /workspace docker.io/bufbuild/buf:1.47.2 build
```

Both pass clean (0 findings) as of 0b. `buf generate` (codegen) additionally
needs network for the remote plugins on buf.build — deferred to 0d when the
reference plugins are built; the SDKs are git-ignored build artifacts.

**Manifests** — no `rat plugin validate` yet (sub-phase 0f). Any JSON Schema
2020-12 validator works; we used a containerized `python:3.12-slim` + `jsonschema`
to confirm the two example manifests pass and the negative vectors are rejected.

## Critical concerns baked in (from [`../reviews/00-synthesis.md`](../reviews/00-synthesis.md))

The synthesis flagged 10 wire-breaking concerns to bake in *before* freeze. The
ones that touch the **manifest** are in `plugin.v1.json` from day one:

- **C4 — resource asks/limits:** `resources` block, **mandatory**.
- **C5 — capability enforcement:** `provides` is what the gateway enforces at runtime (declared = enforced). The manifest is the source of that declaration.
- **C8 — supply-chain trust:** `trust` block (signature + signed_by + attestations); optional at solo, required at team+.

The remaining Critical concerns (C1 trace context, C2 plugin-auth, C3 state
namespacing, C6 conformance, C7 tenancy, C9 two-reference, C10 listener split)
live in the protos (0b/0c) and the core (Phase 1), not the manifest — tracked in
[`../roadmap/backlog.md`](../roadmap/backlog.md) as prospective ADRs 004–013.

## Related

- [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D3 (JSON Schema for manifests), D4 (capability major-versioning).
- [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) — the two-reference freeze gate.
- [docs/architecture/overview.md](../docs/architecture/overview.md) — the contract triple section this schema formalizes.
- [reviews/02-plugin-ecosystem-builder.md](../reviews/02-plugin-ecosystem-builder.md) — the author-surface gaps this Phase-0 work closes.
