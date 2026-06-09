# ADR-040: Published ports — a launch lifecycle for UI / HTTP plugins

## Status: Accepted (2026-06-09)

## Context

The [clean-room rebuild](../../../CLEAN-ROOM.md) built a web UI as a `ui` plugin — a
backend-for-frontend that serves HTTP and turns browser actions into gateway capability calls
([Guide 05](../../guides/05-a-web-ui.md)). It exposed [GAPS.md](../../guides/GAPS.md) #9: **RAT had
no lifecycle for a plugin that needs a browser-reachable port.**

Every other plugin is a gRPC server: `rat plugin pack` launches it under I9 and probes its declared
capabilities; `rat serve` launches it and the deployment-runtime publishes its gRPC port to an
ephemeral *loopback* host port for the core to dial. A UI plugin breaks both assumptions — it serves
**HTTP**, not gRPC, and its port must be reachable by a **browser**, not just the core. So the BFF
had to be run as a **sidecar container the operator published themselves**, registered only for its
C5 identity — outside the pack/launch/reconcile lifecycle every other plugin enjoys.

## Decision

**Let a plugin declare browser-reachable ports in its manifest, and have the deployment-runtime
publish them to the host on launch.**

1. **Additive manifest field `ports`** ([plugin.v1.json](../../../contracts/schema/plugin.v1.json)):
   ```yaml
   ports:
     - name: http
       container_port: 8088
   ```
   Optional; most plugins (pure capability providers) declare none. Additive + backward-compatible
   within `rat/1` (no proto change — this is manifest-only).

2. **The deployment-runtime publishes them.** On launch, `rat serve` passes the declared ports to
   the runtime via a reserved launch-env directive `RAT_PUBLISH_PORTS` (the existing
   `LaunchSpec.env` extension point — **no change to the frozen `deployment_runtime` proto**). The
   Podman runtime maps each to an ephemeral host port on **all interfaces** (`-p 0.0.0.0::<port>`, so
   a browser can reach it — unlike the gRPC port's loopback-only publish) and **logs the host URL**
   so the operator can find it.

3. **Health stays gRPC.** A launched plugin is "healthy" when its `RAT_PLUGIN_ADDR` (gRPC) port
   accepts a TCP connection. A UI plugin opens that port too (an empty gRPC server via the SDK,
   alongside its HTTP server) — so health is uniform, and the published HTTP port is purely for
   out-of-band browser traffic. Capability traffic still flows through the gateway (C5); the
   published port carries only the plugin's own HTTP surface.

With this, the BFF is **launched + reconciled like any other plugin** (`launch:` in the plane), not a
hand-run sidecar.

## Consequences

**Good:**
- A UI plugin joins the normal lifecycle: declared in the plane, launched under I9, reconciled,
  self-healing. The operator gets the URL from the serve log.
- No frozen-proto change, no SDK regeneration — the manifest is the source of truth, the env
  directive is the transport.
- Generalizes beyond UIs: any plugin with an out-of-band HTTP surface (a metrics endpoint, a
  webhook receiver) can publish a port.

**Costs / residual:**
- `RAT_PUBLISH_PORTS` is a *convention* over `LaunchSpec.env`, not a structural field. The mapped
  host port is surfaced via a **log line**, not the `LaunchResponse` (which has only `endpoint`).
  A GA refinement adds `published_ports` to `LaunchResponse` (a proto change + SDK regen) so the
  mapping is structured — e.g. `rat status` could show the UI URL. Deferred to avoid touching the
  sealed wire here.
- The health-port-plus-HTTP-port shape means a UI plugin opens two listeners. Acceptable; the SDK
  makes the gRPC health server one line.
- `rat plugin pack` still probes gRPC capabilities — a UI driver has none (`provides: []`), so pack
  verifies "launches healthy under I9" only. An HTTP-aware pack health-check is a follow-on.

## Related
- [GAPS.md](../../guides/GAPS.md) #9 — the gap this resolves.
- [ADR-039](039-driver-plugins-and-the-authoring-gate.md) — the driver shape a UI plugin uses.
- [ADR-016](016-plugin-provisioning-via-deployment-runtime.md) — the launch/I9 model this extends.
- [plugins/ui/web-bff-py](../../../plugins/ui/web-bff-py/) — the reference UI plugin, now launched.
