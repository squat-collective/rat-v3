# ADR-037: Trim the committed SDKs to the consumed languages (Go + Python)

## Status: Accepted (2026-06-08)

## Context

[ADR-006](006-sdk-distribution-and-plugin-layout.md) D1 decided to **commit** generated
per-language SDKs into `contracts/sdks/<lang>/`, "one peer directory per language, none
privileged," mirroring Kubernetes' vendored-clients model. Its 2026-05-31 amendment wired
**four** languages — Go, Python, TypeScript, Rust — and `scripts/gen-sdks.sh` regenerates all
four; the committed trees total ~185 files.

The professionalization restructure audit ([docs/restructure/AUDIT.md](../../restructure/AUDIT.md))
checked actual consumption across the whole repo:

- **Go SDK** — consumed by 5+ reference plugins + the `ratplugin` helper + the `plugin-base-go`
  image. **Load-bearing.**
- **Python SDK** — consumed by 9+ plugins/platform files + `rat.plugin`/`rat.contrib` + the
  `plugin-base-py` image. **Load-bearing.**
- **TypeScript SDK** — **zero consumers.** The only TS in the repo (`vscode-rat`) talks plain
  REST over `http`, never the protobuf SDK. No TS plugin exists.
- **Rust SDK** — **zero consumers.** No `Cargo.toml` anywhere; `contracts/codegen/Dockerfile.rust`
  itself notes the SDK "is never compiled" and has "no reference plugins."

So two of the four committed SDK trees (~84 files) demonstrate the any-language promise only on
paper. They churn on every proto change (large mechanical diffs in `make gen-check`) and carry
two codegen toolchain images, for no consumer.

## Decision

**Commit only the SDKs a repo artifact actually consumes — today Go and Python.** Remove the
TypeScript and Rust committed SDKs and their codegen wiring:

- delete `contracts/sdks/typescript/` and `contracts/sdks/rust/`
- delete `contracts/buf.gen.typescript.yaml`, `contracts/buf.gen.rust.yaml`,
  `contracts/codegen/Dockerfile.typescript`, `contracts/codegen/Dockerfile.rust`
- also delete the already-dead `contracts/buf.gen.python.yaml` (bypassed by the
  `Dockerfile.python` ENTRYPOINT since ADR-018; it still pointed at a BSR remote plugin)
- `scripts/gen-sdks.sh`: `LANGS=(go python)`

**The source of truth is unchanged.** `contracts/proto/**` still defines every axis; the
codegen toolchain (ADR-018) can regenerate a TypeScript or Rust SDK **on demand** the moment a
first consumer in that language appears — at which point that language's SDK becomes committed
again under the same ADR-006 rule. This ADR narrows *which generated outputs we vendor*, not the
language-neutrality of the contracts.

## Consequences

**Positive.** ~84 fewer committed files (~29% of `contracts/`); `make gen-check` stops churning
two unused trees; two codegen images retired; the committed SDK set now *demonstrates* the
any-language promise (each vendored SDK has a real consumer) instead of diluting it.

**Negative — accepted.** A TS/Rust plugin author must run `make gen-sdks` (or re-add the wiring)
before the SDK exists, rather than finding it pre-vendored. This is the exact offline-convenience
ADR-006 optimized for — but only for languages with zero current authors, so the cost is latent.
Re-adding a language is a mechanical, reversible change.

**Neutral.** Partially narrows ADR-006 D1's "four languages, none privileged" amendment: Go and
Python are now *de facto* privileged by being the only vendored outputs — justified by being the
only consumed ones. ADR-006 D1 carries a pointer to this ADR.

## Alternatives considered

- **Keep all four committed (status quo).** Rejected — vendors generated code no artifact reads,
  churning it forever to satisfy the letter of the four-language amendment, not its intent.
- **Stop committing SDKs entirely; regenerate always.** Rejected — Go and Python have many
  real consumers and the offline/instant authoring experience (ADR-006's core rationale) is
  worth keeping for them.
- **Write a TS/Rust reference plugin so the SDKs earn their keep.** Rejected as scope — no axis
  needs a third-language reference today (ADR-003 is satisfied by Go+Python twins); manufacturing
  a consumer to justify a vendored artifact is backwards.

## Related

- [ADR-006](006-sdk-distribution-and-plugin-layout.md) — the committed-SDK decision this narrows.
- [ADR-018](018-connectionless-codegen-local-plugins.md) — the connectionless codegen that makes
  on-demand regeneration cheap (and which already orphaned `buf.gen.python.yaml`).
- [ADR-003](003-two-references-before-contract-freeze.md) — the two-reference rule, satisfied by
  the Go + Python twins.
- [docs/restructure/AUDIT.md](../../restructure/AUDIT.md) — the consumption analysis behind this.
