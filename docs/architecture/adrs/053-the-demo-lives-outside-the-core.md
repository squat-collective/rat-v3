# ADR-053 — The demo lives outside the core (`rat-v3-demo`)

**Status:** Accepted (2026-06-11)

## Context

The platform demo (`platform/`: the dbt medallion, compose infra, plane files, bff UI
backend, sample landing data) lives inside the core repo and only runs from a clone
plus `make plugin-images`/`make platform-up`. The maintainer's call: *"the demo should
be outside the core — it should absolutely work independently."* The org already has
the graduation precedent: `rat-data-dev` (the ML-lakehouse experiment) moved out at the
restructure and published independently.

## Decision

1. **`platform/` graduates to `github.com/squat-collective/rat-v3-demo`** — a
   self-contained, public, Apache-2.0 repo: the launch-mode plane (`plugins.yaml`),
   manifests, `.env` fact sheet, the dbt project + landing data, the infra compose
   (Postgres + MinIO), the bff source, and a README whose only prerequisites are
   **the `rat` binary, podman, and this repo** — no rat-v3 clone, no make.
2. **Image ownership follows source ownership.** The four reference plugins the demo
   launches (dbt-runner, state-postgres, secret-env, scheduler-cron) live in rat-v3's
   `plugins/` and are published by **rat-v3's release** (ADR-052). The bff is
   demo-specific glue: its source moves with the demo, and **rat-v3-demo's own CI**
   publishes `ghcr.io/squat-collective/rat-v3-demo-bff`. The demo's `plugins.yaml`
   references all five by GHCR ref — `rat serve` pulls them at launch.
3. **rat-v3 sheds the demo surface**: the `platform-*` make targets, `plugin-images`,
   and the compose attach-mode demo path leave the core repo with the directory. The
   attach-mode topology remains documented in the guide (it's an architecture fact);
   its worked example now lives in the demo repo.
4. The move is a **fresh-history extraction** (the `rat-data-dev` pattern): the demo's
   past stays in rat-v3's history; the new repo starts at its graduation commit with a
   provenance note.

## Consequences

**Positive.** A stranger runs the full medallion with three ingredients (binary +
podman + tiny repo); the core repo drops ~all of its non-platform content; demo
iterations stop rippling through core seals; "everything is a plugin" finally includes
the demo itself.

**Negative — accepted.** Two-repo coordination: a wire-visible change in rat-v3 can
require a demo-repo follow-up (mitigated: the demo pins image tags; `:latest` floats by
default with pinning documented). The bff needs its own one-job CI. Cross-repo doc
links replace relative ones.

**Neutral.** `make composition` and the core test suite never touched `platform/` —
core gates are unaffected.

## Alternatives considered

1. **Keep the demo in-repo, publish only images.** Still requires the rat-v3 clone for
   the plane/dbt files — fails "absolutely independent".
2. **Embed a demo in the binary (`rat demo`).** Demo content inside the core is the
   exact opposite of the instruction, and bloats the binary with dbt fixtures.
3. **Fold the demo into `rat-data-dev`.** Different audiences: rat-data-dev is the ML
   experiment showcase; this is the front-door product demo. Separate doors.

## Related

- [ADR-052](052-the-binary-is-the-interface.md) — the posture this completes.
- [ADR-020](020-data-platform-bundle.md) / [ADR-022](022-plugins-are-launched-not-composed.md) — the demo this graduates.
- [ADR-038](038-reference-plugins-live-under-plugins.md) — the `rat-data-dev` graduation precedent.
