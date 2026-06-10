# ADR-050 — Plane-file env interpolation (`${VAR}`)

**Status:** Accepted (2026-06-10)

## Context

The DX review's #1 operator frustration was config duplication in the platform bundle:
the same facts (the Postgres DSN, the MinIO credentials, infra endpoints) are stated up
to six times across `compose.yaml`, `compose.infra.yaml`, `plugins.yaml`, and the dbt
`profiles.yml` — and a typo in one copy passes every static layer (backlog DX-5).
Compose already interpolates `${VAR}` from an env file, so its half of the duplication
is solvable with a single fact sheet. The rat plane file (`plane.yaml` / `plugins.yaml`
/ the `rat.toml`-materialized plane) could not interpolate anything — which is also why
`plugins.yaml` had to inline the entire `RAT_SECRETS` credentials blob as a literal in a
committed file.

## Decision

`planeFromRaw` expands **`${VAR}`** (braced form only) from the **process environment**
in the plane's string values: the listen `addr`, each plugin's `endpoint`, its
`launch.image`, and every `launch.env` value.

- **Bare `$VAR` stays literal** — only `${…}` interpolates (no surprise expansion in
  values that legitimately contain `$`).
- **An undefined variable is a load error**, not an empty string — the error names the
  variable. Fail-loud is the house philosophy (`rat validate` runs the same loader, so
  the preflight catches a missing var before boot).
- **`$${` escapes a literal `${`** (none exist in-repo today).
- The platform bundle gains **`platform/.env`** as the single fact sheet: flat simple
  tokens (users, passwords, hosts, ports) consumed by compose natively and by the plane
  via `set -a; . ./.env; set +a` before `rat serve`. Structured values (DSNs, the
  `RAT_SECRETS` JSON) stay in the files that need them, **composed from** the tokens —
  structure stays visible, facts live once.

## Consequences

**Positive.** One source of truth for the demo platform's facts; credentials leave the
committed plane file; the add-a-plugin checklist loses its "now grep five files" step;
`rat validate` catches a typo'd variable by name before boot.

**Negative — accepted.** A plane file is no longer guaranteed self-contained: it may
depend on ambient environment. Mitigations: the braced-only syntax makes the dependency
visible in the file; the loader's error names the missing variable; the convention is a
`.env` next to the plane (committed when it holds no secrets, like the demo's).

**Neutral.** No wire change, no manifest change — this is plane-file (operator-side)
semantics only. Planes without `${` behave byte-identically.

## Alternatives considered

1. **Generate compose/plane entries from manifests.** The full fix for duplication, but
   a real tool with real design questions (which topology is the source?), and the
   manifests deliberately don't know operator facts (endpoints, credentials). Deferred —
   interpolation removes most of the pain at ~40 lines.
2. **rat auto-loads `.env` like compose does.** Rejected: rat is not compose; implicit
   file loading hides the dependency the braced syntax is meant to surface. `source` (or
   a process supervisor's `EnvironmentFile=`) keeps the contract explicit. Revisit if
   the ceremony stings.
3. **`os.Expand` with `$VAR` + `${VAR}`.** Rejected: bare-`$` expansion would corrupt
   values that legitimately contain `$` (connection strings, generated tokens).

## Related

- [ADR-019](019-rat-serve-daemon.md) / [ADR-022](022-plugins-are-launched-not-composed.md) — the plane file this extends.
- [ADR-031](031-durable-local-storage.md) — the `.rat/` runtime dir convention the `.env` sits beside.
- `roadmap/backlog.md` ⑤ DX-5 — the frustration this closes.
- [docs/guides/building-a-platform.md](../../guides/building-a-platform.md) — the operator-facing documentation.
