# ADR-038: Reference plugins live under `plugins/`, not `examples/`

## Status: Accepted (2026-06-08)

## Context

[ADR-006](006-sdk-distribution-and-plugin-layout.md) D2 placed the reference implementations
under a top-level `examples/<axis>/<impl>-<lang>/` — "the term ADR-001 + roadmap already use." At
the time they were a handful of wire-contract demos.

They are no longer "examples" in the throwaway-sample sense. They are the **first-party reference
plugins** that *define* each axis: the ADR-003 two-reference pairs that gate every contract
freeze, the conformance subjects `make conformance`/`make composition` run, and the templates
ADR-026's `rat plugin` scaffolds from. `examples/` undersells them — it reads as "optional sample
code," when in fact they are load-bearing conformance artifacts. The professionalization restructure
([docs/restructure/](../../restructure/)) flagged this: the top-level tree should *spell out the
thesis* — "the core does six things, **everything else is a plugin**" — and the directory of those
plugins should be called `plugins/`.

## Decision

**Rename `examples/` → `plugins/`.** The reference plugins live under a top-level `plugins/<axis>/<impl>-<lang>/`.

Mechanical scope (a same-level rename — depth unchanged):

- `git mv examples plugins` (history preserved).
- Go module paths: `module github.com/rat-dev/rat/examples/...` → `.../plugins/...` (8 modules).
  These are nominal paths with **no cross-module importers**; the `replace … => ../../../contracts/sdks/go`
  directives are relative and depth-preserving, so they are unchanged.
- Build-by-path callers updated: `core/composition/*_test.go` (builds reference modules by path),
  `Makefile` (`plugin-images`, `bench`), `platform/` compose `working_dir`s, scripts.
- All `examples/…` path references in docs/manifests/configs → `plugins/…`.

This **supersedes ADR-006 D2's location only** (the layout shape `<axis>/<impl>-<lang>/` and the
"each plugin is its own module" rule are unchanged). ADR-006 D2 carries a pointer here; its body
stays as the historical record.

## Consequences

**Positive.** The tree communicates the architecture: `contracts/` (the wire) · `core/` (the
control plane) · `plugins/` (everything else) · `platform/` (the assembled product). The reference
plugins are named for what they are — load-bearing references, not disposable samples.

**Negative — accepted.** A wide mechanical churn (~300 path references, 8 go.mod module lines) and
a break in any external bookmark/clone that hard-coded `examples/`. One-time; `git mv` preserves
blame/history. Verified by: repo-wide markdown link check (0 broken), containerized `go build` of
the renamed modules, and the conformance/composition suites.

**Neutral.** ADR-001 and older docs that say "examples" as prose are left as historical wording;
only paths and links are rewritten.

## Alternatives considered

- **Keep `examples/`.** Rejected — it actively miscommunicates these are optional samples when
  they are the conformance backbone; the audit called it the highest signal-per-churn rename.
- **`reference-plugins/` or `refplugins/`.** Rejected — longer, and `plugins/` reads cleanest
  against `core/` + `contracts/` and directly echoes "everything else is a plugin."
- **Leave the Go module paths as `rat/examples/…` while moving the dir.** Rejected — a module path
  that says `examples` under a `plugins/` dir is exactly the inconsistency this ADR removes; the
  rename is safe because nothing imports those modules.

## Related

- [ADR-006](006-sdk-distribution-and-plugin-layout.md) — D2 set the original `examples/` location;
  superseded here (location only).
- [ADR-001](001-everything-is-a-plugin.md) — "everything else is a plugin," which the directory
  name now reflects.
- [ADR-003](003-two-references-before-contract-freeze.md) — why these are references, not samples.
- [docs/restructure/TARGET.md](../../restructure/TARGET.md) — the end-state tree this realizes.
