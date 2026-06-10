# RAT v3 contracts (`rat/1`)

The **contract triple** — the entire surface a plugin author builds against:

1. **Manifest** — [`schema/plugin.v1.json`](schema/plugin.v1.json) (JSON Schema 2020-12). The operator/author-editable description of a plugin.
2. **Proto** — `proto/` (one `.proto` per axis + `common/` cross-cutting + the core's own services). The gRPC wire contract.
3. **Capability URIs** — `rat://<axis>/<major>/<capability>`. The only coupling between plugins.

This surface is **FROZEN** at `rat/1` (Phase 0 sealed at `rat/1.5`; everything ≤ `rat/2.0`
is the frozen wire — see [`../roadmap/current.md`](../roadmap/current.md)). Post-freeze
changes land as **additive, capability-gated amendments** — [`AMENDING.md`](AMENDING.md)
documents the procedure; `state/v1` `delete` (ADR-035) and `create-if-absent` (ADR-049)
are the precedents. `make breaking` enforces the freeze against `main`.

**Writing a plugin against these contracts?** Start with
[`../docs/guides/authoring-a-plugin.md`](../docs/guides/authoring-a-plugin.md), then read
your axis's `CONTRACT.md` (next to its proto).

## Layout

```
contracts/
├── README.md              # this file
├── buf.yaml               # buf module + lint(STANDARD) + breaking(FILE) config
├── buf.gen.go.yaml        # Go codegen template (Python uses codegen/Dockerfile.python)
├── codegen/               # pinned connectionless codegen toolchain (ADR-018): Go + Python
├── .gitignore             # excludes only sdks/go/go.sum (the SDKs themselves ARE committed)
├── schema/
│   ├── plugin.v1.json     # the manifest envelope schema ✅ FROZEN v1 (ADR-011)
│   ├── kinds/             # 18 per-kind schemas layered on the envelope (ADR-011)
│   └── README.md          # schema design notes + the per-kind decision
├── proto/                 # axis service contracts — FROZEN rat/1 (+ additive amendments)
│   └── rat/
│       ├── common/v1/     # context (C1), data, event envelope, audit record, the
│       │                  # capability annotation, + the canonical ERROR_MODEL.md
│       ├── core/v1/       # the core's own wire: CapabilityInvokeService (ADR-005/008)
│       │                  # + ControlService (live register/deregister, ADR-027)
│       └── <axis>/v1/     # one dir per axis (18) — <axis>.proto + CONTRACT.md, the
│                          # per-axis author guide: engine, runtime, format, strategy,
│                          # catalog, storage, state, identity, tenancy,
│                          # deploymentruntime, scheduler, secret, observability,
│                          # auditlog, ui, notifications, marketplace, billing
├── conformance/           # golden vectors per axis + cross-language context-carriage
├── sdks/                  # COMMITTED generated Go + Python SDKs + the hand-written
│   ├── go/                # runtime SDKs: Go `ratplugin` (Serve/Gateway/CallerTenant)
│   └── python/            # + Python `rat/plugin.py` — ADR-029
└── examples/
    ├── rat-strategy-scd2.plugin.yaml     # canonical valid manifest
    ├── rat-format-deltalake.plugin.yaml  # second valid manifest (signed)
    └── INVALID-examples.md               # negative test vectors
```

## Status

| Sub-phase | Artifact | Status |
|---|---|---|
| 0a | Manifest envelope schema (`plugin.v1.json`) | ✅ **FROZEN v1** (ADR-011) |
| 0a | Example + negative manifests | ✅ gated by `make validate-manifests` |
| 0b | 18 axis protos + common | ✅ **FROZEN `rat/1`** (ADR-009; `make breaking` guards) |
| 0b | Per-kind manifest schemas | ✅ 18 in `schema/kinds/` (ADR-011) |
| 0c | Cross-cutting protos (context, data, event envelope, audit, annotations) | ✅ frozen with 0b |
| 0d–0e | Reference implementations | ✅ — [`../plugins/`](../plugins/), 40 plugins; the ADR-003 two-reference gate held for the data-plane axes |
| 0f | Conformance harness + manifest validation | ✅ `make conformance` + `make validate-manifests` + `rat plugin check` (ADR-026) |
| 0g | Per-axis `CONTRACT.md` | ✅ 18 author guides, each next to its proto |
| 0h | `rat/1` freeze | ✅ **SEALED** — Phase 0 closed at `rat/1.5` (2026-06-01) |
| post-freeze | Additive amendments | `state/v1` `delete` (ADR-035) + `create-if-absent` (ADR-049) — procedure in [`AMENDING.md`](AMENDING.md) |

## Validating the contracts (container-only, per Tom's rule)

Everything runs through `make` from the repo root (podman preferred, docker fallback —
nothing installs on the host):

```bash
make check               # FAST per-commit gate: buf lint (seconds)
make verify              # FULL: lint + build + SDK freshness + SDK compile + vector lint + core tests
make breaking            # the freeze gate: buf breaking vs the sealed baseline (main)
make conformance         # every reference plugin against its golden vectors
make validate-manifests  # examples + per-kind schemas + the INVALID corpus
make validate-vectors    # golden-vector lint: envelope schema + per-file key registry
```

Capability discovery is a CLI verb: **`rat capabilities [<axis>|<kind>]`** renders the
registry compiled into the binary (the same annotations the gateway enforces).

Codegen runs **connectionless** via pinned local toolchain images (ADR-018,
`make gen-sdks`; freshness gated by `make gen-check`) — no BSR/network at gen time.
The generated SDKs **are committed** under `sdks/<lang>/` (ADR-006 D1) for **Go +
Python**, the consumed languages (ADR-037 trimmed the unused TS + Rust trees);
the proto stays the source of truth and any language regenerates on demand.

**Manifests** — per-plugin validation is `rat plugin check` (envelope + per-kind
schema + capability/axis coherence, ADR-026); the repo-wide gate is
`make validate-manifests`.

## Critical concerns baked in (from [`../reviews/00-synthesis.md`](../reviews/00-synthesis.md))

The synthesis flagged 10 wire-breaking concerns to bake in *before* freeze. The
ones that touch the **manifest** are in `plugin.v1.json` from day one:

- **C4 — resource asks/limits:** `resources` block, **mandatory**.
- **C5 — capability enforcement:** `provides` is what the gateway enforces at runtime (declared = enforced). The manifest is the source of that declaration.
- **C8 — supply-chain trust:** `trust` block (signature + signed_by + attestations); optional at solo, required at team+.

The remaining Critical concerns (C1 trace context, C2 plugin-auth, C3 state
namespacing, C6 conformance, C7 tenancy, C9 two-reference, C10 listener split)
live in the protos and the core, not the manifest — they landed as ADRs 004–013
(see the [ADR index](../docs/architecture/adrs/README.md)) and are enforced by the
sealed core (C2 hardened by ADR-042's channel-authenticated identity at `rat/6.7`).

## Related

- [`AMENDING.md`](AMENDING.md) — how to amend a frozen axis (the additive-amendment procedure + the measured cost).
- [`../docs/guides/authoring-a-plugin.md`](../docs/guides/authoring-a-plugin.md) — the plugin-author walkthrough.
- [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D3 (JSON Schema for manifests), D4 (capability major-versioning).
- [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) — the two-reference freeze gate.
- [docs/architecture/overview.md](../docs/architecture/overview.md) — the contract triple section this schema formalizes.
- [reviews/02-plugin-ecosystem-builder.md](../reviews/02-plugin-ecosystem-builder.md) — the author-surface gaps this Phase-0 work closes.
