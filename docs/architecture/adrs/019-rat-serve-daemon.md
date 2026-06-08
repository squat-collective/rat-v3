# ADR-019: `rat serve` — the runnable core daemon + the beginner compose stack

## Status: Accepted (2026-06-02)

> **Ready to build.** This ADR is self-contained: a fresh session can execute it cold from
> the *Implementation map* + *Build phases* + *Kickoff checklist* below, with no other
> context. All prior open questions are **resolved** (see *Decisions*). Decisions are firm
> per ADR tone rules; if one is wrong, supersede with a new ADR — do not silently drift.

## Context

The Phase-1 core is **sealed** (`rat/2.0`): registry, gateway, supervisor, deployment-runtime
(local-process + podman), reconciler, lease, arrowticket — all built and **tested against real
launched plugins**. But it is a tested Go **library, not a runnable server.** `core/gateway` is
constructed *in tests* via `gateway.New(reg, providers, auditor, …)` with provider connections
injected, served over `bufconn`. The only `main.go` files under `core/` are **test plugins**.

The data-dev-plane experiment surfaced the gap (finding **F9** + the "why not use the core
gateway?" thread):

> **"core components built + tested" ≠ "core runs as a server a client can connect to."**

Because no core server runs, the experiment's VS Code UI talks to a hand-rolled **BFF
stand-in** ([`experiments/data-dev-plane/gateway`](../../../experiments/data-dev-plane/)) that
*plays* the front-door role and hosts the plugins in-process. There is nothing running to point
a client at. `rat serve` closes that gap, and — packaged as a container — becomes the on-ramp
end of the vision's range: **`./rat serve` for the solo hacker, `compose up` for the
batteries-included beginner, the same daemon either way.**

This is the **Phase 2 (Solo deployment) kickoff** ([phases.md](../../../roadmap/phases.md)): its
"done when" is `rat run` end-to-end + a <60s front-door demo — exactly what the daemon + the
compose stack deliver. The data-dev experiment already produced the bundle's reference plugins
(engine/catalog/storage/strategy/ui).

### What already exists (the assembly is built — this is mostly glue)

| capability | exact API | note |
|---|---|---|
| manifests → launch → healthcheck → dial → register → gateway | `supervisor.BringUp(ctx, rt, []PluginSpec, auditor, healthTimeout, descriptors…) (*Plane, error)` | the whole launch assembly, one call |
| the running plane | `Plane{ Gateway *gateway.Gateway; Registry *registry.Registry }` + `Plane.Shutdown(ctx)` | teardown closes conns + kills instances |
| one plugin to bring up | `PluginSpec{ Manifest *manifest.Manifest; Launch *deploymentruntimev1.LaunchSpec }` | `Launch == nil` ⇒ register-only (a caller/driver) |
| launch a plugin | `deploymentruntime.NewLocalProcess()` / `NewPodman()` | local: `exec.Command(spec.Image)` + env; podman: image |
| capability routing + C5 + audit | `gateway.New(reg, providers, auditor, descriptors…) *Gateway` (a `corev1.CapabilityInvokeServiceServer`) | already a gRPC service impl |
| audit sink interface | `gateway.Auditor interface{ Record(AuditRecord) }` (`gateway.MemAuditor` exists) | implement a stdout one |
| load a manifest | `manifest.Load(path string) (*Manifest, error)` / `manifest.LoadDir(dir)` | reads a `plugin.yaml` |
| index by capability | `registry.New([]*manifest.Manifest) (*Registry, error)` | built |
| ongoing supervision | `reconciler.New(rt, []Desired, Config) *Reconciler` + the loop in `core/reconciler/loop.go` | crash-loop backoff+jitter (sre#4) |
| register the gateway on a server | `corev1.RegisterCapabilityInvokeServiceServer(grpcServer, plane.Gateway)` | (tests use bufconn) |
| proto descriptors for routing | `catalogv1.File_rat_catalog_v1_catalog_proto`, `enginev1.File_…`, … (one per axis) | pass the **union** of all axes the plane may route |

### What is missing (the daemon glue)

1. **An entrypoint** — `core/cmd/rat/main.go` (no binary exists).
2. **A plane file** — tests hardcode `[]PluginSpec`; the daemon reads it from disk (schema below).
3. **A real network listener** — `net.Listen("tcp", addr)` + `grpcServer.Serve(lis)` (today: bufconn).
4. **Lifecycle** — SIGINT/SIGTERM → cancel ctx → `plane.Shutdown` → `grpcServer.GracefulStop`.
5. **Wire the reconcile loop** (launch mode) so a crashed plugin restarts; **attach-mode health-check** (report Degraded, don't restart — compose owns restart).
6. **Attach assembly** — a launch-free path that dials `endpoint:`s and calls `registry.New` + `gateway.New` directly (modeled on `core/composition/composition_realproviders_test.go`). Add as `supervisor.Attach(ctx, specs, auditor, descriptors…) (*Plane, error)` or inline in `cmd/rat`.

## Decision

Build `rat serve` as a **thin daemon over the sealed core**, distributed as a static binary
**and** a container image, in **three phases** (A→B→C), each independently provable.

### Two runtime modes — launch vs attach (the key to compose-without-DinD)

Same daemon + gateway; only *where providers come from* differs:

- **launch mode** (`runtime: local|podman`): the daemon **launches + supervises** plugins via
  `supervisor.BringUp` + the reconciler loop. The `./rat serve` solo path (Phase A/B).
- **attach mode** (a plane entry carries `endpoint:` instead of `launch:`): the daemon **dials
  already-running** plugins and only registers + fronts them. **Compose is the orchestrator**
  (it starts every plugin container); the daemon connects by service name. **No docker-in-docker,
  no socket mount.** This is what the Phase-C compose stack uses.

### The plane file (the daemon's only config — full schema)

```yaml
# plane.yaml — the desired plugin set for one RAT plane
addr: 0.0.0.0:7777          # gateway listen address (gRPC)
runtime: local              # local | podman   (ignored for attach-only planes)
health_timeout: 10s         # per-plugin readiness wait (launch mode)
plugins:
  - name: rat-catalog                 # MUST equal manifest metadata.name
    manifest: ./manifests/catalog.plugin.yaml
    # exactly ONE of launch:/endpoint: per plugin —
    launch:                           # launch mode: the daemon starts it
      image: ./bin/catalogplugin      # local: a binary path · podman: a container image
      isolation: i9                   # the I9 profile (non-root, cap_drop ALL, …)
      env: { FOO: bar }               # optional; NEVER secrets
  - name: rat-engine
    manifest: ./manifests/engine.plugin.yaml
    endpoint: rat-engine:7001         # attach mode: the daemon dials this (compose service)
```

Validation: every plugin has a `manifest` + exactly one of `launch`/`endpoint`; `name` matches
`manifest.metadata.name`; a plane mixing both modes is allowed.

### Phase A — the daemon MVP (`core/cmd/rat serve`), proven on Go test plugins

```
rat serve --plane plane.yaml
```

Steps (all exact APIs in the *Implementation map* above):
1. Parse `plane.yaml`; for each plugin `manifest.Load(p.manifest)` → `PluginSpec` (Launch set
   from `launch:`, or recorded as an attach endpoint).
2. `rt := deploymentruntime.NewLocalProcess()` (Phase A default).
3. **launch:** `plane, err := supervisor.BringUp(ctx, rt, specs, auditor, healthTimeout, descs…)`.
4. `srv := grpc.NewServer()`; `corev1.RegisterCapabilityInvokeServiceServer(srv, plane.Gateway)`;
   `lis, _ := net.Listen("tcp", addr)`; `go srv.Serve(lis)`.
5. `reconciler.New(rt, desired, cfg)` + run the loop (`core/reconciler/loop.go`) to keep plugins healthy.
6. Block on a signal; on SIGTERM/SIGINT → cancel ctx → `plane.Shutdown(ctx)` → `srv.GracefulStop()`.

Also write: `core/cmd/rat/auditor.go` (a `StdoutAuditor{}` implementing `gateway.Auditor` —
`Record(r)` → one JSON line to stdout), minimal `plugin.yaml` manifests for the chosen test
plugins, and `go build` the test plugins to `./bin/`.

**Exit criteria (A):** `rat serve --plane plane.yaml` boots `core/testplugins/{catalogplugin,
stateplugin,…}` via the deployment-runtime and serves the gateway on TCP; an external client
(`grpcurl` or a tiny Go client invoking `corev1.CapabilityInvokeService/Invoke`) routes a
capability through it with **C5 enforced + an audit line emitted**; an undeclared capability is
**denied**; SIGTERM drains cleanly. *First time the core runs as a server.*

### Phase B — front the data-dev plane through `rat serve` (the real gateway)

1. **Containerize the Python plugins** (engine `duckdb-ml`, catalog `ducklake`, storage
   `minio-s3`, strategy `incremental-embed`): a `Dockerfile` each under the plugin dir,
   `CMD ["python","main.py"]`, honoring `RAT_PLUGIN_ADDR`. **Image-only — no proto change:** the
   container's own CMD runs python, so `LaunchSpec.image` = the image and the daemon needs no
   `args` field (frozen wire untouched). Build images tagged `rat/<plugin>:dev`.
2. Write their **plugin.yaml manifests** (they exist in `plugins/.../plugin.yaml` already) +
   a `data-dev-plane.yaml` (launch mode, `runtime: podman`, images + the I9 profile + the
   `RAT_DUCKLAKE_*` / `MINIO_*` env each needs).
3. `rat serve --plane data-dev-plane.yaml` runs the **real** ML lakehouse under the actual core:
   the strategy's `engine.execute` / `catalog.commit` hops route through the **real
   `core/gateway`** (C5 + audit + ADR-007 context-stamping), not the Python composition stand-in.
4. The **VS Code UI's control calls** (catalog browse, `strategy.Apply`) go to the real core
   gateway via the generated Connect **TS SDK**; the **BFF shrinks to the F9 data-leg only**
   (query *rows*, until a real Flight engine retires it).

**Exit criteria (B):** a target (e.g. `make data-dev-served`) runs the pipeline with the plugins
**launched and mediated by `rat serve`**; semantic search works end-to-end; the control hops
appear in the core's audit log.

### Phase C — the beginner compose stack (`deploy/data-dev-starter/`)

`docker compose up` → a working data-dev plane, **no Go/Python toolchain on the host**:

1. **Daemon image** — `core/Dockerfile` builds the `rat` static binary into `distroless`/`alpine`;
   entrypoint `rat serve --plane /etc/rat/plane.yaml`. The mounted plane uses **attach mode**.
2. **`deploy/data-dev-starter/compose.yaml`** brings up, on one network:
   - **rat-serve** (the daemon, attach mode) — the front door on `:7777`,
   - **base plugins** as containers (Phase-B images): `duckdb-ml` (engine), `ducklake` (catalog),
     `minio-s3` (storage), `incremental-embed` (strategy),
   - **infra**: **MinIO** (S3) + **Postgres** (DuckLake metadata),
   - the **BFF** for the F9 data-leg, so the VS Code extension connects to one URL.
   Compose starts everything; the daemon **attaches** by service name — **no DinD, no socket
   mount**.
3. **"Base plugins"** = one engine + one catalog + one storage + one strategy + MinIO + Postgres
   (the minimal end-to-end set). Swapping/adding is editing `compose.yaml` + `plane.yaml` — the
   "different plugin sets, same daemon" promise.

**Exit criteria (C):** a fresh machine with only podman/docker runs `compose up` in
`deploy/data-dev-starter/` and within ~a minute the VS Code extension (or `grpcurl`) drives the
pipeline + semantic search against the **compose-managed plane fronted by the real core
gateway**. The "getting started" is two lines.

## Decisions (resolved — were the open questions)

1. **Default runtime:** `local-process` for Phase A; `podman` for B/C.
2. **Python-plugin launch:** **containerize** (podman). **Image-only — NO proto change**; the
   image CMD runs `python main.py`. (The frozen `LaunchSpec` stands.)
3. **Auditor sink:** a **stdout JSON auditor** in the daemon (`StdoutAuditor` implements
   `gateway.Auditor`). The mandatory-audit invariant holds without an audit-log plugin; a real
   sink is a later plugin.
4. **Binary location:** **`core/cmd/rat/`** (in the sealed module). Adding a `cmd/` `main`
   package is **additive** — it does not touch the tested packages or the `rat/2.0` tag (tags
   point at commits; new files on a work branch don't move them).
5. **Phase placement:** **build now** — this is the **Phase 2 kickoff** (make the solo bundle
   runnable). It is *make-it-real infra*, not GTM, and is **not** behind Gate B (which gates
   Phase 2 → 3 on ≥10 users).
6. **Attach-mode supervision:** the daemon **health-checks** attached providers and reports
   Degraded (drops them from routing); it does **not** restart them — **compose owns restart**.
7. **Compose stack:** lives in **`deploy/data-dev-starter/`**; scoped here, built in **Phase C**
   after B produces the plugin images.

## Explicitly OUT of scope (v1)

- Multi-node / cross-host leader election (`lease` exists; v1 is single-node).
- Real tier-0 backends beyond minimal (state-backend / identity / NATS bus) — v1 uses
  local-process + the stdout auditor; bootstrap plugins are a follow-on.
- mTLS / per-plugin tokens on the **control** listener (PU-1 channel-auth is a frozen MUST for
  the *bytes* leg; control-listener hardening is a separate pass).
- **REST** alongside gRPC (v1 is gRPC only; grpc-gateway/REST is additive later).
- The **F9 data-leg** (a real Arrow Flight engine) — orthogonal; the BFF carries it until then.

## Consequences

- **Turns "components + scaffolding" into a running platform** — the single most valuable
  make-it-real step: the core stops being a library and becomes a thing you point clients at.
- **The BFF shrinks to its honest minimum** (the F9 data-leg); the UI's control path becomes the
  *real* core gateway via the TS SDK — retiring the "why not the core gateway?" gap.
- **Validates the deployment topology for real** (CLAUDE.md principle #8): plugins launched by
  the deployment-runtime, mediated by the enforcing gateway, kept alive by the reconciler.
- **Delivers the on-ramp** the vision promises: `./rat serve` (solo) and one-command `compose up`
  (beginner) — *same daemon*, launch vs attach. Two-mode design gets compose orchestration
  **without DinD**, and makes "different plugin sets, same binary" a config edit. First concrete
  piece of the ecosystem on-ramp (backlog EC-1).
- **Cost (all additive; no change to the sealed tested packages):** the daemon (`cmd/rat`) +
  attach assembly + stdout auditor (Phase A); 4 Dockerfiles + manifests + a plane file (B); a
  daemon image + a compose file (C).

## Alternatives considered

1. **Keep the BFF stand-in (status quo).** Cheapest, but never validates that the real core runs;
   "use the core gateway" stays impossible. Rejected as the end-state; fine as the F9-leg interim.
2. **A one-shot CLI** (`rat run --plane … --pipeline …`, brings up → runs once → tears down).
   Simpler, but not a server — clients can't connect to a living plane and the reconciler/health
   story isn't exercised. Rejected; a daemon is the point.
3. **Jump to the full Phase-2+ platform** (multi-node, real tier-0 backends, REST, mTLS). Too big;
   violates minimal-surface discipline. The A→B→C phasing keeps each step provable.
4. **Docker-in-docker / socket-mount for the daemon to launch plugin containers.** Rejected for
   the compose stack — security smell + complexity. The attach mode (compose orchestrates,
   daemon connects) is strictly simpler and is what production schedulers do anyway.

## Kickoff checklist for a fresh session (Phase A first)

1. Branch off `main` (or the active phase branch) — never commit to `main` (the hook blocks it).
2. `core/cmd/rat/main.go` — flag parsing (`--plane`), the assembly (steps 1–6 above), signals.
3. `core/cmd/rat/plane.go` — the YAML schema (above) + `Load(path) ([]PluginSpec, planeOpts)`.
4. `core/cmd/rat/auditor.go` — `StdoutAuditor` implementing `gateway.Auditor`.
5. `core/cmd/rat/manifests/*.plugin.yaml` + `go build ./core/testplugins/...` → `./bin/`.
6. Run `rat serve --plane …`; verify with `grpcurl`/a Go client: an authorized capability routes
   (C5 ok + audit line), an undeclared one is `PERMISSION_DENIED`, SIGTERM drains.
7. `make` target (`core-serve-smoke`?) wrapping the above containerized; keep `core-test` green.
8. Roadmap: `done.md` → `current.md`. Then Phase B.

## Related

- [ADR-014](014-spike-core-registry-and-invoke-gateway.md) — the registry + invoke gateway this serves.
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the launch contract B/C lean on.
- [ADR-005](005-capability-invocation-model.md) / [ADR-007](007-call-context-transport.md) — what the gateway enforces on every routed call.
- [`experiments/data-dev-plane/README.md`](../../../experiments/data-dev-plane/README.md) §10 **F9** — the BFF/data-leg this mostly retires.
- Code being wrapped: `core/supervisor/supervisor.go` (`BringUp`/`Plane`), `core/gateway/gateway.go` (`New`/`Auditor`), `core/reconciler/{reconciler,loop}.go`, `core/manifest/manifest.go` (`Load`), `core/deploymentruntime/{localprocess,podman}.go`. Attach-mode model: `core/composition/composition_realproviders_test.go`.
