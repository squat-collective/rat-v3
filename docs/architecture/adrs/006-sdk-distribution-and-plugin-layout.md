# ADR-006: SDK distribution, reference-plugin layout, and codegen toolchain

**Status:** Accepted
**Date:** 2026-05-31
**Deciders:** Tom, Claude (architecture session)
**Context:** entering sub-phase 0d (first reference implementations) — the first code in the project.

---

## Context

The contract triple (`.proto` + `plugin.yaml` schema + capability URIs) is language-neutral by
design — ADR-001 commits that "plugin authors can use any language; the contract is proto +
manifest," and [vision.md](../../vision.md) commitment #3 forbids "language-specific SDKs that
other languages must replicate." Sub-phase 0d now needs to produce *running code* (reference
plugins) against that contract, which forces three project-shaping decisions that have not been
made yet:

1. **How are per-language SDKs generated and distributed** from the one proto source, without
   privileging any language?
2. **Where do reference plugins live**, and in what layout?
3. **What codegen toolchain runs**, given this project's hard constraint that nothing installs on
   the host — everything runs in Podman/Docker (root `CLAUDE.md`), and the sandbox has shown
   flaky/blocked external network (GHCR was 403 during 0b; the Go SDK pulls `grpc` which needs a
   newer Go toolchain than the base image ships).

These are hard to change once an ecosystem and a body of plugin code exist, so they are decided
here before scaffolding rather than left implicit in a directory tree.

## Decision

### D1 — SDK distribution: vendored `sdks/<lang>/` now, BSR-published later

Generated SDKs are **committed into the repo**, one peer directory per language, none privileged:

```
contracts/
  proto/                      # SOURCE OF TRUTH (language-neutral)
  schema/                     # SOURCE OF TRUTH (manifest JSON Schema)
  buf.gen.go.yaml             # one codegen template per language
  buf.gen.python.yaml
  buf.gen.typescript.yaml
  buf.gen.rust.yaml
  sdks/
    go/                       # generated *.pb.go (+ grpc-go)        — committed
    python/                   # generated *_pb2.py (+ grpc)          — committed
    typescript/               # generated *.ts (protobuf-es + connect-es) — committed
    rust/                     # generated *.rs (prost + tonic)       — committed
```

> **Narrowed by [ADR-037](037-trim-committed-sdks-to-consumed-languages.md) (2026-06-08):**
> the TypeScript + Rust committed SDKs (zero consumers) were removed; only **Go + Python**
> (the consumed languages) remain vendored. The proto source of truth is unchanged and any
> language regenerates on demand. The four-language amendment below is the historical record.

**Amendment (2026-05-31):** all four target languages — **Go, Python, TypeScript,
Rust** — are now wired and generated (not Go-first-only as the original text below
contemplated). Counts at first generation: go 43 (+go.mod), python 46, typescript
42, rust 39. Plugin stacks: Go = protocolbuffers/go + grpc/go; Python =
protocolbuffers/python + grpc/python; TypeScript = bufbuild/es + connectrpc/es;
Rust = community neoeinstein-prost + neoeinstein-tonic. `scripts/gen-sdks.sh`
LANGS + CI regenerate all four. NOTE: codegen uses **remote buf.build plugins**
(network); regenerating all four in quick succession can hit BSR rate limits —
acceptable because regen is infrequent and the SDKs are committed, but a reason
the freshness gate must tolerate transient BSR 429s (retry rather than fail hard).

The proto remains the single source; `sdks/<lang>/` are **regenerated, never hand-edited**
(enforced by a header + a regen script — see D3). When a real external ecosystem exists, the
same protos additionally get **published to the Buf Schema Registry** (`buf.build/rat-dev/rat`)
so outside authors can `go get` / `pip install` / `npm i` hosted SDKs without cloning — but BSR
is a *later, additive* distribution channel, not a dependency of the monorepo's own development.

**Why vendored-now:** (a) offline + instant for any plugin author in this container-only,
sometimes-network-blocked environment — clone, point at `sdks/<your-lang>/`, build, no codegen
step and no external service; (b) self-contained, matching the `chmod +x ./rat` ethos; (c)
language-symmetric by construction (N peer dirs). This mirrors **Kubernetes exactly**: vendor
generated clients for the monorepo's own use, publish per-language libs for outsiders.

**Accepted cost:** committed generated code churns on proto changes (large mechanical diffs).
Mitigation: regenerate only via the script, never by hand; the diffs are reviewable as "did the
contract change as intended." The opposite tradeoff (gitignore + regenerate-on-build, "Option
A") was rejected for now because it makes "clone and build a plugin" a multi-step,
toolchain-dependent operation in an environment where the toolchain is itself fiddly.

### D2 — Reference-plugin layout: `examples/<axis>/<impl>-<lang>/`

> **Superseded (location only) by [ADR-038](038-reference-plugins-live-under-plugins.md)
> (2026-06-08):** the reference plugins now live under **`plugins/`**, not `examples/` — they are
> load-bearing conformance references, not samples. The layout shape below (`<axis>/<impl>-<lang>/`,
> one module each) is unchanged; read `plugins/` for `examples/` throughout this section.

Reference plugins live under a top-level `examples/` (the term ADR-001 + roadmap already use),
one directory per (axis, implementation), language suffixed so the any-language story is visible
in the tree:

```
examples/
  format/
    inmemory-go/              # first format reference impl (Go)
    inmemory-py/             # second, independent impl (Python) — the ADR-003 pair
  README.md                  # how to build/run a reference plugin + the golden-data harness
```

Each plugin is its own module in its own language (own `go.mod` / `pyproject.toml` /
`package.json`), depending on the matching `contracts/sdks/<lang>/`. The **ADR-003 two-reference
rule** is satisfied per critical axis by two `examples/<axis>/*` dirs in *different* languages
running the shared golden-data conformance vectors against each other.

### D3 — Codegen toolchain: containerized `buf generate`, pinned, scripted

Codegen runs through `buf` in a container (consistent with how `buf lint/build` already run in
this project), driven by a committed `scripts/gen-sdks.sh` that regenerates all `sdks/<lang>/`.
Two specifics this ADR pins, both forced by problems already hit:

- **Go SDK Go-version floor.** The generated Go gRPC stubs pull `google.golang.org/grpc`, whose
  recent releases require Go ≥ 1.25 (the base `golang:1.23` image failed to build the SDK during
  this session). The Go SDK module therefore pins a Go-version floor (and the codegen/build
  containers use a matching image), OR pins `grpc`/`protobuf` to versions compatible with the
  chosen image — whichever the gen script settles. This is captured so the next session doesn't
  rediscover it.
- **Remote vs local buf plugins.** `buf generate` currently uses *remote* plugins on buf.build
  (needs network). The gen script must work in this environment; if remote plugins are
  unreachable, fall back to local codegen plugin images. The script is the single place this is
  resolved.

`sdks/` is committed; the transient `gen/` path used during 0b is removed (superseded by
`sdks/go/`).

## Consequences

### Positive
- The any-language promise (ADR-001, vision #3) becomes **visible and tested**, not aspirational:
  the tree shows Go/Python/TS as peers, and 0d's two-impl pairs are cross-language.
- Plugin authors get an offline, one-step "point at the SDK and build" experience.
- The contract stays the single source; SDKs are derived artifacts with a scripted, reviewable
  regen path.
- A clean future migration to BSR for external distribution, without changing the monorepo's
  internal workflow.

### Negative — accepted
- Committed generated code in N languages enlarges the repo and produces mechanical diffs on
  contract changes. Accepted; mitigated by the regen-only discipline.
- Maintaining a codegen pipeline per language (container images, version pins) is real
  maintenance the core team owns. Accepted as the cost of the any-language commitment.
- Three languages now means three toolchains to keep working; if that proves heavy pre-freeze,
  the *generation* can be narrowed to Go first while keeping the layout (the decision is the
  layout + strategy, not "all three must exist on day one").

### Neutral
- BSR publication is deferred, not rejected — it becomes an ops task when there are external
  consumers.

## Alternatives considered

- **Option A — ship only protos, authors codegen locally (nothing committed).** Leanest repo,
  fully symmetric, matches gRPC's documented flow. Rejected for *now* because it makes building
  any plugin a toolchain-dependent multi-step operation in an environment where the toolchain is
  fiddly and the network unreliable. Remains the fallback if vendored gen becomes too heavy.
- **Option B — Buf Schema Registry as the primary channel now.** The modern best-in-class
  answer; zero in-repo generated code; what buf built BSR for. Rejected as the *primary* path now
  because it needs network + a BSR org and would couple day-one local development to an external
  service the sandbox may block. Adopted as the *later* external-distribution channel.
- **Single-language (Go-only) SDK.** Rejected outright: directly violates ADR-001 + vision #3.
  Go may be generated *first* for pragmatism, but the layout and strategy are multi-language from
  the start so it's never retrofitted.

## Migration

- Add `contracts/buf.gen.<lang>.yaml` templates + `scripts/gen-sdks.sh`; settle the Go-version /
  grpc-pin question in the script; generate `contracts/sdks/{go,python,typescript}/`; commit.
- Remove the transient `gen/` artifact path; update `contracts/.gitignore` (drop the `gen/`
  ignore, since `sdks/` is now committed) and `contracts/README.md`.
- Scaffold `examples/format/inmemory-go/` as the first reference plugin (0d), then a second
  independent impl in another language for the ADR-003 golden-data cross-run.
- Publish to BSR when the first external plugin author appears (future ops task).

## Related

- [ADR-001](001-everything-is-a-plugin.md) — any-language plugin commitment this operationalizes.
- [ADR-002](002-founding-tech-stack.md) — D2 (gRPC), D1 (Go core); core SDKs published by the core team.
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference rule the `examples/` layout serves; cross-language pairs are the strongest form of it.
- [ADR-004](004-core-language-go.md) — Go is the *core* language; this ADR ensures plugins are not Go-bound.
- [reviews/06-proto-contract-review.md](../../../reviews/06-proto-contract-review.md) — the freeze-blocker remediation that made the contracts ready for reference impls.
- [roadmap/current.md](../../../roadmap/current.md) — sub-phase 0d entry.
