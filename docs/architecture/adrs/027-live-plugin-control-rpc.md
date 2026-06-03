# ADR-027: Live plugin control — the daemon's `ControlService` (Register/Deregister/List)

## Status: Proposed (2026-06-03)

## Context

`rat add` / `rat remove` are **declarative** ([ADR-023](023-rat-as-a-per-project-daemon.md)):
they edit `rat.toml`, and the change takes effect only on the next `rat up` — i.e. a full
daemon **restart** (`rat down` + `rat up`). For a platform whose premise is *reconcile to
desired state* (K8s-shaped, [ADR-001](001-everything-is-a-plugin.md)), making the operator
bounce the whole plane to add one plugin is backwards. The reconciler already converges a
desired set to a running set, launching/terminating/re-wiring as needed — it just can't be
**told about a new desired entry at runtime**.

Two of the three mechanisms a live change needs already exist:

- **Gateway re-bind** — `gateway.SetProvider`/`RemoveProvider` (the concurrency-safe mutable
  provider map, [ADR-022](022-plugins-are-launched-not-composed.md) Q5).
- **Reconciler re-wire** — the `Rewire` Bind/Unbind hook fired on health transitions
  (ADR-022 slice 2a/2b): a crashed plugin already re-binds the gateway at its new endpoint.

What's missing is the **input** half and the **surface**:

- The **registry** (`registry.New(manifests)`) is built once and immutable — no runtime
  `Register`/`Deregister`, so the core can't authorize (C5) a plugin it didn't boot with.
- The **reconciler** desired-set (`reconciler.New(rt, desired, cfg)`) is fixed at construction
  — no way to add/drop a desired entry while the loop runs.
- The daemon serves exactly one gRPC service (`CapabilityInvokeService`) and has **no control
  surface** a client can call to mutate the plane.

The "runtime self-registration" idea was parked in [`ideas/inbox.md`](../../../ideas/inbox.md)
precisely pending this ADR + the mutable-core change. This is that ADR.

**Is this a 7th core thing?** No. The six are Registry, Identity gateway, State gateway, Event
bus, Reconciler, API gateway ([plugin-architecture.md](../../../.claude/rules/plugin-architecture.md)).
Live control **mutates the Registry and exposes an admin RPC on the API gateway** — it extends
two existing responsibilities, it does not add a new one. The reconciler's job ("drive the
desired set to convergence") is unchanged; we only make its *input* mutable. No temptation
logged.

## Decision

Add a **dedicated control RPC** to the daemon — Option 1 of the design fork (the alternatives,
incl. control-as-capability and SIGHUP-reload, are recorded below). Five parts.

### 1. A new `ControlService` gRPC (additive to the frozen wire)

`contracts/proto/rat/core/v1/control.proto`, served by the daemon on its **control listener**
alongside `CapabilityInvokeService`:

```proto
service ControlService {
  // Add a plugin to the RUNNING plane: validate its manifest, register it (C5
  // authorization now knows it), add it to the reconciler's desired set (launch +
  // wire), and wait (bounded) for it to become Healthy. Idempotent: re-registering
  // a present plugin refreshes its spec.
  rpc RegisterPlugin(RegisterPluginRequest) returns (RegisterPluginResponse);

  // Remove a plugin from the running plane: drop it from the desired set
  // (terminate the instance), unbind it from the gateway, deregister it.
  rpc DeregisterPlugin(DeregisterPluginRequest) returns (DeregisterPluginResponse);

  // The live plane as the daemon sees it (distinct from `rat status`, which reads
  // rat.toml): name, kind, state (Healthy/…), endpoint, provides.
  rpc ListPlugins(ListPluginsRequest) returns (ListPluginsResponse);
}
```

`RegisterPluginRequest` carries the **manifest** (kind + identity + provides/requires — what
the registry authorizes from) and the **`LaunchSpec`** (image / isolation / env — what the
reconciler launches), the same two artifacts a plane entry already reduces to. The control
surface lives in `core/v1` because it is the **API gateway's admin face**, not a plugin axis.

### 2. Authorization: control is an operator action, reachability is the gate (v1)

C5 authorizes *plugin* calls by manifest `requires`. Control is an **operator** action — `rat
add` is a CLI, not a plugin with a manifest — so it does **not** flow through C5. In v1 the
**control listener's reachability IS the authorization**: a per-project **unix socket**
(`.rat/daemon.sock`, ADR-023) means filesystem permissions gate who can register, exactly as
they gate `rat down`. This is the same "tier-0 trust boundary" honesty the project already
applies to bootstrap-critical surfaces. Operator **identity + mTLS** for a *TCP* control
endpoint (remote control) is deferred — see Q01.

### 3. The registry becomes mutable

`Register(*manifest.Manifest) error` / `Deregister(name string)` under a `sync.RWMutex`,
preserving the existing invariants **at runtime**: a duplicate plugin name or a duplicate
capability provider is **rejected** (returned as an error), never silently overridden — the
spike still has no provider-selection policy, so an ambiguous live register must fail loudly.
`Authorize`/`ProviderOf`/`Capabilities` take the read lock.

### 4. The reconciler desired-set becomes mutable

`AddDesired(Desired)` / `RemoveDesired(name)` under the reconciler's existing lock. `AddDesired`
inserts the entry; the next reconcile tick launches it and `Rewire.Bind`s the gateway on
Healthy (the *existing* path). `RemoveDesired` terminates the instance, `Rewire.Unbind`s, and
drops the status. **Convergence semantics are unchanged** — we made the desired input mutable,
not the loop's behavior.

### 5. `rat add` / `rat remove` become daemon-aware (rat.toml stays the source of truth)

After editing `rat.toml` (unchanged), `add`/`remove` check for a **running daemon** for this
project (`runningPid`, ADR-023). If one is up, they dial the control socket and call
`RegisterPlugin`/`DeregisterPlugin` to **materialize the change live**; if not, they stay
declarative and `rat up` applies it. So:

- `rat.toml` remains the single source of truth; the RPC only **applies the diff** to the
  running daemon (the file and the live plane never diverge — the file is written first).
- The live path is **idempotent**: the materialize step is "make the running plane match this
  one new/removed entry," safe to repeat.
- A `--no-live` escape hatch keeps a pure-declarative edit when wanted.

## Consequences

**Positive.**
- Live `rat add` / `rat remove` — a plugin joins/leaves a running plane with **no restart**,
  the reconcile-to-desired-state premise finally exercised at runtime.
- `rat.toml` stays the source of truth; the RPC is a *materializer*, not a second store — no
  drift between the file and the live plane.
- Reuses the whole existing convergence + re-wire machinery; the new code is the *input* and
  the *surface*, not new reconcile logic.
- Control is **cleanly separated** from capability-invocation — different service, different
  authorization model — which keeps the C5 plugin-bus uncontaminated by operator actions.
- `ListPlugins` gives the *live* plane truth (vs `rat status` reading the file), useful to
  every surface (CLI/VS Code/webapp) and to debugging.

**Negative — accepted.**
- A **new service on the frozen wire** (`rat/2.0`). It is *additive* (`make breaking` clean)
  and core↔operator (not a plugin contract), but it does grow the surface during the freeze;
  it is bound by the [ADR-017](017-pre-unfreeze-contract-amendment-gate.md) pre-unfreeze gate
  like any other additive change, and needs **SDK regen** ([ADR-018](018-connectionless-codegen-local-plugins.md)).
- The registry and reconciler gain a **concurrency surface** (locks) they didn't have as
  boot-once structures — needs `-race` tests for register-during-reconcile.
- v1 control authz is **reachability-only** — fine for the unix-socket default, insufficient
  for a TCP control endpoint (Q01). We ship the socket path and gate TCP behind that ADR.
- A live `RegisterPlugin` can **fail at launch** (image missing, never Healthy) → the request
  returns an error, but the desired entry may linger; Register must roll back (remove the
  desired entry + deregister) on a launch-timeout so a failed live-add leaves no partial state.

**Neutral.**
- The daemon now has an admin API distinct from its data API — a normal apiserver shape, new
  for rat.
- `ListPlugins` overlaps `rat status` in spirit but differs in truth-source (live vs file);
  both stay.

## Open questions

- **Q01 — Operator identity + mTLS for TCP control.** The unix-socket default is gated by
  filesystem perms; a *networked* control endpoint needs real operator authn (mTLS / a signed
  operator token). Deferred to a dedicated security ADR; until then TCP control is
  trusted-network-only and documented as such.
- **Q02 — Atomic multi-plugin apply.** v1 materializes **one** entry per call. Applying a whole
  `rat.toml` diff transactionally (all-or-nothing, e.g. after editing several `[[plugin]]`
  blocks) is a future `ApplyPlane` RPC; v1's per-plugin calls are the primitive it would batch.
- **Q03 — Deregister cascade.** Removing a *provider* breaks dependents (the resolver already
  warns on `rat remove`). Should `DeregisterPlugin` refuse / force / cascade when a still-running
  plugin `requires` the victim's capability? v1 warns (mirrors `rat remove`); enforcement is a
  follow-on.
- **Q04 — Hot reconfigure.** Re-registering with a changed env/image is an *update*, not an
  add. v1 treats re-register as refresh-the-spec; a first-class `UpdatePlugin` with a
  rolling-restart is deferred.

## Alternatives considered

- **Control-as-capability** (register/deregister as `rat://control/v1/*` invoked through the
  existing `Invoke` RPC, daemon as in-process provider). *Rejected:* it bends the C5
  plugin-capability model to carry an **operator** action (the caller isn't a plugin with a
  `requires`), and forces **in-process-provider** support into the gateway — a muddier core
  change than a clean separate service. Smaller wire surface, worse conceptual fit.
- **SIGHUP reload** (edit `rat.toml`, signal the daemon to re-read + diff). *Rejected:* not an
  RPC — no structured request/response, no return status, no remote control, and coarse
  (re-reads the whole file). It is the most *conservative* option and stays in reserve if the
  freeze discipline ever outweighs the RPC; for now the explicit control API is wanted.
- **Restart-only (status quo).** *Rejected:* it is the problem this ADR exists to remove.

## Migration

Additive; **Phase 9**. Step 1 is this ADR (committed alone). Then: **(2)** `control.proto` +
`make gen-sdks` (Go SDK regen, `gen-check` green); **(3)** registry `Register`/`Deregister`
(+`-race` tests); **(4)** reconciler `AddDesired`/`RemoveDesired` (+`-race` tests); **(5)** the
`ControlService` impl in the daemon, served on the control listener, wired to registry +
reconciler + the existing `Rewire`; **(6)** `rat add`/`remove` daemon-aware (live materialize,
`--no-live` escape); **(7)** prove live — a running daemon gains/loses a plugin with no
restart, audited, `rat.toml` unchanged as the source of truth. `make breaking` clean throughout.

## Related

- [ADR-019](019-rat-serve-daemon.md) — `rat serve`, the runnable core daemon this extends.
- [ADR-022](022-plugins-are-launched-not-composed.md) — launched-not-composed; **Q5** the
  `gateway.SetProvider` re-bind this builds on.
- [ADR-023](023-rat-as-a-per-project-daemon.md) — the per-project daemon, the unix-socket
  control surface (the v1 authz boundary), and the declarative `add`/`remove` this makes live.
- [ADR-017](017-pre-unfreeze-contract-amendment-gate.md) — the additive-change gate this new
  service is bound by.
- [ADR-018](018-connectionless-codegen-local-plugins.md) — the codegen toolchain the new proto
  regenerates through.
- `core/registry`, `core/reconciler`, `core/gateway` — the three structures made mutable/served.
- [`ideas/inbox.md`](../../../ideas/inbox.md) — the parked "runtime self-registration" idea this
  resolves.
