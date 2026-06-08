# ADR-016: Plugin provisioning via the deployment-runtime axis — the core launches, it doesn't dial (D1)

## Status: Accepted (2026-06-01)

## Context

The spike core ([ADR-014](014-spike-core-registry-and-invoke-gateway.md)) provisions providers by
**dialing pre-running gRPC servers** handed to the gateway as connections — a deliberate shortcut
(ADR-014 §3: "plugins are started as local gRPC servers … process launch deferred"). **D1** (the
board's exit criterion; [ADR-015](015-phase-1-commitment-gate-cleared.md) definition-of-done)
requires a real *enforcing* deployment-runtime: plugins run as **isolated processes the core brings
up and supervises**, with the **I9 isolation profile enforced** — not pre-launched by a harness.
This is the trust boundary the "install many 3rd-party plugins" bet leans on.

The frozen `deployment-runtime/v1` axis (`rat/1.3`) already specifies exactly this:
`Launch(LaunchSpec{image, requests, limits, IsolationProfile, env}) → {instance_id, endpoint}`,
`Healthcheck`, `Terminate`, with I9 as **wire-level contract** (a runtime MUST honor the profile +
refuse below the minimum). It is a **TIER-0 plugin** (bootstrap-critical, selected at boot —
[plugin-architecture.md](../../../.claude/rules/plugin-architecture.md)).

## Decision

**The core provisions plugins by LAUNCHING them through the deployment-runtime axis, then dialing
the returned endpoint — it never dials a pre-running provider.**

### 1. The provisioning sequence (replaces the spike's dial-pre-running)

For each plugin in the registry's manifests, the core:
1. builds a `LaunchSpec` from the manifest (`image`/command + `resources` → `requests`/`limits` + an `IsolationProfile`),
2. calls `deployment-runtime.Launch(plugin_id, spec)` → `{instance_id, endpoint}`,
3. `Healthcheck(instance_id)` until `HEALTHY` (bounded — a launch that never goes healthy fails the bring-up),
4. dials `endpoint`, registers `(kind,name,version) → conn` (**the registry/gateway interfaces from ADR-014 are unchanged** — only the *source* of the conn changes),
5. on shutdown, `Terminate(instance_id)`.

### 2. Tier-0 bootstrap (the chicken-and-egg)

The deployment-runtime is itself a plugin, so it can't launch itself. The core brings up **one**
deployment-runtime at boot via a minimal built-in seat (it runs in-core / is exec'd specially —
"selected at boot, not hot-swapped," the documented tier-0 treatment). **Every other plugin** is
then launched *through* it via the `launch` capability. The six-thing core gains **no** general
process-management responsibility — that lives in the deployment-runtime plugin; the core holds
only the tiny tier-0 bootstrap seat. (No 7th core thing — temptation ledger stays at 0.)

### 3. The D1 increment: a real `local-process` deployment-runtime

Implement a Go `local-process` deployment-runtime (`kind: deployment-runtime`) that:
- interprets `LaunchSpec.image` as a plugin **binary to exec** (the local / `chmod +x ./rat`
  runtime; `image` is an OCI ref only for *container* runtimes),
- launches each plugin as a distinct **child OS process** (distinct PID from the core — real
  process isolation, the seed of the sandboxing story),
- applies + **ENFORCES** the process-enforceable subset of the I9 profile and **REFUSES to Launch
  below the minimum** (`run_as_non_root` + `drop_all_capabilities` + `no_new_privileges`) →
  `FAILED_PRECONDITION` (matching the frozen contract + the `local-process-py` reference),
- supports `Healthcheck` (PID liveness) + `Terminate` (kill).

The Go composition (catalog/format) is re-run with providers **launched by this runtime** (the
spike's in-test fakes promoted to small standalone plugin binaries the runtime execs) — proving
the **launch → healthcheck → dial → register → route → terminate** lifecycle end-to-end, with C5
still enforced against the (now real-process) providers.

### 4. Scope / what's deferred within D1

`local-process` enforces the **process-level** subset of I9 (non-root, cap-drop, no-new-privs, PID
isolation). The **full** profile (`read_only_root_fs`, `block_metadata_egress`, `seccomp`) needs
container isolation — a **podman deployment-runtime** is the second reference that enforces it (the
board's literal "podman, not dry-run"). **D1 is COMPLETE when the podman runtime passes a
full-profile isolation vector**; `local-process` is the stepping stone that proves the mechanics +
the I9-refusal gate. (podman is available per the project's container-only rule; it needs plugin
container images — the heavier follow-on.)

## Consequences

**Positive.**
- Providers are real isolated processes the core brings up + supervises — D1's trust boundary
  becomes code, not a test-harness convenience.
- Uses the **frozen** deployment-runtime contract unchanged (Launch/Healthcheck/Terminate +
  IsolationProfile) — another exercise of the wire by a real enforcer, extending the spike's
  de-risking into D1.
- Unblocks **D3** (storage-cred scoping needs a real process boundary) + **C5-against-real-providers**.

**Negative — accepted.**
1. **Two runtimes to reach full I9** — local-process (process subset) now, podman (full profile)
   next. Accepted: process isolation proves the lifecycle + refusal gate cheaply; the container
   profile is additive.
2. **Standalone plugin binaries needed** — the spike's in-test fakes become small `cmd/` binaries
   the runtime execs. Minor scaffolding; also makes them runnable references.
3. **The core carries a tier-0 bootstrap seat** — a minimal in-core path to bring up the first
   deployment-runtime. Documented tier-0 treatment; not a new core responsibility (the launching
   itself is the plugin's job).

**Neutral.** The registry/gateway interfaces (ADR-014) are unchanged; only conn provenance changes
(launched, not handed in).

## Alternatives considered

1. **Keep dialing pre-running providers.** Rejected: that's the spike shortcut D1 exists to remove
   — it proves nothing about isolation.
2. **A general process launcher in the core (not via the deployment-runtime axis).** Rejected:
   that's a 7th core thing; launching is exactly what the deployment-runtime axis is *for*. The
   core only bootstraps the tier-0 seat.
3. **podman runtime first (skip local-process).** Rejected for the increment: heavier (images,
   build) for the same lifecycle/refusal proof; local-process gets there faster, podman follows for
   the full profile.

## Migration

New `core/deploymentruntime` (the local-process runtime) + a core "supervisor" that turns manifests
→ Launch → dial → register; the spike's in-test catalog/format fakes promoted to standalone
binaries. Composition-on-Go re-run through launched providers. CI: `make core-test` covers it; a
podman full-profile vector lands with the podman runtime. Frozen contracts untouched (`make
breaking` stays green) — this is all `core/` + examples.

## Related

- [ADR-015](015-phase-1-commitment-gate-cleared.md) — the cleared gate / full-build commitment whose D1 this starts · [ADR-014](014-spike-core-registry-and-invoke-gateway.md) — the spike core (dial-pre-running) this supersedes for provisioning.
- [ADR-001](001-everything-is-a-plugin.md) — tier-0 + the six things · [reviews/10](../../../reviews/10-phase-1-spike-exit.md) §"what the spike did NOT prove" (D1).
- [`deployment_runtime.proto`](../../../contracts/proto/rat/deploymentruntime/v1/deployment_runtime.proto) + its [CONTRACT.md](../../../contracts/proto/rat/deploymentruntime/v1/CONTRACT.md) — the frozen axis · `plugins/deploymentruntime/local-process-py` — the existing reference this Go runtime mirrors.
