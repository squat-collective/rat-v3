# `rat-deploymentruntime-k8s-dryrun-py` — the k8s (dry-run) deployment-runtime

The divergent second ADR-003 reference for `deployment-runtime/v1` (paired with
[`local-process-py`](../local-process-py)). Instead of forking a local process, it models
a **managed / declarative** runtime: it translates the `LaunchSpec` — crucially the I9
`IsolationProfile` — into a Kubernetes **Pod manifest** with a real `securityContext`,
and admits it (dry-run: no cluster required). This is where the proto's "honor the
profile" obligation maps 1:1 onto fields a container runtime actually enforces.

## Why it's the right divergent pair

| | local-process | **k8s-dryrun** |
|---|---|---|
| launch mechanism | fork a child OS process | admit a Pod manifest (managed/declarative) |
| isolation enforcement | process-level subset + the gate | full profile → `securityContext` |
| `Healthcheck` | real PID liveness | admitted-Pod status (dry-run) |

The **I9 trust gate is identical** in both (`check_spec`: refuse below-minimum isolation
→ `FAILED_PRECONDITION`), and both expose the same isolation-honored receipt, so both
pass the *same* [`deploymentruntime-v1.json`](../../../contracts/conformance/deploymentruntime-v1.json)
golden vectors — proving the contract holds across genuinely different runtime
technologies (local fork vs container), the ADR-003 cross-implementation point.

## securityContext mapping

| `IsolationProfile` | Pod `securityContext` |
|---|---|
| `run_as_non_root` | `runAsNonRoot` |
| `no_new_privileges` | `allowPrivilegeEscalation: !value` |
| `read_only_root_fs` | `readOnlyRootFilesystem` |
| `drop_all_capabilities` | `capabilities.drop: ["ALL"]` |
| `seccomp_profile` | `seccompProfile.type` (default `RuntimeDefault`) |

## How it's tested

`make conformance` — the shared lifecycle + I9-gate vectors. `Launch` admits a manifest
and returns a cluster-DNS endpoint; `Healthcheck` reports HEALTHY + the
securityContext-derived receipt; `Terminate` removes the Pod; a below-I9 launch is
refused.

## Files

- [`store.py`](store.py) — the manifest-generating runtime + the shared I9 gate + the securityContext mapping
- [`server.py`](server.py) — the `DeploymentRuntimeService` gRPC servicer
- [`harness_test.py`](harness_test.py) — the shared conformance harness
- [`main.py`](main.py) — standalone gRPC entrypoint
