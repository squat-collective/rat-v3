# `deployment-runtime/v1` — plugin contract (author guide)

> **Status (2026-06-10) — the core is built and sealed.** What this guide describes **runs
> today**: capability routing, channel-authenticated plugin identity (C2, ADR-042), C5
> capability authz, deadline-bounding, and mandatory audit emission are enforced by the
> sealed core (`rat/2.0`, hardened through `rat/6.13`). `make conformance` checks the
> references against the golden vectors; `make composition` runs the cross-axis suite
> against real providers. The wire stays frozen (`rat/1`); post-freeze changes land as
> additive, capability-gated amendments (e.g. ADR-035 `delete` + ADR-049
> `create-if-absent` on `state/v1`).

> Canonical guide for implementing a `kind: deployment-runtime` plugin. Pairs with the wire
> contract [`deployment_runtime.proto`](deployment_runtime.proto) and the golden vectors
> [`deploymentruntime-v1.json`](../../../../conformance/deploymentruntime-v1.json). Status: **v1 (frozen — rat/1.3; two divergent references: local-process + k8s-dryrun)**.

## What a `deployment-runtime` plugin is

A `kind: deployment-runtime` plugin (local-process, docker, podman, k8s, nomad, lambda, fargate)
owns **WHERE PLUGINS RUN** — process lifecycle. It is the only party that spawns or terminates
plugin instances; the core never directly launches a process.

> ⚠️ **TIER-0 — bootstrap-critical.** A deployment-runtime is not a hot-swappable plugin like
> a catalog or engine. It is selected at boot and must be running before the core can launch any
> other out-of-process plugin. It carries the same "still a plugin (different impls possible)"
> property as all tier-0 axes — it is just load-bearing for the core to start, so it gets extra
> rigor and is not marketed as "swap at runtime." See
> [`.claude/rules/plugin-architecture.md`](../../../../../.claude/rules/plugin-architecture.md)
> "tier 0".

It is also the **reactor the reconciler's desired-plane state converges through**: the reconciler
declares that a plugin instance should exist; the deployment-runtime makes it so. `Healthcheck`
is the observable the reconciler polls to drive that convergence.

The axis is **control-plane only** — there is no Arrow data plane.

## Capabilities

| capability URI | method | what it does |
|---|---|---|
| `rat://deployment-runtime/v1/launch` | `Launch` | start a plugin instance from an OCI image ref |
| `rat://deployment-runtime/v1/terminate` | `Terminate` | stop a running plugin instance by `instance_id` |
| `rat://deployment-runtime/v1/healthcheck` | `Healthcheck` | liveness/readiness of an instance (reconciler polling) |

## The RPCs

- **`Launch(plugin_id, spec)` → `{instance_id, endpoint}`** — validate the `LaunchSpec` (image
  required → `INVALID_ARGUMENT` if empty; `IsolationProfile` MUST meet the I9 minimum → see
  ISOLATION OBLIGATION below), spawn the instance, return an opaque `instance_id` (used by all
  subsequent calls) and the `endpoint` address the core dials to reach the launched plugin. Never
  pass secrets in `spec.env` — those come via secret-backend.
- **`Terminate(instance_id)` → `{terminated: bool}`** — stop the instance. `terminated=false`
  means the `instance_id` was not found (not an error — idempotent by design).
- **`Healthcheck(instance_id)` → `{status: HealthStatus, detail: string}`** — return
  `HEALTH_STATUS_HEALTHY`, `HEALTH_STATUS_UNHEALTHY`, or `HEALTH_STATUS_UNKNOWN` (unknown
  instance). The `detail` field carries a JSON isolation-honored receipt (see ISOLATION
  OBLIGATION); the reconciler uses it to verify the property is observable.

`RequestContext` rides in the `rat-callmeta-bin` metadata header
([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)), **not** a
request field — `reserved 1` on every message is a reminder. The `reserved` slot must not be
repurposed.

## Conformance obligations — ISOLATION OBLIGATION (reviews/04 I9 / reviews/08 D1)

The `IsolationProfile` in `LaunchSpec` is **contract, not convention**. A deployment-runtime
MUST enforce the minimum profile; a runtime that ignores it fails conformance. This is the trust
boundary the whole "install many 3rd-party plugins" bet leans on
([reviews/01](../../../../../reviews/01-adversarial-architect.md)).

**I9 minimum (required for launch):**
- `run_as_non_root = true`
- `drop_all_capabilities = true`
- `no_new_privileges = true`

A `LaunchRequest` whose `IsolationProfile` does not satisfy all three **MUST** be rejected with
`FAILED_PRECONDITION`. Both reference implementations share the identical `check_spec` gate.

**Honesty note (reviews/08 D1 — `[PROCESS]`/`[ADDITIVE]` · HIGH):** The v1 references
SELF-ATTEST the full profile via an isolation-honored receipt in `Healthcheck.detail`
(a conformance stand-in, like storage's scope receipt). `local-process-py` asserts
`read_only_root_fs:true` while `Popen`-ing a bare subprocess that enforces nothing at the kernel
level; the golden vectors check only the three I9 minimum bools, not the full five-field profile.
A runtime can therefore pass conformance today while actually isolating nothing beyond the gate
check. **A real enforcing runtime (podman / container runtime), a full-profile vector, and a
structured `IsolationAttestation` message (rather than a free-form `detail` string) are
Phase-1/GA obligations** — these items are additive (not breaking) and do not wait for a v2.

**Healthcheck isolation receipt shape (v1):** the JSON in `detail` MUST carry at minimum:
```json
{
  "kind": "<runtime-kind>",
  "isolation_honored": {
    "run_as_non_root": true,
    "drop_all_capabilities": true,
    "no_new_privileges": true,
    "read_only_root_fs": <bool>,
    "block_metadata_egress": <bool>
  }
}
```

Pass [`deploymentruntime-v1.json`](../../../../conformance/deploymentruntime-v1.json): the
Launch → Healthcheck → Terminate lifecycle with the full `IsolationProfile` vector, PLUS the two
error vectors:
- `below_i9_minimum` (one of the three required bools false) → `FAILED_PRECONDITION`
- `empty_image` → `INVALID_ARGUMENT`

## Cross-cutting (every axis)

- **Error reporting** follows the canonical [error model](../../common/v1/ERROR_MODEL.md)
  (pinned at `rat/1`): gRPC status codes for malformed/unauthorized/missing/precondition
  failures; in-response `bool` fields for normal domain outcomes (`terminated=false` on an
  unknown `instance_id`).

- `RequestContext` rides in the `rat-callmeta-bin` metadata header ([ADR-007](../../../../../docs/architecture/adrs/007-call-context-transport.md)),
  not a field. Invocation is core-mediated ([ADR-005](../../../../../docs/architecture/adrs/005-capability-invocation-model.md));
  the deployment-runtime implements a plain gRPC `DeploymentRuntimeService` server.

## Writing a plugin

1. Implement `DeploymentRuntimeService` (`Launch`/`Terminate`/`Healthcheck`) for your target
   substrate (local process, docker, podman, k8s, nomad, lambda, …).
2. In `Launch`: validate `spec.image` (empty → `INVALID_ARGUMENT`); check the I9 minimum on
   `spec.isolation` (any required bool false → `FAILED_PRECONDITION`); record the committed
   isolation profile so `Healthcheck` can return the receipt.
3. In `Healthcheck`: return `HEALTH_STATUS_UNKNOWN` for an unknown `instance_id` (not an error);
   include the isolation-honored receipt in `detail` as JSON.
4. In `Terminate`: return `terminated=false` for an unknown `instance_id` (idempotent, not an
   error); stop the instance and return `terminated=true`.
5. Never accept `reserved 1` as a request field — it is reserved for the call-context transport.
6. Pass [`deploymentruntime-v1.json`](../../../../conformance/deploymentruntime-v1.json) via
   `make conformance`.

## Reference implementations

| ref | round | demonstrates |
|---|---|---|
| [`plugins/deploymentruntime/local-process-py`](../../../../../plugins/deploymentruntime/local-process-py) | 1 (wire) | I9 gate + lifecycle over bare OS subprocesses; isolation-honored receipt self-attested |
| [`plugins/deploymentruntime/k8s-dryrun-py`](../../../../../plugins/deploymentruntime/k8s-dryrun-py) | 1 (wire) | same I9 gate; `IsolationProfile` mapped 1:1 to a k8s Pod `securityContext` — the real kernel-level enforcement surface |

These two references are **technologically divergent** per [ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md):
local-process (fork/subprocess) vs managed/declarative (k8s Pod manifest), running the identical
golden vectors — proving the contract holds across genuinely different runtime technologies.

## Related

[`deployment_runtime.proto`](deployment_runtime.proto) · [`deploymentruntime-v1.json`](../../../../conformance/deploymentruntime-v1.json) ·
[`common/v1/ERROR_MODEL.md`](../../common/v1/ERROR_MODEL.md) ·
[`.claude/rules/plugin-architecture.md`](../../../../../.claude/rules/plugin-architecture.md) (tier-0) ·
[reviews/08](../../../../../reviews/08-post-freeze-board-review.md) D1 ·
[ADR-003](../../../../../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (two references before freeze)
