# ADR-026: Plugin authoring & packaging — the `rat plugin` toolkit, the verified-plugin gate, scaffolded CI/CD

## Status: Proposed (2026-06-03)

## Context

Everything built so far is **runtime** (launch → wire → route) plus **distribution of `rat` itself**
(the GHCR release pipeline, Phase 4). The missing half is **authoring**: how a plugin gets
*created, proven to actually work, packaged, and published* — the build-time complement.

A plugin is already an OCI image that rat launches ([ADR-022](022-plugins-are-launched-not-composed.md)/
[ADR-023](023-rat-as-a-per-project-daemon.md)). But today writing one means hand-rolling a Dockerfile,
a manifest, and wiring conformance by hand — too much friction for an ecosystem. The conversation set
the target: a **local packaging service** (build + verify locally, like `docker build`), a
**builder/packager that checks the plugin actually works** (not just "it built"), and a
**`rat plugin init/check/test`** workflow "a bit like `poetry init`" with scaffolded **portable CI/CD**
("a standard that works for GH and others"). The team difference is simply *who publishes to GHCR*.

Crucially, the checks are **not new** — the primitives exist: `validate-manifests` (the frozen
`plugin.v1.json` + the per-kind schemas, [ADR-011](011-manifest-schema-freeze-and-per-kind-layer.md)),
`conformance` (golden vectors per axis, [ADR-003](003-two-references-before-contract-freeze.md)), and the
I9 launch (the deployment-runtime, [ADR-016](016-plugin-provisioning-via-deployment-runtime.md)). The
packager *orchestrates* them per-plugin and **gates** on them.

## Decision

**Ship a `rat plugin` authoring toolkit that scaffolds, verifies, packages, and publishes plugins. A
plugin's artifact is a VERIFIED OCI image (manifest stamped in); a plugin only publishes if it passed
`check` + `test`. `rat plugin init` scaffolds a ready-to-build folder — including portable CI/CD — so
authoring a plugin is `poetry init`-easy.** Seven parts:

### 1. The `rat plugin` toolkit (build-time verbs, not a runtime plugin)

```
rat plugin init <name> --kind <kind> [--lang python|go]   # scaffold (poetry init)
rat plugin check                                          # STATIC gate: manifest schema + per-kind + coherence
rat plugin test                                           # launch under I9 + run the axis conformance vectors
rat plugin pack                                           # check + test + build → a VERIFIED local image
rat plugin publish [--registry ghcr.io/<you>]             # push the verified image (the team diff)
```

These are **client-side, build-time** — packaging is dev-time, so it stays OUT of the six-thing core
(no new runtime responsibility). They live in the one `rat` binary ([ADR-023](023-rat-as-a-per-project-daemon.md))
as a sub-namespace, exactly as `rat init`/`add` are project verbs.

### 2. The artifact: a verified OCI image with the manifest stamped in

`pack` builds the image and **embeds the validated manifest as an image label**. So `rat add <ref>`
reads the manifest *from the image* — closing the manifest-from-image follow-on and dropping the
`--manifest` path. A "verified plugin" = one proven to launch + conform, not merely one that built.

### 3. The verified-plugin GATE (orchestrates existing checks, refuses on failure)

```
rat plugin pack:
  check  →  manifest vs plugin.v1.json + the per-kind schema  (validate-manifests)
            + coherence: kind matches the axis of `provides`; requires/provides/contributes
              are well-formed capability/slot URIs that name something real
  test   →  build → launch under I9 → run the axis golden vectors (conformance)
            + assert the declared `provides` are actually served
  pack   →  stamp the manifest label → tag the verified image locally
```

`check` is fast + static (no run); `test`/`pack` actually run the plugin (the strong proof). `publish`
only ships an image that passed.

### 4. `rat plugin init` — the scaffold (driven by the frozen per-kind schema)

Given a kind, `init` knows from the per-kind schema what the plugin must provide, and generates a
folder that *compiles and passes `check` on the first run*, with TODOs where the logic goes:

```
my-plugin/  manifest.yaml (provides pre-filled for the kind) · server stub (SDK servicer)
            Dockerfile · README.md · .gitignore · ci.sh · .github/workflows/plugin.yml
```

This is the barrier-lowering that makes an ecosystem happen — nobody hand-writes a Dockerfile or wires
conformance.

### 5. Scaffolded portable CI/CD — the logic is in `rat`, the CI files are thin wrappers

The validate/test/pack/publish **logic lives in `rat plugin` verbs**, never in GitHub-specific actions.
So the scaffolded pipeline is:

```
PR / push      →  CI:  rat plugin check && rat plugin test && rat plugin pack   (gate, no publish)
tag / release  →  CD:  rat plugin publish → ghcr.io
```

and the per-platform file (`plugin.yml`, `.gitlab-ci.yml`, …) is a 5-line wrapper that `curl …/install.sh
| sh` (the Phase-4 release pipeline) then runs the verbs. **"Works for GH and others" falls out for free.**
CD only ships things that passed the gate — automatic delivery, never a broken plugin. `init` defaults to
**tag-triggered** CD (matches rat's own release pipeline); `--auto-release` adds conventional-commit
versioning ("merge = ship").

### 6. Two tiers: local packaging vs team publish

- **local (dev):** `rat plugin pack` → the **local OCI store** (podman). No service to run; the dev loop.
- **team:** `rat plugin publish` → **`ghcr.io`** — the *plugin* parallel to rat's own release pipeline.
  Optionally a local **`registry:2`** for sharing without GHCR. The deployment-runtime already pulls by
  ref, so a local image, a local registry, or a GHCR ref resolve uniformly in `rat add`.

### 7. The build step + the templates want to be plugin axes (later)

Consistent with "everything is a plugin": *how* the image is built (podman/buildah/kaniko/nix) is a
**build-runtime axis**, parallel to the deployment-runtime ("the packager builds via a backend, it
doesn't bake one in"). And the **templates** could be a `kind: template` axis (community-extensible). v1
**bakes** podman-build + a handful of golden templates (from the reference plugins); the axes are the GA
shape. The validate + conformance steps are NOT pluggable — they are the frozen contract enforcing itself.

## Consequences

**Positive.**
- **Ecosystem barrier collapses** — `rat plugin init → check → test → pack → publish` is `poetry`/`cargo`
  for plugins; a new plugin builds + ships with CI/CD from `init`.
- **Verified plugins** — the gate means a published/marketplace plugin is proven to launch + conform.
- **Portable CI/CD for free** — logic in `rat`, bootstrapped by the one-line install; GH/GitLab/etc. are
  thin wrappers.
- **Consistent** — a plugin's CD is rat's own release pattern, scaffolded; same registry, same shape.

**Negative — accepted.**
- **Build-time surface in `rat`** — the binary grows authoring verbs + baked templates to maintain
  (mitigated: templates derive from the reference plugins; the build/template axes externalize later).
- **Conformance-per-plugin glue** — driving the axis vectors against an arbitrary plugin needs per-axis
  wiring (open question: a harness vs a `conformance` capability the plugin serves).
- **Signing + the marketplace index are deferred** — `publish` is where signing lands and where a
  marketplace registration would hook; both out of v1 scope.

**Neutral.**
- The manifest-from-image label format is introduced here (a new convention, not a wire change).

## Open questions

- **Q01 — build-backend axis.** When does podman-build become a `kind: build-runtime` plugin (buildah,
  kaniko, nix)? v1 bakes podman.
- **Q02 — template axis.** Baked templates (v1) vs a `kind: template` community-extensible axis.
- **Q03 — conformance driving.** A harness `rat plugin test` shells, vs a first-class `conformance`
  capability the plugin serves so the packager drives every axis uniformly.
- **Q04 — release trigger.** Tag-triggered (default) vs conventional-commit auto-release (`--auto-release`);
  where `rat plugin version` (semver bump) lives.
- **Q05 — manifest-in-image.** The label key + format; how `rat add <ref>` reads it; precedence vs an
  explicit `--manifest`.
- **Q06 — signing + marketplace.** `publish` signs (the manifest `trust`/C8 block) + registers in a
  marketplace index for discovery — both future.

## Alternatives considered

1. **A separate `rat-pkg` tool.** Rejected: ADR-023 made `rat` the one multi-call binary; authoring is a
   `rat plugin` namespace, not a second binary.
2. **Hand-written Dockerfile + manual conformance (status quo).** Rejected: the friction that kills an
   ecosystem; `init` + the gate exist to remove it.
3. **GitHub-Action-native CI (logic in actions).** Rejected: locks CI to GitHub. Logic-in-`rat` + thin
   wrappers is portable by construction.
4. **A long-running packaging daemon** (à la `dockerd`). Rejected for v1: `rat plugin pack` is a command;
   the local OCI store is the "service". A build-cache daemon is a future optimization, not the model.

## Update — 2026-06-04 (Phase 10): SDK distribution + polyglot scaffold

Two gaps surfaced by *dogfooding* `rat plugin init/pack` to author a plugin from scratch, both
fixed:

- **SDK as a base image, not vendored.** The scaffold told Python plugins to *vendor the whole
  generated SDK* (844K of all-axes stubs) into their own repo. Replaced with **`rat/plugin-base-py`**
  (the SDK + grpc baked into `site-packages`, built by `make plugin-base-py`); scaffolded Python
  plugins now `FROM` it and ship only their own code (proven: a plugin repo 892K → 40K, still passes
  the verified gate). The plugin-axis contract is the wire — a plugin needs the *generated stubs* for
  the axes it speaks, but as an installed dependency, not committed source.
- **`rat plugin init --lang` is polyglot.** The toolkit emitted Python only, though the architecture
  (gRPC + manifest, four generated SDKs) was always language-agnostic. Added **`go | typescript |
  rust`** (Python default): each emits an idiomatic server stub + build manifest + Dockerfile.
  Compiled langs (Go, Rust) link the SDK in and ship a tiny static-ish binary (no base image —
  Go is **14.9 MB on `scratch`** vs the 153 MB Python base); interpreted langs get the SDK from a
  base / npm. All four verified: scaffold → `check` → `pack` (build → launch under I9 → healthy →
  serves-gate). `--lang go` is arguably the better default for performance-sensitive axes.

**Still open (follow-ons):** publish `rat/plugin-base-py` to ghcr (today a local `make` target, the
same placeholder gap as the official-marketplace URL); a per-axis SDK split; and `init --lang`
emitting a *fully implemented* engine stub (today the servicer is a TODO, so a fresh `pack` fails the
serves-gate until you implement it — by design).

## Related

- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime axis; the
  build-runtime axis is its build-time parallel.
- [ADR-003](003-two-references-before-contract-freeze.md) — conformance (the strong check `test` runs).
- [ADR-011](011-manifest-schema-freeze-and-per-kind-layer.md) — the manifest + per-kind schema `check`
  validates against and `init` scaffolds from.
- [ADR-023](023-rat-as-a-per-project-daemon.md) — the one `rat` binary + the release pipeline that
  bootstraps the portable CI.
- ideas/inbox — the marketplace/distribution + trust ideas (`publish` → signing + the marketplace index).
