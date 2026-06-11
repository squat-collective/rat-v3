# ADR-052 — The binary is the interface (zero-`make` user paths)

**Status:** Accepted (2026-06-11)

## Context

The maintainer's call, post-publication: *"I really want rat to be a single binary easy
to execute, like docker or podman."* The binary already IS that for the core flow
(`install.sh → rat init/add/up/call`, stranger-verified at `rat/6.18+`) — but `make`
still leaks into three experiences: the QUICKSTART (`make rat-build`,
`make stateplugin-image`), the author scaffold (`FROM localhost/rat/plugin-base-*:dev`
forces a clone + `make plugin-base-*`), and the platform demo (ADR-053). Docker and
podman are themselves make-built projects — their users never see it. That's the
posture to adopt explicitly.

## Decision

**Every user, author, and operator path is a `rat` verb plus published GHCR artifacts.
`make` exists only for hacking on rat's own core.** Concretely:

1. **The image matrix ships with every release** (release.yml): the daemon and the
   plugin-base images (multi-arch, already shipping) gain `rat-v3-stateplugin`
   (multi-arch; the quickstart demo plugin) and the four platform reference plugins —
   `rat-v3-dbt-runner`, `rat-v3-state-postgres`, `rat-v3-secret-env`,
   `rat-v3-scheduler-cron` (amd64 first; arm64 when someone asks — their pip installs
   under QEMU don't justify the CI minutes yet).
2. **The scaffold defaults to the published bases**: `rat plugin init` emits
   `FROM ghcr.io/squat-collective/rat-v3-plugin-base-{py,go}:latest` — an author
   anywhere runs `rat plugin init → check → test → pack → publish` with **zero clone,
   zero make**. The `localhost/rat/plugin-base-*:dev` form stays documented in the
   emitted Dockerfile as the SDK-hacker override (`make plugin-base-*` from a clone).
3. **`rat validate` learns real pull semantics.** The probe's "the podman runtime does
   not pull at launch" claim was wrong — the runtime launches via `podman run`, which
   auto-pulls. The probe now passes a locally-absent image if it resolves remotely
   (`podman manifest inspect`), with a "will pull at launch" note; an unresolvable ref
   stays an error (the typo'd-tag Degraded-loop is still the enemy).
4. **The QUICKSTART is zero-make and zero-clone**: install.sh → manifests → `rat add
   --image ghcr.io/squat-collective/rat-v3-stateplugin:latest` → `rat up` (the pull
   proves itself) → call → C5 deny → audit → down. Building from source moves to a
   "hacking on rat" footnote.

## Consequences

**Positive.** The docker-grade first touch; plugin authors need nothing but the binary
and podman; the scaffold's output is honest out of the box; the preflight stops
overclaiming.

**Negative — accepted.** Releases get slower (four more image builds); scaffold output
depends on GHCR availability (the localhost override is the escape hatch); `:latest`
base refs mean authors ride the newest SDK unless they pin (documented in the emitted
Dockerfile).

**Neutral.** Contributor tooling (`make verify/conformance/gen-sdks/breaking`) is
untouched — that's the project's own toolchain, the same place docker's own Makefile
lives.

## Alternatives considered

1. **Embed builds in the binary (`rat build-base` etc.).** Re-implements podman inside
   rat for no gain; the binary orchestrating podman is the docker model already.
2. **Keep localhost bases + document the make step.** That's today's friction — a clone
   requirement disguised as a Dockerfile default.
3. **Multi-arch for all plugin images now.** QEMU pip builds cost real CI minutes for
   zero current arm64 demand on the *demo* plugins; the author-facing bases stay
   multi-arch (M-series laptops are real).

## Related

- [ADR-051](051-publish-apache2-squat-collective.md) — the publication this completes.
- [ADR-053](053-the-demo-lives-outside-the-core.md) — the demo's independence (the third `make` leak).
- [ADR-026](026-plugin-authoring-and-packaging.md) — the authoring flow this un-clones.
- `roadmap/backlog.md` DX-2-residual — PyPI stays the one deferred channel.
