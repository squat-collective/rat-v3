# ADR-019: `rat serve` — assembling the sealed core into a runnable daemon

## Status: Proposed (2026-06-02)

> Scope-only. This ADR proposes *what to build and where the seams are*; it does not
> start the build. Ratify (and pick the open questions) before any `core/cmd/` code lands.

## Context

The Phase-1 core is **sealed** (`rat/2.0`): registry, gateway, supervisor,
deployment-runtime (local-process + podman), reconciler, lease, arrowticket — all built
and **tested against real launched plugins**. But it is a tested Go **library, not a
runnable server.** There is no entrypoint: `core/gateway` is constructed *in tests* via
`gateway.New(reg, providers, auditor, …)` with provider connections injected, and served
over `bufconn`. The only `main.go` files under `core/` are **test plugins**.

The data-dev-plane experiment surfaced the gap concretely (the conversation that
produced this ADR; see also the experiment's finding **F9**):

> **"core components built + tested" ≠ "core runs as a server a client can connect to."**

Because no core server runs, the experiment's VS Code UI talks to a hand-rolled **BFF
stand-in** ([`experiments/data-dev-plane/gateway`](../../../experiments/data-dev-plane/))
that *plays* the front-door role and hosts the plugins in-process. Asked "why not use
the core API gateway?", the honest answer is: **there is nothing running to point at.**
`rat serve` closes that gap.

**Onboarding goal (added 2026-06-02).** The daemon must itself be **containerizable** so we
can later ship a **beginner compose stack** — a `docker compose up` / `podman compose up`
that brings up the core daemon **+ a base set of plugins + the infra they need**, giving a
newcomer a working data-dev plane in one command. This is the on-ramp end of the vision's
range ("scales from `chmod +x ./rat` to multi-tenant cloud — same binary, different plugin
sets"): `./rat serve` for the solo hacker, `compose up` for the batteries-included starter,
the same daemon either way.

### What already exists (the assembly is mostly built)

| capability | where | note |
|---|---|---|
| manifests → launch → healthcheck → dial → register → gateway | `supervisor.BringUp(ctx, rt, []PluginSpec) → *Plane{Gateway, Registry}` | the assembly, one call |
| graceful teardown | `Plane.Shutdown(ctx)` | stops launched plugins |
| launch a plugin (process/container) | `deploymentruntime.NewLocalProcess()` / `NewPodman()` | `exec.Command(spec.Image)` / podman |
| capability routing + C5 + audit | `gateway.New(...)` → `corev1.CapabilityInvokeServiceServer` | already a gRPC service impl |
| ongoing supervision (crash-loop backoff+jitter) | `reconciler.New(...)` + `Loop.Run(ctx)` | sre#4, built |
| manifest index by capability | `registry.New([]*manifest.Manifest)` | built |

### What is missing (the daemon glue)

1. **An entrypoint** — `core/cmd/rat/main.go` (no binary exists).
2. **Config / a "plane file"** — tests hardcode `[]supervisor.PluginSpec`; a daemon reads
   the desired plugin set (manifests + launch specs) from disk.
3. **A real network listener for the gateway** — `corev1.RegisterCapabilityInvokeServiceServer(grpcServer, plane.Gateway)` + `net.Listen("tcp", addr)` + `Serve` (today: `bufconn`).
4. **Lifecycle** — signal handling (SIGINT/SIGTERM) → `plane.Shutdown`; drain in flight.
5. **Wire ongoing supervision** — run `reconciler.Loop` so a crashed plugin restarts
   (today `BringUp` is one-shot).

So `rat serve` is **mostly glue**: config + a listener + lifecycle + the reconcile loop,
over the sealed assembly. The core was built to be assembled; this assembles it.

## Decision

Build `rat serve` as a **thin daemon over the sealed core**, in three phases: **A** proves
the daemon against the core's *existing Go test plugins* (no new plugin work); **B** fronts
the data-dev (Python) plane with it; **C** ships the beginner compose stack. The daemon is
distributed as both a static binary (`./rat`) and a container image (`rat serve` in compose).

### Two runtime modes — launch vs attach (the key to compose-without-DinD)

The daemon supports two ways to get its providers, because "the core runs in a container
that orchestrates plugin containers" would otherwise mean docker-in-docker:

- **launch mode** (`--runtime local|podman`): the daemon **launches** plugins itself via the
  deployment-runtime (`supervisor.BringUp`) and supervises them (`reconciler.Loop`). This is
  the `./rat serve` solo path and what Phase A/B prove.
- **attach mode** (`--plane` entries carry an `endpoint:` instead of a `launch:`): the daemon
  **dials already-running** plugins and just registers + fronts them — it does **not** launch
  them. This is what the **compose stack** uses: **compose is the orchestrator** (it starts
  every plugin container), and `rat serve` connects to them by service name. No socket-mount,
  no DinD. This mode is already half-built — `gateway.New(reg, providers, …)` takes externally
  dialed connections, exactly as `core/composition/composition_realproviders_test.go` does;
  attach mode is "build the `providers` map from `endpoint:`s" instead of from `BringUp`.

The two modes are the same daemon + gateway; only *where providers come from* differs.

### Phase A — the daemon MVP (`core/cmd/rat serve`)

```
rat serve --plane plane.yaml [--addr 127.0.0.1:7777] [--runtime local|podman]
```

1. Parse `plane.yaml` → `[]supervisor.PluginSpec` (manifest + launch spec per plugin).
2. `rt := deploymentruntime.New{LocalProcess,Podman}()`.
3. `plane, err := supervisor.BringUp(ctx, rt, specs)` — launch + healthcheck + register + gateway.
4. `grpcServer := grpc.NewServer(...)`; `corev1.RegisterCapabilityInvokeServiceServer(grpcServer, plane.Gateway)`; `net.Listen("tcp", addr)`; `Serve`.
5. `reconciler.New(rt, desired, cfg)`; `go loop.Run(ctx)` — keep plugins healthy.
6. Trap SIGINT/SIGTERM → cancel ctx → `plane.Shutdown` → `grpcServer.GracefulStop`.

**The plane file** (the daemon's desired-state input):

```yaml
# plane.yaml — which plugins this RAT plane runs
addr: 127.0.0.1:7777
runtime: local            # or: podman
plugins:
  - name: rat-catalog
    manifest: ./manifests/catalog.plugin.yaml
    launch: { image: ./bin/catalogplugin, isolation: i9 }
  - name: rat-engine
    manifest: ./manifests/engine.plugin.yaml
    launch: { image: ./bin/engineplugin, isolation: i9 }
```

An **attach-mode** entry swaps `launch:` for `endpoint:` — the daemon dials it instead of
launching it (what the compose stack uses; compose started the container):

```yaml
  - name: rat-engine
    manifest: ./manifests/engine.plugin.yaml
    endpoint: rat-engine:7001        # a compose service the daemon connects to
```

**Phase-A exit criteria:** `rat serve --plane plane.yaml` boots the core's Go test
plugins via the deployment-runtime and serves the gateway on a TCP port; an external
client (grpcurl / a tiny Go client / the generated TS SDK) invokes a capability through
it with **C5 enforced + an audit record emitted**; SIGTERM drains cleanly. This is the
first time the core *runs*.

### Phase B — front the data-dev plane through `rat serve`

Make the experiment's plugins real plugins the daemon manages:

1. **Containerize the Python plugins** (engine / catalog / strategy / storage) — a
   `Dockerfile` each, `CMD ["python","main.py"]`, honoring `RAT_PLUGIN_ADDR`. Required
   because the launch contract execs `image` directly (`exec.Command(spec.Image)`, **no
   args** in local-process) — so a Python plugin needs either a container image (podman
   runtime) or a wrapper binary. Containerizing is the intended path (experiment §2: "each
   plugin a container").
2. **Image manifests + launch specs** → a `data-dev-plane.yaml`.
3. `rat serve --plane data-dev-plane.yaml --runtime podman` runs the **real** ML lakehouse
   under the actual core: the strategy routes its `engine.execute` / `catalog.commit` hops
   through the **real `core/gateway`** (C5 + audit + context-stamping), not the Python
   composition stand-in.
4. The **VS Code UI points at the real core gateway** for *control* (catalog browse,
   `strategy.Apply`) via the TS SDK; the **BFF shrinks to the F9 data-leg only** (serving
   query *rows*, since the reference engine's Arrow leg stays in-proc until a real Flight
   engine retires it).

**Phase-B exit criteria:** `make data-dev-remote` (or a new target) runs the pipeline with
the plugins **launched and mediated by `rat serve`**, search works end-to-end, and the
control hops are visible in the core's audit log.

### Phase C — the beginner compose stack (batteries-included on-ramp)

`docker compose up` → a working data-dev plane, no Go/Python toolchain on the host:

1. **A daemon image** — a `Dockerfile` building the `rat` static Go binary into a minimal
   image (`distroless`/`alpine`), entrypoint `rat serve --plane /etc/rat/plane.yaml --attach`.
2. **A compose file** (`deploy/data-dev-starter/compose.yaml`) that brings up, on one network:
   - the **rat-serve** daemon (in **attach mode**),
   - the **base plugins** as their own containers (the Phase-B images): `duckdb-ml` (engine),
     `ducklake` (catalog), `minio-s3` (storage), `incremental-embed` (strategy),
   - the **infra** those need: **MinIO** (S3) + **Postgres** (DuckLake metadata),
   - *(optional)* the **BFF + nothing else UI-side**, so the VS Code extension connects to one
     URL and the data-leg works out of the box.
   Compose is the orchestrator; the daemon **attaches** to the plugin services by name — so
   there is **no docker-in-docker, no socket mount**. A beginner runs one command and has a
   lakehouse + ML + a UI endpoint.
3. **"Base plugins"** = the minimal set that makes data-dev work end-to-end: one engine, one
   catalog, one storage, one strategy, + MinIO + Postgres. Swapping/adding plugins is editing
   the compose file + the plane file — the "different plugin sets, same daemon" promise.

**Phase-C exit criteria:** a fresh machine with only podman/docker runs
`compose up` in `deploy/data-dev-starter/` and, within a minute, the VS Code extension (or
`grpcurl`) drives the pipeline + semantic search against the **compose-managed plane fronted
by the real core gateway**. The README's "getting started" is two lines.

## Explicitly OUT of scope (v1)

- Multi-node / cross-host leader election (the `lease` primitive exists; v1 is single-node).
- The full tier-0 backends beyond minimal: real state-backend / identity / NATS bus
  (v1 uses local-process + an in-memory/stdout auditor; bootstrap plugins are a follow-on).
- mTLS / per-plugin tokens on the gateway listener (PU-1 channel-auth is a frozen MUST for
  the *bytes* leg; the control listener hardening is a separate hardening pass).
- **REST** alongside gRPC (the six-thing core names gRPC+REST; v1 ships gRPC only — REST/
  grpc-gateway is additive later).
- The **F9 data-leg** (a real Arrow Flight engine) — orthogonal to the daemon.

## Open questions (decide on ratify)

1. **Default runtime for v1** — `local-process` (simplest, Phase-A) with `podman` proven in
   Phase-B? (Lean: yes.)
2. **Python-plugin launch** — containerize (podman) vs. a wrapper-binary path for
   local-process. (Lean: containerize; it's the real deployment story.) Does this want a
   small additive `args`/`command` on `LaunchSpec`, or is `image`-only fine? (Frozen-wire
   check required if we touch the proto.)
3. **Auditor sink** — stdout/file in v1, or require an `audit-log` plugin? (Lean: stdout/
   file; the mandatory-audit invariant holds without a plugin.)
4. **Where the binary lives** — `core/cmd/rat/` (in the sealed module) vs. a new module.
   Touching `core/` post-seal: is `cmd/` additive-safe to the seal? (It adds a `main`, no
   change to the tested packages — but confirm against the `rat/2.0` tag policy.)
5. **Phase placement** — this is "finish Phase-1 deployability," but it shades into Phase 2
   (running the platform), which is **user-pull-gated** (Gate B, ≥10 users). Is `rat serve`
   in-gate (it's make-it-real, not GTM) or does it wait? (Recommend: in-gate — it validates
   the deployment topology, principle #8, and unblocks the "use the real gateway" story.)
6. **Attach-mode supervision** — in attach mode the daemon doesn't launch plugins, so the
   reconciler can't restart them; compose owns lifecycle/restart. Does the daemon still
   *health-check* attached providers (report Degraded, drop from routing) or stay dumb and
   trust compose? (Lean: health-check + report, don't restart.)
7. **The beginner "base plugin" set + where the compose stack lives** — `deploy/data-dev-starter/`?
   Is the starter stack part of this ADR's deliverable or its own follow-on once A/B land?
   (Lean: scope it here, build it in Phase C after B proves the images.)

## Consequences

- **Turns "components + scaffolding" into a running platform.** The single most valuable
  make-it-real step the experiment surfaced: the core stops being a library and becomes a
  thing you point clients at.
- **The BFF shrinks to its honest minimum** (the F9 data-leg), and the UI's control path
  becomes the *real* core gateway via the TS SDK — retiring the "why not the core gateway?"
  gap.
- **Validates the deployment topology for real** (CLAUDE.md principle #8): plugins launched
  by the deployment-runtime, mediated by the enforcing gateway, kept alive by the
  reconciler — the architecture proving itself as a process, not a test.
- **Delivers the on-ramp** the vision promises: `./rat serve` for the solo hacker and a
  one-command `compose up` starter for beginners — *the same daemon*, launch mode vs attach
  mode. The two-mode design gets compose orchestration **without docker-in-docker**, and
  makes "different plugin sets, same binary" a config edit. This is the first concrete piece
  of the ecosystem on-ramp (backlog EC-1).
- **Cost:** containerizing the Python plugins (Phase B), a possible additive `LaunchSpec`
  field (frozen-wire check), the daemon lifecycle + attach-mode code, a daemon image, and a
  compose file to maintain (Phase C). All additive; no change to the sealed tested packages.

## Alternatives considered

1. **Keep the BFF stand-in (status quo).** Cheapest, but never validates that the real core
   runs — the gap stays open and "use the core gateway" stays impossible. Rejected as the
   end-state; fine as the interim.
2. **A one-shot CLI** (`rat run --plane … --pipeline …`) that brings up, runs one pipeline,
   tears down. Simpler, but not a server — clients can't connect to a living plane, and the
   reconciler/health story isn't exercised. Rejected; a daemon is the point.
3. **Jump to the full Phase-2 platform** (multi-node, real tier-0 backends, REST, mTLS).
   Too big; violates minimal-surface discipline. The phased A→B keeps each step provable.

## Related

- [ADR-014](014-spike-core-registry-and-invoke-gateway.md) — the registry + invoke gateway this serves.
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the launch contract Phase B leans on.
- [ADR-005](005-capability-invocation-model.md) / [ADR-007](007-call-context-transport.md) — what the gateway enforces on every routed call.
- [`experiments/data-dev-plane/README.md`](../../../experiments/data-dev-plane/README.md) §10 **F9** — the BFF/data-leg this retires (mostly).
- `core/supervisor/supervisor.go` (`BringUp`/`Plane`), `core/gateway/gateway.go`, `core/reconciler/loop.go` — the assembly being wrapped.
