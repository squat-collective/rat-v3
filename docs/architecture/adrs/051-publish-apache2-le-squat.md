# ADR-051 — Publish RAT v3: Apache-2.0, `github.com/le-squat/rat`, GHCR distribution

**Status:** Accepted (2026-06-10)

## Context

Backlog **DX-2** — the last item of the DX frustration review — was never an engineering
task: the repo was complete, gated, and groomed for the outside world (`rat/6.14`–`6.17`),
but unpublished. Until publication, external plugin authors were structurally impossible:
`scripts/install.sh` 404'd, the Go SDK wasn't fetchable, the base images lived only on
the build machine, and there was **no license** (README: "TBD — the license decision is
itself a future ADR"). This is that ADR. The Q02 external-review gate this publication
nominally waited on was set aside earlier as impractical for a solo dev (see
`roadmap/current.md`); the user-pull gates (phases.md Gate B+) govern *building more
product*, not *making the existing work reachable* — publication is in fact their
precondition.

## Decision

1. **License: Apache-2.0** (LICENSE = the canonical text; NOTICE carries the copyright).
   The platform's thesis is a third-party plugin ecosystem; Apache-2.0 is the
   K8s/NATS/Temporal-tier norm for exactly that: permissive, explicit patent grant,
   no enterprise-adoption friction.
2. **Home: `github.com/le-squat/rat`, public.** The `le-squat` GitHub org is the
   maintainer's chosen home (over the placeholder `rat-dev` baked in at Phase 0).
3. **The module-path rename `rat-dev` → `le-squat` lands with the publication** —
   `go get github.com/le-squat/rat/gen` must actually work, so the Go module paths, the
   proto `go_package` options, the committed SDKs (regenerated), `install.sh`'s default
   repo, and the GHCR image refs all follow the org. **This consciously trips
   `buf breaking`'s `FILE_SAME_GO_PACKAGE` rule against the pre-rename baseline — once,
   for this seal.** `go_package` is build metadata, not wire shape: the full core suite,
   conformance (32/32), and the regenerated cross-language SDKs prove the wire held.
   Historical ADRs keep their `rat-dev` mentions (they were true when written).
4. **Distribution is GitHub Releases + GHCR** (the existing `release.yml`, repaired +
   extended): per `rat/N.M` tag — 4-platform static binaries + `SHA256SUMS` +
   `install.sh` on the Release, and `ghcr.io/le-squat/rat` (daemon) **plus
   `ghcr.io/le-squat/rat-plugin-base-{go,py}`** (the SDK distribution for plugin
   authors, per the user's "defer PyPI, use GHCR" call).
5. **PyPI is deferred** — the Python SDK ships inside `rat-plugin-base-py`; a pip
   package needs packaging work (pyproject) + a PyPI account, recorded as the residual
   (backlog ⑤).
6. **CI ships repaired, not as written:** `contracts.yml`'s SDK job predated ADR-018/037
   (it regenerated **four** languages via remote buf plugins and **auto-committed to
   `main` from CI** — violating both the pinned-codegen design and the sealed-`main`
   discipline). It now runs the repo's own freshness gate (`scripts/gen-sdks.sh --check`)
   and never commits. Historical `rat/*` tags stay local; only `rat/6.18`+ are pushed
   (each pushed tag cuts a Release — 17 retroactive releases would be noise and would
   race `releases/latest`).

## Consequences

**Positive.** `curl …install.sh | sh` works as documented; the Go SDK is `go get`-able;
plugin authors `FROM ghcr.io/le-squat/rat-plugin-base-py` with no clone; the license
question stops blocking everything ecosystem-shaped.

**Negative — accepted.** Publication is one-way (public code is cached/indexed forever);
Apache-2.0 permits competing commercial use (that's the ecosystem bet); the one-time
`FILE_SAME_GO_PACKAGE` breaking-gate waiver is a precedent that must stay metadata-only
(any *wire* rule waiver still requires its own ADR); local historical tags diverge from
the remote's tag list until deliberately pushed.

**Neutral.** The six-thing core, the frozen wire, and every gate are untouched — this
ADR changes where the bytes live, not what they are.

## Alternatives considered

1. **`rat-dev` org (the baked-in placeholder).** Zero-diff, but it's a name claimed for a
   placeholder, not an identity the maintainer wants. Rejected by the maintainer.
2. **Personal repo (`tomblancdev/rat`).** Works, but signals a personal project rather
   than a platform org, and a later org migration would re-break module paths anyway.
3. **MIT.** Fine, but no patent grant — Apache-2.0 is the stronger ecosystem signal.
4. **AGPL-3.0 / BSL.** Protect against cloud resellers at the cost of chilling the very
   third-party plugin ecosystem the architecture exists for. Wrong trade for a platform
   whose moat is *pluggability*, not the code.
5. **Publish private first.** One-click reversible caution, but it keeps DX-2's actual
   goal (external authors) blocked. The repo had four seals of grooming; declined.

## Related

- `roadmap/backlog.md` ⑤ **DX-2** — the item this closes (PyPI recorded as residual).
- [ADR-018](018-connectionless-codegen-local-plugins.md) / [ADR-037](037-trim-committed-sdks-to-consumed-languages.md) — why `contracts.yml`'s old SDK job was wrong.
- [ADR-026](026-plugin-authoring-and-packaging.md) — the authoring flow the base images complete.
- [ADR-050](050-plane-file-env-interpolation.md) — the preceding DX seal.
- `.github/workflows/release.yml` — the distribution pipeline this turns on.
