# ADR-022: Plugins are launched, not composed — adding a plugin is one declaration (socket-mount local, Kubernetes prod)

## Status: Proposed (2026-06-02)

> **The complaint that drove this:** in the ADR-019/020 platform, adding a plugin means
> hand-writing a whole **compose service** (image, install command, env, healthcheck,
> `depends_on`) **plus** a plane entry **plus** a manifest. That is backwards — *adding a
> plugin should be almost nothing.* This ADR makes rat **launch** plugins (it already can —
> [ADR-016](016-plugin-provisioning-via-deployment-runtime.md): "the core launches, it
> doesn't dial"): **adding a plugin = one entry in `plugins.yaml`; rat does launch → inject
> config + secrets → wire deps → healthcheck → connect → register.** The deployment-runtime
> is **socket-mount locally** (rat-in-a-container drives the host container socket, the
> docker/k8s-daemon model) and **Kubernetes in prod** (rat asks the API to run the plugin —
> no socket, no DinD). Secrets live in a **secret plugin**, never in the infra.

## Context

**What I built, and why it's wrong.** [ADR-019](019-rat-serve-daemon.md) gave the platform
**attach mode**: compose starts *every* plugin as a container and rat merely *connects* to
the already-running ones. I chose attach for the compose stack to avoid **docker-in-docker**
(a containerized rat launching other containers). The cost is exactly the frustration: the
**infra grows by one compose service per plugin** — `engine`, `catalog`, `state`,
`scheduler`, `pipeline`, `bff` are each a ~15-line compose block with a pip command, env,
and a healthcheck. Adding a plugin is heavy, repetitive infra surgery. That makes rat "a
compose file with extra steps," not an orchestrator.

**v3 already has the right primitive.** [ADR-016](016-plugin-provisioning-via-deployment-runtime.md)
established the **deployment-runtime axis** — *the core launches plugins, it does not dial
pre-running ones* — with `LaunchSpec` (image + isolation + non-secret env) and two
references (local-process, podman). The platform simply didn't use it, because a
*containerized* rat launching *containers* needs access to a container runtime. That access
is a **solved, standard pattern** (every CI runner, every k8s kubelet, the Docker daemon
does it) — I over-avoided it.

**The requirement (Tom).** Adding a plugin should be a **single small declaration**; rat does
the rest. The infra should be **tiny and stable** — rat + the raw backends + a secret plugin
— and *not grow per plugin*. Secrets are **stored once in a secret plugin** and referenced by
name; no credentials in the infra. KISS.

## Decision

### 1. rat launches plugins. Adding one is a single declaration.

A platform has **one** file listing the plugins rat runs. Adding a plugin = adding an entry —
no compose service, no healthcheck, no endpoint, no `depends_on`:

```yaml
# plugins.yaml — the desired plugin set. This is the WHOLE thing you edit to add a plugin.
runtime: local            # local (socket-mount) | k8s
plugins:
  - name: catalog
    image: rat/ducklake:dev
    secrets: [pg-password]
    config:  { metadata_host: postgres }

  - name: dbt-runner
    image: rat/dbt-duckdb:dev
    needs:    [catalog, storage]     # plugin deps — rat wires them
    secrets:  [s3-creds]             # references secrets in the secret plugin
```

For each entry rat: **launches** the image (deployment-runtime) → **injects** non-secret
`config` (env) → lets the plugin **fetch its `secrets`** from the secret plugin → **wires
`needs`** to the providers (capability composition) → **health-checks** it → **dials** it →
**registers** it. The reconciler keeps it alive (restart on crash). The operator wrote one
entry.

### 2. The deployment-runtime: socket-mount locally, Kubernetes in prod

*Where* the container runs is a plugin axis (ADR-016). Two profiles, selected by one line
(`runtime:`):

- **`local` — socket-mount.** rat runs in a container with the **host container socket**
  mounted (`/run/podman/podman.sock` or `/var/run/docker.sock`). It launches plugin
  containers as **siblings** on a shared rat network and dials them by name. This is the
  **docker/k8s-daemon model** — exactly what Tom pointed at with "like k8s or docker." One
  small, documented privilege (the socket ≈ host root); fine for local/dev.
- **`k8s` — the Kubernetes deployment-runtime.** rat calls the **k8s API** to create a
  Pod/Deployment + Service per plugin and dials the Service. **No socket, no DinD** — rat is
  a normal client of the cluster (a ServiceAccount + RBAC). The production answer.

Same `plugins.yaml`, same "add a plugin = one line"; only the runtime plugin differs. (The
existing `podman` runtime is the seed of the socket-mount path; a `k8s` runtime is new.)

### 3. The infra shrinks to the bootstrap

The compose/helm/manifest the operator deploys is **fixed** — it does **not** grow per
plugin:

```
rat (container, socket mounted) + Postgres + MinIO + secret-plugin
        └─ reads plugins.yaml → launches everything else (engine, catalog, runner, …)
```

Postgres + MinIO are raw backends (not RAT plugins). The **tier-0** plugins rat needs to
function — **state-backend, secret-backend** — are launched first from `plugins.yaml`; the
deployment-runtime itself is rat's *boot config* (the socket / the kubeconfig), not a launched
plugin (it's what does the launching — no chicken-and-egg). The desired plugin set is read
from the **file** at boot, so it doesn't depend on the state plugin to start.

### 4. Secrets live in the secret plugin, never in the infra

The launch contract already **forbids secrets in `LaunchSpec.Env`** (ADR-016). So:

- store secrets **once** in the secret plugin (`rat secret set s3-creds …`, or a sealed file);
- a plugin declares `secrets: [s3-creds]` and **fetches them at startup** via
  `rat://secret/v1/get` (rat wires it to the secret plugin + hands it a per-plugin token);
- `plugins.yaml` carries **no** credentials; the infra carries **no** credentials.

This is the "store one or two secrets on a communicating secret plugin, and that's it" Tom
described.

## Consequences

- **Adding a plugin is one declaration.** No compose, no boilerplate. The infra is tiny and
  stable; the plugin set is *data* (`plugins.yaml`), not *infrastructure*.
- **rat becomes a real orchestrator** — it owns plugin lifecycle (launch, health, restart)
  via the deployment-runtime + reconciler, the same way k8s owns Pods.
- **One model, local → prod.** Socket-mount and k8s are the *same contract* (the
  deployment-runtime axis); a platform graduates by changing `runtime:` and the runtime
  plugin, not its `plugins.yaml`.
- **Secrets are centralized and out of the infra.**
- **Cost / negatives (accepted):**
  - **Socket-mount is a privilege.** Mounting the host container socket into rat is
    root-equivalent on that host. Accepted for **local/dev** and documented; production uses
    the k8s runtime (RBAC-scoped), not a socket.
  - **A Kubernetes deployment-runtime must be built** (new reference plugin: Pod/Deployment +
    Service + status mapping).
  - **It resurfaces the mutable-provider gap.** rat launching + the reconciler *restarting* a
    plugin requires the **gateway to re-bind a provider connection at runtime** —
    `gateway.New` fixes its provider map at construction (the Phase-A reconciler-rewire
    finding + the parked runtime-registration idea). Launch-with-lifecycle **depends on**
    adding a concurrency-safe `gateway.SetProvider`/adopt path. This ADR makes that gap
    load-bearing.
  - **Image distribution becomes real** — plugins are images; you need a registry (or local
    builds/dev tags). Networking of socket-launched sibling containers (shared rat network vs
    published ports) is an implementation detail (Q3).
  - **Secret bootstrap** — how a freshly-launched plugin authenticates to the secret plugin
    (the per-plugin launch token) must be specified (Q4).

## Alternatives considered

1. **Attach mode + a compose service per plugin (status quo).** Rejected: the infra grows per
   plugin; adding a plugin is heavy. Attach stays valid only for genuinely *external*,
   already-running services (a managed Postgres, a shared engine) — not the default.
2. **Docker-in-docker (nested daemon).** Rejected vs. socket-mount: DinD runs a second daemon
   inside rat (heavy, storage-driver pain); socket-mount reuses the host daemon — the standard
   "daemon launches containers" pattern.
3. **rat on the host (bare-metal binary) launching containers.** Viable and DinD-free, but Tom
   wants rat itself containerized; socket-mount gives that. (The bare-metal path remains
   supported — same launch contract, no socket needed.)
4. **Secrets in `plugins.yaml` / compose env.** Rejected — the whole point. Secrets go in the
   secret plugin; the launch contract forbids secret env.
5. **k8s-only (no local launch).** Rejected: local dev needs a zero-cluster path — socket-mount
   is it.

## Open questions

- **Q1 — the `plugins.yaml` schema.** Final fields: `name`, `image`, `runtime?`, `needs`,
  `secrets`, `config`, `isolation` (the I9 profile), resources. Is this the plane file
  ([ADR-019](019-rat-serve-daemon.md)) evolved, or a sibling?
- **Q2 — dependency wiring.** How a launched plugin learns a peer's connection: pure capability
  composition (the plugin calls `needs`' capabilities — e.g. `storage/vend`, a
  `lake/vend-connection`), vs. rat injecting endpoints as env. Lean: capabilities for secrets +
  connections, env only for static config. **Partially resolved (2026-06-03):** the one
  topology-dependent address — *how a driver plugin dials the gateway BACK* — is injected by rat
  as `RAT_GATEWAY` (computed per mode: `host.containers.internal` on the host, rat's own
  shared-net name when socket-mounted, loopback for local processes), so the SAME `plugins.yaml`
  runs both topologies unchanged. Peer-to-peer connections still go through capabilities.
- **Q3 — socket-mount networking. ✅ RESOLVED (2026-06-03) — shared user-defined network, dial
  by name** (the lean held). The podman runtime gained a SIBLING mode (`NewPodmanNetworked` /
  `$RAT_PODMAN_NETWORK`): every launched plugin joins a shared `rat-net` under a stable
  `--name`, and the runtime returns `<name>:50051` as the endpoint — rat resolves it via podman
  DNS. NOT host-published ports: a containerized rat's `127.0.0.1` is its own netns, so a
  sibling's host port is unreachable from rat; a name on the shared net is. This is also exactly
  the k8s pod-to-pod-by-service-name shape (the prod target). I9 holds — a user bridge is still a
  private netns that drops the 169.254 metadata route. **Built + proven end-to-end:** `rat AS A
  CONTAINER` (socket-mounted, `--user 0` for the rootless host socket) launched all 4 platform
  plugins as siblings, they interconnected by name, self-drove, and recorded durable run history
  — `make platform-socket` / [`platform/run-socket-mount.sh`](../../../platform/run-socket-mount.sh).
- **Q4 — secret bootstrap.** The per-plugin token the launch hands a plugin so it can
  authenticate to the secret plugin (ties to the cross-cutting "plugin-to-core auth" in
  [plugin-architecture.md](../../../.claude/rules/plugin-architecture.md)).
- **Q5 — the gateway re-bind. ✅ RESOLVED (2026-06-02).** `gateway.SetProvider`/`RemoveProvider`
  (concurrency-safe, `-race` clean) + a reconciler `Rewire` Bind/Unbind hook fired on health
  transitions; the launch-mode daemon (`launchPlane`) wires it so a relaunched plugin re-binds at
  its new endpoint. The keystone this ADR made load-bearing — now built + tested.
- **Q6 — image distribution.** Registry vs. local build for dev images; how `plugins.yaml`
  refers to images (tags, digests).

## Related

- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the deployment-runtime axis
  ("the core launches"). This ADR makes **launch the default** for the platform and adds the
  **socket-mount** + **k8s** profiles.
- [ADR-019](019-rat-serve-daemon.md) — `rat serve`, launch vs **attach**. This **redirects the
  platform from attach → launch**; attach stays for external services only.
- [ADR-021](021-orchestrator-pipelines-as-code.md) — *the infra declares only plugins; pipelines
  are code.* This is its companion: **how those plugins get there with near-zero effort.**
- [ADR-001](001-everything-is-a-plugin.md) — the deployment-runtime + secret-backend are plugin
  axes; this is the launch story for all of them.
- Phase-A reconciler-rewire finding ([roadmap/backlog.md](../../../roadmap/backlog.md)) +
  runtime self-registration ([ideas/inbox.md](../../../ideas/inbox.md)) — the **mutable-provider
  gap (Q5)** this ADR makes load-bearing.
- `.claude/rules/plugin-architecture.md` — tier-0 (state/deployment-runtime/secret) bootstrap;
  plugin-to-core auth (Q4).
