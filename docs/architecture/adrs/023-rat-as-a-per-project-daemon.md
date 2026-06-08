# ADR-023: rat is a per-project daemon — poetry-style hybrid control over an external spec, isolated per instance

## Status: Accepted (2026-06-03) — built + proven (see roadmap/done.md)

## Context

Phase 2 produced a working `rat serve --plane plugins.yaml`: a daemon that launches a
declared plugin set, reconciles it, heals it, and routes capability calls through the
gateway ([ADR-019](019-rat-serve-daemon.md), [ADR-022](022-plugins-are-launched-not-composed.md)).
But the *shape of the daemon* — how a person installs it, addresses it, configures it, and
runs more than one — was never decided. Three threads forced the question:

1. **Distribution** (ideas/inbox 2026-06-03): rat should install as a **GHCR binary + image**,
   not `git clone` + `make`. A *user* never builds.
2. **What rat fundamentally IS.** We weighed two poles: an **immutable appliance** (truth lives
   in an external declaration; the process is disposable) vs. a **living broker** (boots empty;
   you add plugins live; the running process *is* the source of truth). The first is GitOps /
   immutable-infra; the second is dockerd / a database server / VS Code-with-extensions.
3. **Many rats on one machine.** A solo dev has several projects. If rat is "the platform for a
   project," then — like a Python venv — **each project has its own daemon**, and several run
   side by side.

The decisive realization: "Docker image vs. server you add to" conflates three independent
axes — *distribution* (is it an image?), *control* (mutable at runtime?), and the load-bearing
one, *source of truth* (external declaration vs. internal process state). The whole industry
learned the same lesson (Terraform, Kubernetes, GitOps): infrastructure whose truth lives *in
the running server* becomes a snowflake you cannot reproduce. But the ecosystem lesson
(npm, Cargo, VS Code, **poetry**) is the opposite: low-friction `add` is how plugin ecosystems
grow, and editing YAML-then-redeploy is hostile to that.

**poetry already resolved this exact tension.** Its imperative commands (`poetry add`) *mutate
a declarative manifest* (`pyproject.toml`) and update a lock (`poetry.lock`); `poetry install`
reconstructs from them. The file is the truth, committed to git — but it is **command-written,
never hand-edited**, and the venv is *derived*. That is the model rat should adopt, with the
running daemon playing the role of the venv.

This ADR also revisits one [ADR-019](019-rat-serve-daemon.md) decision: that `rat` (server) and
`ratctl` (client) ship as **two binaries**. That separation is an *architectural* boundary worth
keeping; whether it must be two *artifacts* is a distribution choice, re-decided here.

## Decision

**rat is a small, disposable per-project daemon whose desired state lives in an external,
git-committed spec. Control is hybrid (poetry-style): imperative commands write the spec and
then reconcile; declarative `install` rebuilds from it. The running registry is *status*,
never the source of truth. Many instances run on one machine, fully isolated.** Seven parts:

### 1. Daemon-first, shipped as both a binary and an image

`rat` is a long-running daemon (the six things). It ships **two ways, no tradeoff**:
- a **static binary** on a GitHub Release / `ghcr.io` → `curl … -o rat && chmod +x ./rat`
  (the founding `chmod +x ./rat` path, the solo front door), and
- the **daemon image** `ghcr.io/rat-dev/rat:<tag>` for the containerized / socket-mount / k8s
  path ([ADR-022](022-plugins-are-launched-not-composed.md)).

Plugins are themselves GHCR images, pulled and launched by the daemon — never bundled into it.
The daemon stays the six things; DuckDB, dbt, Postgres-drivers, etc. live **inside their
plugins**, beside the daemon, in every tier.

### 2. The source of truth is an EXTERNAL spec; rat is config-stateless

A project carries two committed files:
- **`rat.toml`** — the desired plugin set + config (the *spec*).
- **`rat.lock`** — pinned plugin **image digests** (reproducibility).

rat reads these on boot and reconciles reality to them. **rat holds no durable desired-state
store of its own** — restart = re-read the spec = identical. The live registry of running
plugins is **status** (observed), derived from the spec, exactly as Kubernetes separates `spec`
(truth) from `status` (observed). The git repo is the backup; there is nothing else to back up,
migrate, or corrupt in the control plane. *Run history, applied projects, and other operational
data are NOT rat's state — they live in the state-backend plugin already (ADR-021).*

### 3. Hybrid control, poetry-shaped — imperative writes the spec, then reconciles

The user-facing surface mirrors poetry; the spec is **command-written, never hand-edited**:

| rat | poetry analog | effect |
|---|---|---|
| `rat init` | `poetry init` | write `rat.toml` shell (no plugins, no daemon) |
| `rat add <ghcr-ref>` | `poetry add` | record in `rat.toml` + pin in `rat.lock`; if a daemon is up, hot-register |
| `rat remove <name>` | `poetry remove` | inverse |
| `rat install` | `poetry install` | reconstruct the platform from `rat.toml`+`rat.lock` (fresh checkout / CI / prod) |
| `rat lock` | `poetry lock` | re-resolve + re-pin digests |
| `rat up` / `down` / `status` / `ls` | (the running env) | start/stop/inspect this project's daemon |
| `rat apply` / `run` / `logs` | — | operate (ADR-021) |

**The drift-killing rule:** `rat add` writes the spec **first**, *then* reconciles the live
daemon. There is never a window where the running state is the only record. A live edit is never
canonical; `rat diff` (live vs. `rat.toml`) reports drift as a defect to reconcile, like
`terraform plan`. This is what keeps the hybrid honest and prevents the snowflake failure mode.

### 4. rat is per-project (a venv with a heartbeat)

Each project root holds its own platform:
```
my-project/
  rat.toml        # committed — the spec
  rat.lock        # committed — pinned digests
  .rat/           # gitignored — runtime: daemon.sock, daemon.pid, logs/
```
Mirrors `pyproject.toml`/`poetry.lock` (committed) + `.venv` (gitignored). The CLI is
**project-scoped by cwd**: every command walks up from the working directory to find `rat.toml`
(like git / poetry / cargo) and operates on *that* project's daemon. Local development is
therefore **many small rats** (one per project); the cloud topology is **one big rat** (one
control plane + a tenancy plugin, on k8s) — same binary, different topology, which is the
founding thesis made operational.

### 5. Instances are isolated — mandatory, because many run on one machine

Coexistence is a first-class requirement, and it dictates three controls:

- **Control transport is a per-project unix socket** (`.rat/daemon.sock`), not a fixed TCP port.
  Several daemons never collide; filesystem permissions are the access control. A TCP listener is
  **opt-in** (remote control, a UI) and carries its own auth (Q05).
- **Deployment-runtime resources are namespaced per instance.** Each instance has a stable id
  (from `rat.toml`'s `name` + a short hash of the project path). Launched plugins get an
  instance-prefixed network (`rat-net-<instance>`) and container names
  (`<instance>-<plugin>-<seq>`). *This directly extends the ADR-022 podman sibling mode, whose
  current single hardcoded `rat-net` + `<plugin>-<seq>` names would collide across instances.*
- **A machine-global instance registry** (`~/.local/state/rat/instances.json`, written on
  up/down) backs `rat ls` — the `docker ps` of rat daemons.

### 6. `rat add` resolves the capability graph (the poetry resolver analog)

poetry's value is its **resolver** ("A needs B≥2"). rat has the exact analog over
**capabilities**: the registry already validates `requires`/`provides` at launch, so `rat add`
surfaces it at add-time — *"dbt-runner requires `rat://state/v1/get`; no provider registered →
add a state-backend?"* No new machinery; it exposes what the registry already knows, poetry-style.

### 7. One artifact, boundary preserved (refines ADR-019)

`rat` is a single multi-call binary: `rat serve`/`up` (daemon) and `rat add`/`apply`/`call`
(client) are the same executable. [ADR-019](019-rat-serve-daemon.md)'s **architectural**
orchestrator/client boundary stays (they remain distinct concerns, distinct code paths, and a
client still reaches the daemon only over the socket/gateway — no in-process shortcut). What
changes is the **packaging**: one downloadable `rat`, not `rat` + `ratctl`. This matches how the
user already speaks ("`rat apply`", not "`ratctl apply`"). `ratctl` becomes an alias / is folded in.

## Consequences

**Positive.**
- **Reproducible by construction.** External spec + lock ⇒ lose a daemon, `rat install`, identical.
  Disaster recovery is free; the repo is the backup.
- **Stateless control plane.** Nothing in rat to back up, migrate, or corrupt — the heavy
  "operate-a-stateful-server" burden of the living-broker model is avoided.
- **Ecosystem ergonomics.** `rat add <ref>` is as low-friction as `npm i` / a VS Code extension —
  which is how plugin ecosystems actually grow (a core RAT thesis), *without* sacrificing GitOps.
- **Clean isolation + mental model.** Per-project = a venv; no cross-project interference; many
  daemons coexist by construction.
- **Same binary, every tier.** Solo `chmod +x ./rat` → containerized → k8s, distribution and run
  model unchanged.

**Negative — accepted.**
- **Resource cost: N projects up = N plugin sets running.** Two projects using dbt-runner = two
  containers (like two venvs both holding `requests`). Mitigation is behavioral (`rat up` only
  what you work on, `rat down` the rest) and optional shared backends via *plugin config* (one
  Postgres, project-scoped DB) — a plugin concern, not a core one.
- **Two-sources-of-truth risk** (spec vs. live registry) is real. Bought down by the atomic
  write-then-reconcile rule, `rat diff`, and "live is never canonical" — but it is a discipline
  the implementation MUST hold, not a property we get free.
- **Per-instance namespacing complicates the deployment-runtime** (network + name
  parameterization) — concrete work on code ADR-022 shipped.
- **Unix-socket-default means remote control is opt-in**, needing an explicit TCP endpoint + auth
  (Q05) — a small friction for the UI/remote case in exchange for collision-free local multi-run.

**Neutral.**
- `plugins.yaml` (ADR-022) becomes `rat.toml` — largely a rename plus command-management and a
  lock file; the low-level `rat serve --plane <file>` stays as an escape hatch.
- The reconciler/`SetProvider` hot-wire built in Phase 2 is exactly the machinery `rat add`'s
  live-register needs — this ADR mostly *exposes* existing capability behind a poetry surface.

## Open questions

- **Q01 — `rat.toml` format.** TOML (the poetry feel, rarely hand-edited) vs. reuse the existing
  YAML plane parser. Lean TOML for the front door; decide whether to keep one parser internally.
- **Q02 — lock semantics.** How `rat.lock` resolves + pins (pull image manifest digests at
  add/lock time); behavior offline; how `rat install` verifies digests.
- **Q03 — auto-start.** Should `rat run`/`rat apply` lazily `up` the daemon if it's down? Lean:
  explicit `rat up`, with lazy-start as an opt-in convenience.
- **Q04 — resolver strength.** At `rat add`, is an unmet `requires` a warning, a hard block, or an
  auto-suggested install? Lean warn + suggest; block only on `rat up`.
- **Q05 — remote control auth.** When a TCP endpoint is enabled (UI/remote), the plugin-and-client
  auth model (ties to ADR-007 identity + the parked plugin↔core auth question).
- **Q06 — instance identity.** Derive `<instance>` from `rat.toml` `name` + a short project-path
  hash (two dirs sharing a basename must not collide on runtime resource names). Confirm the scheme.
- **Q07 — self-registering plugins.** The federated variant (a plugin running elsewhere dials in
  to register) stays *status*, quarantined + unauthenticated until promoted into the spec — never
  silently canonical. Out of scope here; a future ADR if/when multi-host is real.

## Alternatives considered

1. **Immutable appliance (pure declarative; no live `add`).** Truth external, but every change is
   edit-file-then-redeploy. *Rejected:* hostile to exploration and to the plugin ecosystem; the
   friction that low-friction `add` exists to remove. (We keep its *spine* — external truth — and
   add imperative ergonomics on top.)
2. **Living broker / system-of-record (pure imperative; internal truth).** Boots empty; you mutate
   it live; it persists its own state. *Rejected:* recreates snowflake servers and config drift —
   the precise problem immutable infra was invented to kill — and turns rat into a stateful system
   to back up, migrate, and upgrade, pulling durable-store responsibilities toward the six-thing
   core.
3. **One machine-wide daemon hosting many projects (one rat, many planes).** *Rejected for local:*
   shared blast radius, local multi-tenancy complexity, weaker isolation. Not wrong in general —
   it **is** the cloud topology (one control plane + a tenancy plugin on k8s), so it is the *other
   end of the same axis*, not a competitor to per-project-local.
4. **Fixed TCP port for control.** *Rejected:* collides the moment two daemons run; a per-project
   unix socket is collision-free, filesystem-scoped, and access-controlled by default.
5. **Keep `ratctl` as a separate binary (ADR-019 as-is).** *Refined, not rejected:* the
   architectural boundary stays; only the *artifact* unifies into one `rat`.

## Migration

- **Spec:** `plugins.yaml` → `rat.toml` (+`rat.lock`); `rat serve --plane <file>` retained as the
  low-level escape hatch beneath the poetry verbs.
- **Binary:** fold `ratctl` into `rat` (one module already); `ratctl` kept as an alias during
  transition.
- **Build order is the prototype that follows this ADR — "slice 1" (the load-bearing, code-touching
  pieces) before the ergonomic verbs:**
  1. **Per-project unix socket + cwd project discovery** (kills the fixed-port collision; makes
     commands project-aware).
  2. **Instance-namespaced deployment-runtime** (network + name prefix) — prove **two rats coexist
     on one machine** without collision.
  3. Then layer the poetry verbs (`init`/`add`/`install`/`lock`) + `.rat/` layout + `rat ls`.
- The GHCR release pipeline (binary + multi-arch images) is the distribution half, tracked from the
  ideas inbox; it gates the `curl … chmod +x` story but not the model.

## Related

- [ADR-019](019-rat-serve-daemon.md) — `rat serve` daemon + the rat/ratctl split this **refines**
  (boundary kept, artifact unified).
- [ADR-022](022-plugins-are-launched-not-composed.md) — launch-not-compose + the podman runtime
  this **extends** with per-instance network/name namespacing.
- [ADR-021](021-orchestrator-pipelines-as-code.md) — pipelines as submitted code (`rat apply`); run
  history in the state-backend (why rat itself needs no durable store).
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime axis.
- ideas/inbox 2026-06-03 *(GHCR distribution; single `rat` binary)* and 2026-06-02 *(runtime
  self-registration — promoted here; the `SetProvider` keystone it needed now exists)*.
- Prior art: **poetry** (`pyproject.toml`/`poetry.lock`, command-as-manifest-writer, the resolver),
  **Cargo**, **Kubernetes** (spec/status; `kubectl` imperative-or-declarative over external truth),
  **docker contexts** (per-daemon addressing).
