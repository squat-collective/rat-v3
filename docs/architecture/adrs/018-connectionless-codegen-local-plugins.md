# ADR-018: Connectionless codegen via local plugins

## Status: Accepted (2026-06-02)

## Context

[ADR-006](006-sdk-distribution-and-plugin-layout.md) (D1/D3) chose to generate the
per-language SDKs with **remote BSR plugins** — `buf.gen.*.yaml` references
`remote: buf.build/protocolbuffers/go`, `remote: buf.build/community/neoeinstein-prost`,
etc. `remote:` means buf uploads the protos to buf.build, runs the codegen plugin **there**,
and streams the result back. The appeal was zero local install: the only tool needed is the
`bufbuild/buf` container.

The cost surfaced during the Q02 additive cut (ADR-017): regenerating the SDKs after the
proto amendments, `make gen-sdks` makes **8 BSR calls per run** (2 plugins × 4 languages),
and buf.build's **anonymous rate limit** was exhausted mid-run — repeatedly. The Go/Python/
TypeScript SDKs regenerated, but the **rust storage SDK regen was left pending** (committed
as a documented gap), and several minutes were lost to retry/cooldown churn. The codegen
became a flaky, network- and quota-bound step on the critical path.

Crucially, buf's **core** operations (`lint` / `build` / `generate` / `breaking`) need **no
network** here: `buf.yaml` declares only a local module (`modules: [path: proto]`), no remote
deps. The *plugins* are the sole thing reaching out. So connectionless codegen is entirely
achievable — it is a template choice, not a buf limitation.

## Decision

**Switch all `buf.gen.*.yaml` templates from `remote:` to `local:` plugins, and run codegen
in pinned per-language container images that carry buf + the language's protoc plugin
binaries.** Codegen becomes **connectionless at run time** (no BSR, no rate limit, no auth),
**reproducible** (pinned plugin versions), and **offline-capable**.

### 1. Local plugins, pinned to the committed-SDK versions

Each language's `local:` plugin is pinned to the **exact version recorded in the committed
generated files' headers**, so the swap produces **zero churn**:

| lang | remote (was) | local (now) | pinned version (from headers) |
|---|---|---|---|
| Go | `protocolbuffers/go` + `grpc/go` | `protoc-gen-go` + `protoc-gen-go-grpc` | `v1.36.11` + `v1.6.2` |
| TypeScript | `bufbuild/es` + `connectrpc/es` | `protoc-gen-es` + `protoc-gen-connect-es` | matched to the committed `@bufbuild/protobuf` output |
| Rust | `community/neoeinstein-prost` + `…-tonic` | `protoc-gen-prost` + `protoc-gen-tonic` | prost-build pinned |
| Python | `protocolbuffers/python` + `grpc/python` | protoc builtin `python` + `grpc_python_plugin` (grpcio-tools) | protobuf `7.35.0` |

Where an exact match is not cleanly achievable, the migration takes a **one-time regen
churn** (the SDKs are regenerated to the new pinned toolchain's output, committed once); from
then on the output is stable + offline.

### 2. Per-language toolchain images

`contracts/codegen/Dockerfile.<lang>` builds a small image with **buf + that language's
plugin(s)** pinned. `scripts/gen-sdks.sh` runs each language's `buf generate` in its image
(`podman run … rat-codegen-<lang>`), replacing the single `bufbuild/buf` container. A
`make gen-images` target builds them; the **image build** needs network (base images +
package registries — NOT the BSR, NOT rate-limited the same way), the codegen **run** does
not.

### 3. Staged rollout

Land per language, verifying each (`make breaking` clean, `compile-sdks` / `core-test` green,
diff reviewed): **Go first** (the load-bearing SDK, ADR-006 Go-first; exact-version → zero
churn) → **TypeScript** → **Rust** (also closes the ADR-017 pending rust-storage regen) →
**Python** (the awkward one — protoc builtin + grpc plugin; the `grpc_tools.protoc` fallback
in Alternatives if buf-local python is not clean). Progress tracked in the roadmap; this ADR
is the decision, not gated on full rollout.

## Consequences

**Positive.**
- Codegen is **connectionless, unlimited, and reproducible** — no BSR rate limit, no network
  flakiness, no `buf login` / credentials, works offline. The exact friction that bit the
  ADR-017 cut is gone.
- Pinned plugin versions make SDK output **deterministic across machines + CI**, and end the
  silent drift (the committed python `class X(object)` vs current-remote churn).
- Closes the pending **rust-storage SDK regen** without ever touching the BSR.

**Negative — accepted.**
- We now **maintain per-language toolchain images + pinned plugin versions** (vs the
  zero-maintenance `bufbuild/buf` image). Bumping a plugin is an explicit, reviewed change.
- **Image builds are heavier** than pulling `bufbuild/buf` — especially **rust**, where
  `cargo install protoc-gen-prost protoc-gen-tonic` compiles for minutes (one-time, cached).
- A **one-time regen churn** wherever an exact version match isn't achievable (reviewed +
  committed once as the migration).

**Neutral.** This **supersedes ADR-006's D1/D3 remote-plugin choice** for *how* codegen runs;
the SDK layout, the contract triple, and "SDKs are committed, never hand-edited" are
unchanged. `buf.yaml` (lint/breaking rules, the local module) is untouched.

## Open questions

- **Q01 — ✅ RESOLVED (2026-06-10, the DX-8 review): the protoc-35 hybrid IS the settled
  shape.** `rat-codegen-python`'s ENTRYPOINT runs a pinned standalone `protoc` v35.0
  (matching buf's `protocolbuffers/python` 7.35.0 gencode) for messages +
  `grpcio-tools==1.80.0` (matching the references) for the gRPC stubs — no buf in the
  python leg, fully offline. The "clean" buf shape is not deferred, it's **impossible to
  pin**: the python message generator is a protoc *builtin* (no standalone
  `protoc-gen-python` to install), and `grpc_python_plugin` ships only inside
  grpcio-tools — so bypassing buf for python is the design, not a fallback.
  Operationally: the failure modes are the **toolchain's, not your proto's** (a protoc
  download-URL break at `make gen-images` time, or a protobuf/grpcio-tools pin drift) —
  if `make gen-sdks` fails on python and `make lint` passes, suspect
  `contracts/codegen/Dockerfile.python`, where both pins live. Upgrades = bump the pins,
  regen, commit (the ADR-006 D1 committed SDKs make the diff reviewable). The gen-check
  freshness gate exercises this leg every `make verify` (repaired at `rat/6.16` — protoc
  creates no output dirs, which had silently killed check mode).
- **Q02** — Converge the four per-language images into one combined codegen image later (one
  build, simpler `gen-sdks`) once the per-language shapes are settled?

## Alternatives considered

1. **Keep remote plugins + `buf login`.** A logged-in token raises the rate limit. Rejected
   as the fix: still network-bound + flaky, and it injects a credential/secret into the
   codegen path (CI + every contributor) for a step that has no reason to need one.
2. **One combined codegen image** (buf + all four toolchains). Rejected *for now*: a heavier,
   slower-to-build image that couples the languages; per-language images are more modular and
   each build is smaller. Revisit (Q02) once shapes settle.
3. **Generate Python via `grpc_tools.protoc` directly** (bypass buf for python). Kept as the
   **python fallback** if buf-local python is not clean — `python -m grpc_tools.protoc` is a
   self-contained, offline, pinned codegen (bundled protoc + python + grpc plugins).
4. **Do nothing / wait out the rate limit.** Rejected: the friction recurs on every proto
   change, and it already cost real time + left a committed gap.

## Migration

This is a toolchain swap with no contract change. Sequence, per language:
1. Add `contracts/codegen/Dockerfile.<lang>` (buf + pinned plugin(s)).
2. Switch `buf.gen.<lang>.yaml` `remote:` → `local:`.
3. `make gen-images` (build) → `make gen-sdks` (regenerate connectionless).
4. Review the diff: **zero** where versions matched; a one-time migration regen otherwise.
5. `make breaking` (clean) + `compile-sdks` / `core-test` (green) → commit.

`gen-sdks.sh` + the Makefile gain the per-language image plumbing. `make breaking` /
`validate-manifests` / `core-test` are unaffected (they don't generate SDKs).

## Related

- [ADR-006](006-sdk-distribution-and-plugin-layout.md) — chose remote plugins (D1/D3); this
  supersedes that *mechanism* (not the layout).
- [ADR-017](017-pre-unfreeze-contract-amendment-gate.md) — the additive cut whose regen
  exposed the BSR friction + left the rust-storage gap this closes.
- `scripts/gen-sdks.sh`, `Makefile` (the `gen-sdks` / `gen-check` / new `gen-images`
  targets), `contracts/buf.gen.*.yaml`, `contracts/codegen/`.
