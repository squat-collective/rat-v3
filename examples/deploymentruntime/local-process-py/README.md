# `rat-deploymentruntime-local-process-py` — the local-process deployment-runtime

One of the two ADR-003 references for the `deployment-runtime/v1` axis (the other is
[`k8s-dryrun-py`](../k8s-dryrun-py)). A `deployment-runtime` plugin is **where plugins
run** ([deployment_runtime.proto](../../../contracts/proto/rat/deploymentruntime/v1/deployment_runtime.proto))
— distinct from `runtime` (where data-plane *work* runs). It is **tier-0**:
bootstrap-critical, selected at boot, since the core needs one to launch any
out-of-process plugin.

This reference runs each plugin instance as a **child OS process** on the host — the
simplest, "`chmod +x ./rat`" runtime.

## Capabilities

| capability | method | what it does |
|---|---|---|
| `rat://deployment-runtime/v1/launch` | `Launch` | spawn a plugin instance from an image |
| `rat://deployment-runtime/v1/terminate` | `Terminate` | stop an instance |
| `rat://deployment-runtime/v1/healthcheck` | `Healthcheck` | liveness of an instance (the reconciler drives convergence with this) |

## The I9 trust gate (the load-bearing obligation)

`deployment_runtime.proto` makes the `IsolationProfile` **contract, not convention**:
"a runtime that ignores it fails conformance." This runtime enforces the gate — it
**refuses to launch** a plugin whose isolation is below the I9 minimum
(`run_as_non_root` + `drop_all_capabilities` + `no_new_privileges`) →
`FAILED_PRECONDITION`. Honest scope: a bare process can apply only a *subset* of the
kernel-level controls; it asserts the gate and records the committed profile (exposed
as an isolation-honored receipt in `Healthcheck.detail`). Full cap-drop / read-only-fs
enforcement needs a container runtime — that is exactly what the divergent
[`k8s-dryrun-py`](../k8s-dryrun-py) reference demonstrates (mapping the profile to a Pod
`securityContext`). The two references share the same golden vectors, proving the gate +
lifecycle are identical across genuinely different runtime technologies.

## How it's tested

[`deploymentruntime-v1.json`](../../../contracts/conformance/deploymentruntime-v1.json)
via `make conformance`: Launch (full I9 profile) → real child process → Healthcheck
HEALTHY (+ receipt) → Terminate → Healthcheck no-longer-HEALTHY; plus the gate vectors
(below-minimum → `FAILED_PRECONDITION`, empty image → `INVALID_ARGUMENT`).

## Files

- [`store.py`](store.py) — the local-process runtime + the shared I9 `check_spec` gate
- [`server.py`](server.py) — the `DeploymentRuntimeService` gRPC servicer
- [`harness_test.py`](harness_test.py) — the shared conformance harness
- [`main.py`](main.py) — standalone gRPC entrypoint
