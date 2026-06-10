# Building a platform — choosing a topology, adding plugins, surviving day 2

A **platform is the rat core plus a chosen plugin set** — nothing more. The core is the
same six things everywhere; what makes *your* platform yours is which plugins it runs and
how they're brought up. Today there are **four ways to express that plugin set** (a
`rat.toml` project, an attach-mode `plane.yaml`, a launch-mode `plugins.yaml`, and the
socket-mount variant of launch mode), and they grew in that order for real reasons — but
nothing signposts them, config facts repeat across files, and secrets follow four different
patterns. This guide is the signpost: which front door to pick, what you'll actually touch
when you add a plugin, and where the sharp edges still are. It is deliberately honest about
the gaps; the queued fixes live in [roadmap/backlog.md](../../roadmap/backlog.md).

## Choosing your topology

| | **`rat.toml` project** | **`plane.yaml` attach** | **`plugins.yaml` launch** | **socket-mount** |
|---|---|---|---|---|
| Bring-up | `rat init` / `rat add` / `rat up` | `rat serve --plane plane.yaml` | `rat serve --plane plugins.yaml` | `make platform-socket` (rat runs *as* a container) |
| Who runs the plugins | rat launches them (`local` or `podman` runtime) | **you do** (compose, systemd, k8s sidecars); rat dials `endpoint:` | rat launches each from its `launch.image`, reconciler-supervised | same as launch, but rat drives the **host's** podman over a mounted socket; plugins are siblings on a shared network |
| Spec style | command-written TOML (poetry model, [ADR-023](../architecture/adrs/023-rat-as-a-per-project-daemon.md)) — never hand-edited | hand-written YAML | hand-written YAML ([ADR-022](../architecture/adrs/022-plugins-are-launched-not-composed.md)) | the same `plugins.yaml` |
| Durability | `.rat/data/<plugin>` mounted at `/data` ([ADR-031](../architecture/adrs/031-durable-local-storage.md)) | whatever your runner gives the containers | **ephemeral** (no `/data` mount on a raw `--plane`) | ephemeral |
| Self-healing | yes (reconciler) | **no** — rat only attaches; your runner restarts things | yes (relaunch + re-wire on crash) | yes |
| Live add/remove | yes — `rat add`/`rat remove` materialize against the running daemon ([ADR-027](../architecture/adrs/027-live-plugin-control-rpc.md)) | no | no (edit YAML, restart) | no |
| Pick it when | solo/local dev on one project; the default | the plugins already exist as services you operate (the demo's compose stack) | you want rat as the launcher but manage the plane file yourself | you want everything containerized — the k8s-shaped demo ([ADR-022](../architecture/adrs/022-plugins-are-launched-not-composed.md) socket-mount) |

Rules of thumb:

- **Start with a `rat.toml` project.** It's the only front door with durable plugin data,
  live add/remove, and a per-project control socket (`.rat/daemon.sock` — many rats coexist).
- **Attach mode is for plugins you already run.** rat becomes pure gateway+registry: no
  launching, no healing, no live control (the admin RPC is launch-mode only). The cost is
  yours to carry — every plugin is a compose service you wrote.
- **A plane is all-launch or all-attach.** Mixing `launch:` and `endpoint:` entries in one
  plane is rejected at boot — *"plane mixes launch and attach plugins — not supported in
  v1"* ([core/cmd/rat/main.go](../../core/cmd/rat/main.go), `assemble`). **Register-only
  driver entries** (no image, no endpoint — an operator identity like `platform-runner`)
  are fine in either.
- **Socket-mount is launch mode with rat itself containerized**: it drives the host's
  rootless podman through the user socket and dials sibling containers by name over podman
  DNS — the same shape as k8s pod-to-pod. See
  [platform/run-socket-mount.sh](../../platform/run-socket-mount.sh).

All four converge on the same daemon: one gateway, C5 authorization, mandatory audit.
The topology changes who *starts* plugins, never how they're *called*.

## Adding a plugin to a platform

The honest checklist, per topology. There is **no cross-file validator yet** — nothing
checks that the manifest, plane entry, image tag, and secret refs agree (a thin
`rat plugin validate` and a `rat preflight` are queued — backlog EC-1 / O-2). A typo
surfaces at boot or at first call, not at edit time. Budget for that.

**`rat.toml` project — one command:**

```bash
rat add my-plugin --image rat/my-plugin:dev --manifest path/to/my-plugin.plugin.yaml --env RAT_FOO_REF=ref://foo
# or, if the image was `rat plugin pack`ed (manifest stamped in, ADR-026):
rat add --image ghcr.io/you/my-plugin:0.1.0
```

`rat add` appends the `[[plugin]]` block to `rat.toml`, materializes the manifest under
`manifests/`, and — if the project's daemon is running — registers it live (no restart;
`--no-live` opts out, `--with-deps` auto-adds marketplace providers for unsatisfied
`requires`). You still own: building/pushing the image, and seeding any secret the new
plugin's `ref://` env vars point at.

**`plane.yaml` attach — five touches:**

1. the manifest under `manifests/` (`provides`/`requires` — this *is* the C5 policy);
2. a `plane.yaml` entry (`name` + `manifest` + `endpoint: service:port`);
3. a **full compose service block** (~15 lines: image or pip-install command, env,
   healthcheck, `depends_on`) — this is the per-plugin infra tax
   [ADR-022](../architecture/adrs/022-plugins-are-launched-not-composed.md) was written
   to escape;
4. its config/credentials as compose `environment:` (plaintext today — see Secrets);
5. restart the stack.

**`plugins.yaml` launch (incl. socket-mount) — four touches:**

1. the manifest under `manifests/`;
2. a `plugins.yaml` entry (`name` + `manifest` + `launch: {image, isolation, env}`);
3. an **image** (a `Makefile` build target — see `plugin-images` — typically `FROM`
   the SDK base images of [ADR-026](../architecture/adrs/026-plugin-authoring-and-packaging.md));
4. if it needs credentials: a new `ref://` entry in the secret plugin's store (today: the
   `RAT_SECRETS` JSON blob on the `rat-secret` entry), plus the ref string in the new
   plugin's `env`.

In every topology the same fact can live in 2–4 places (image tag in Makefile + plane;
endpoint in compose + plane; ref names in secret store + consumer env). Until the
validator lands, change them together and grep before you boot.

## Secrets today

Four patterns coexist in the demo platform. Know which one you're looking at:

| Pattern | Where you see it | Grade |
|---|---|---|
| **Compose env plaintext** | [platform/compose.yaml](../../platform/compose.yaml): `POSTGRES_PASSWORD`, `RAT_S3_SECRET=minioadmin`, full DSNs with passwords inline | demo-only. Real deployments must not ship credentials in compose files. |
| **`RAT_SECRETS` JSON blob** | [platform/plugins.yaml](../../platform/plugins.yaml): the `rat-secret` plugin (env-py) is *seeded* with one env var holding the whole `tenant → ref → value` map | demo-only — but it concentrates every value into **one trust boundary** (the secret plugin), which is the right shape. |
| **`ref://` resolution** | consumers carry only refs (`RAT_STATE_PG_REF=ref://state/pg-dsn`, `RAT_LAKE_PG_REF`, …) and resolve them via `rat://secret/v1/resolve` — gateway-routed, C5-authorized, audited | **the production contract.** No credential in any plane file or consumer env. |
| **dbt `env_var()` interpolation** | [platform/dbt-project/profiles.yml](../../platform/dbt-project/profiles.yml): `{{ env_var('RAT_S3_KEY', 'minioadmin') }}` — the dbt-runner resolves refs, then exports the values as env vars for dbt | a bridge into dbt's own config language; the committed *defaults* are demo creds. |

Plainly: **a production secret backend does not exist yet.** The `secret/v1/resolve`
contract is what production swaps in behind (Vault, KMS, cloud secret managers); the env-py
store is the only implementation today. Pattern 3 is the one to build on — patterns 1 and 4's
literal values are demo conveniences, and pattern 2 is the seed mechanism for the demo store.

## Durability & `.rat/`

- **`rat up` projects get durable plugin data.** The daemon sets the runtime's data root to
  the project's `.rat/data/`, so each launched plugin gets `.rat/data/<plugin>/` mounted at
  `/data` — surviving restart, crash-relaunch, and redeploy
  ([ADR-031](../architecture/adrs/031-durable-local-storage.md)). Under the I9 profile the
  rootfs is read-only and `/tmp` is a tmpfs, so **anything a plugin writes outside `/data`
  is gone on relaunch**. Plugins opt in by configuration (e.g. `RAT_STATE_DB=/data/state.db`).
- **Raw `rat serve --plane` is ephemeral.** No project → no data root → no `/data` mount.
  Launch- and socket-mode planes (the demo's `plugins.yaml`) keep durable state in *services*
  (Postgres, MinIO) instead — that's why the demo's infra exists.
- **What lives in `.rat/`:** `daemon.sock` (the per-project control socket — the default
  listen addr), `daemon.pid`, `daemon.log` (`rat up -d` redirects the daemon's output here,
  including the audit tee), `audit.jsonl` (the **append-only durable C5 audit log**, one
  JSON line per gateway decision — [ADR-046](../architecture/adrs/046-native-observability.md)),
  and `data/` (the durable mounts above). The whole directory is gitignored (`.rat/.gitignore`
  contains `*`).
- **Don't delete `.rat/` casually.** `data/` is your plugins' embedded state (sqlite/duckdb
  ledgers, import staging); `audit.jsonl` is your decision trail; deleting the socket/pid
  under a running daemon orphans it. Known wart: the per-plugin data dirs are created
  world-writable (0777) so the container uid can write them (ADR-031 accepted cost).

## Day-2: observing & debugging

- **What's running:** `rat status` (this project's daemon: pid, addr, socket) and `rat ls`
  (every running rat on the machine — read from `~/.local/state/rat/instances.json`).
  For launched plugin containers: `podman ps` / `podman logs <name>`.
- **The audit trail:** every gateway decision is one JSON line
  (`{"kind":"decision","capability":…,"caller":…,"allowed":…,"reason":…}`) — always on
  stdout, and durably in `.rat/audit.jsonl` for projects. `grep capability` over the
  daemon's logs shows the control hops end-to-end.
- **Metrics:** set `RAT_METRICS_ADDR` and the core serves Prometheus text at `/metrics` —
  `rat_gateway_calls_total` (by outcome) and `rat_plugin_up` (per plugin), dependency-free
  ([ADR-046](../architecture/adrs/046-native-observability.md)).
- **Crash loops:** in launch mode the reconciler relaunches a failed plugin on an
  exponential, capped backoff; at the crash-loop cap the plugin is marked **Degraded** and
  left alone (no relaunch hammering). A Degraded plugin shows up in status/metrics; fix the
  cause and bring the plane up again.
- **The honest wart: misconfig surfaces late.** A bad manifest path fails at boot with an
  error; but a wrong capability in `requires`, a missing secret ref, or a mismatched image
  tag surfaces only at first call (a C5 deny, a resolve failure, a launch error). There is
  no preflight validation pass yet — it's queued work (see the backlog). Until then, the
  audit log's `"allowed":false` lines and `podman logs` on the failing plugin are your
  debugging front line.

## Federation

One machine routinely runs several rats (per-project daemons), and **`rat hub`**
([ADR-033](../architecture/adrs/033-workspace-federation-hub.md)) is the front door across
them: a gateway-of-gateways that reads the same instance registry as `rat ls` and routes
capability calls to the right workspace (`rat call … --addr <hub> --workspace <name>`). It
sits *above* planes — not a seventh core thing. **Local federation works today; the
cross-machine story (NATS-leaf, outbound-only) is future work** (ADR-033 Q01), and the hub
is localhost/trusted-network only until the identity plugin + TLS land
([ADR-034](../architecture/adrs/034-security-responsibility-model.md)).

## Related

- [platform/README.md](../../platform/README.md) — the batteries-included demo platform
  (all of attach, launch, and socket-mount, runnable).
- [ADR-020](../architecture/adrs/020-data-platform-bundle.md) — the data platform bundle.
- [ADR-021](../architecture/adrs/021-orchestrator-pipelines-as-code.md) — pipelines are
  code you submit; rat is a pure orchestrator.
- [ADR-022](../architecture/adrs/022-plugins-are-launched-not-composed.md) — launch mode +
  socket-mount; adding a plugin is one declaration.
- [ADR-023](../architecture/adrs/023-rat-as-a-per-project-daemon.md) — the `rat.toml`
  project model.
- [ADR-031](../architecture/adrs/031-durable-local-storage.md) — the `/data` mount.
- [ADR-033](../architecture/adrs/033-workspace-federation-hub.md) /
  [ADR-034](../architecture/adrs/034-security-responsibility-model.md) — federation + the
  security responsibility model.
