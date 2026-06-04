# ADR-031: Durable local storage — the per-project `/data` mount (and, later, configurable volumes)

## Status: Accepted (2026-06-04) — phase 1 built (Phase 10)

## Context

Plugins launch under the **I9 profile** — **read-only rootfs**, only `/tmp` (a tmpfs) writable —
so an embedded store (a sqlite ledger, a duckdb file, a working/staging dir) is **ephemeral**: a
`/tmp/state.db` is wiped on restart. The conversation on the durability spectrum landed three legs:

| what you're storing | mechanism | durability |
|---|---|---|
| control-plane KV (state, configs) | a state-backend **service** | the service owns its disk; migrate through the `state/v1` contract |
| data-plane bulk (parquet, files) | the **storage axis** (S3) | object storage; data already durable |
| **genuinely-local embedded** (sqlite/duckdb, caches, **migration/import staging**) | a **volume** | ← this ADR |

The local-embedded leg is real (not a grudging escape hatch): sqlite/duckdb for latency or no-S3
deployments, and staging a bulk import/migration before it lands. The good news — the podman
runtime **already has the mechanism**: a `DataRoot` field that mounts a per-plugin host directory
(`<DataRoot>/<plugin>`) at `/data`, surviving `Terminate`+relaunch. It was simply **never wired**
into the `rat up` path. So phase 1 is *activation*, not new machinery — and needs **no proto change**.

## Decision

Ship durable local storage in two phases; **phase 1 is the 90% case, built now.**

### Phase 1 (now): the per-project `/data` mount

`rat up` sets the podman runtime's `DataRoot` to the project's **`.rat/data/`** directory. Each
launched plugin then gets `<project>/.rat/data/<plugin>/` mounted at **`/data`** — the one writable,
**persistent** path under I9. It survives daemon restart / crash-relaunch / redeploy, and it's
`gitignored` (under `.rat/`, ADR-023), so durable data never lands in the repo.

A plugin **opts in** by writing to `/data` — e.g. `RAT_STATE_DB=/data/state.db` in its `rat.toml`
env. The plugin's *default* path stays `/tmp/...` so it still launches with **no** mount (a raw
`rat serve --plane`, and crucially `rat plugin pack`'s standalone verify, where `/data` isn't
mounted). Persistence is a per-plugin **configuration choice**, not a default that breaks the gate.

### Phase 2 (deferred): configurable volumes

When a plugin needs *more than one* mount, a custom path, a named volume, or read-only — an
additive `LaunchSpec.volumes` (`repeated VolumeMount{name, mount_path, read_only}`) + a `rat.toml`
`[[plugin.volume]]` block, with each deployment-runtime mapping it (podman `-v`, k8s PVC). The
single `/data` convention covers the common case; this generalizes it. Deferred until a plugin
actually needs it — see Q01.

## Consequences

**Positive.**
- Durable sqlite/duckdb (and import/migration staging) **today, no proto change** — just activating
  an existing, tested mechanism.
- Per-project, gitignored, survives restart — the obvious mental model (`.rat/data/` is "this
  project's local plugin data").
- Keeps the default ephemeral, so `rat plugin pack` and raw `rat serve` are unaffected.

**Negative — accepted.**
- The host-dir bind is **0777** (forced past umask) so the non-root container uid can write it — a
  world-writable dir is a known wart; a uid-mapping refinement is Q03.
- `.rat/data/` grows on the host (operator manages it; a `rat remove --purge` is Q04).
- **Socket-mount mode** (rat *is* a container driving the host podman) has different host-path
  semantics — phase 1 targets **host-mode podman** (rat as a host binary, the kitchen's setup);
  socket-mount durability is Q02.
- A single `/data` per plugin is **not tenant-scoped** — a multi-tenant plugin namespaces inside it
  (same rule as creds/secrets).

**Neutral.** Local-process runtime is a no-op for this (no container, the plugin uses host paths
directly).

## Open questions

- **Q01 — Configurable volumes** (phase 2): the additive `LaunchSpec.volumes` for multi-mount /
  custom-path / named-volume / read-only needs. Build when a plugin needs more than `/data`.
- **Q02 — Socket-mount durability:** the host-path the host's podman binds when rat itself is a
  container; needs an explicitly host-side `DataRoot`.
- **Q03 — uid-mapping vs 0777** for the data dir (`--userns`/`:U`), so it's owned by the plugin uid
  instead of world-writable.
- **Q04 — `rat remove --purge`** to delete a removed plugin's `/data` dir (today it lingers).
- **Q05 — k8s mapping:** `/data` → a PVC + volumeMount (the k8s deployment-runtime).

## Alternatives considered

- **Configurable `LaunchSpec.volumes` first.** Rejected for phase 1: the single `/data` convention
  is the 90% case and ships with zero proto change; generalize once a plugin needs it.
- **Named podman volumes** (vs the host-dir bind). Deferred: the host-dir bind is inspectable
  (`ls .rat/data/<plugin>`) and gitignore-able; named volumes are a phase-2 option per mount.
- **No local durability — push everything to service/S3 backends.** Rejected: the local-embedded
  leg is genuinely needed (the sqlite→postgres migration discussion + no-S3/latency cases).

## Migration

Phase 1 (this ADR's build): wire `DataRoot = <project>/.rat/data` in the daemon's `rat up`/serve
path; no proto change, `make breaking` clean. Phase 2: the `LaunchSpec.volumes` proto, when needed.

## Related

- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime axis +
  the I9 profile this works within.
- [ADR-022](022-plugins-are-launched-not-composed.md) — Q4 launch-spec extensions (the phase-2 home).
- [ADR-023](023-rat-as-a-per-project-daemon.md) — the per-project `.rat/` dir `/data` lives under.
- The "durability spectrum" conversation (state→service · bulk→storage · local-embedded→volume).
