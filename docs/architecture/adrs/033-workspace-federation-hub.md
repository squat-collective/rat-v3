# ADR-033: Workspace federation — the `rat hub` (a gateway-of-gateways)

## Status: Accepted (2026-06-04) — first cut built (Phase 10)

## Context

rat is **per-project**: every `rat.toml` is its own daemon on its own control listener
(default a per-project unix socket — ADR-023). So **many workspaces running at once is the
normal state**, not an edge case — `rat ls` exists precisely because one machine routinely runs
several rats, and across machines there are many more. The machine-global instance registry
(`~/.local/state/rat/instances.json`: `{name, dir, pid, addr}`) already enumerates them.

That creates a remote-access problem the single-instance design doesn't have:

- **N exposure points.** Exposing each daemon individually = N public ports, N certs, N firewall
  holes. The attack surface scales with the number of workspaces — exactly backwards.
- **No addressing.** A remote client has no way to say *"the `kitchen` workspace"* vs *"the
  `analytics` one"* — there's no name→endpoint resolution above the per-project socket.
- **No auth-once.** Authenticating separately to each workspace doesn't compose.

This is the same problem **Teleport** (one proxy → many nodes/clusters), **Tailscale** (one
coordination plane → many nodes), and **Kubernetes fleet/context** management all solve: a single
front door that **discovers, addresses, and authenticates once**, fanning out to many private
backends. rat needs the same shape — and it must respect the **shared-responsibility security
model** this conversation established (the perimeter is environmental; rat owns identity-required +
C5 authz + audit; *owed its own ADR*): the hub is the **single guarded door**, and workspaces stay
**private** (no individual public exposure).

## Decision

Add **`rat hub`** — a **gateway-of-gateways**: a process that federates many workspace daemons
behind one endpoint, routing capability calls to the right workspace.

### 1. Position — above the planes, not in the core

The hub is NOT a 7th core thing. The six-thing core is **per-plane**; the hub sits **above** planes,
on the **client/edge side** of the orchestrator↔client split (ADR-019), like `ratctl`. It is the
**API-gateway pattern applied across daemons** — it reuses the existing `CapabilityInvokeService`,
adds no per-axis knowledge, and each workspace's own core still does the real work.

### 2. Transparent capability forwarding (the mechanism)

The hub implements `CapabilityInvokeService` (invoke.proto) and is a **generic byte-relay**, exactly
as the per-plane gateway is (ADR-005):

1. read a **`rat-workspace`** selector from the request's transport metadata,
2. resolve it to a workspace endpoint via the **instance registry** (auto-discovered) or hub config
   (remote workspaces),
3. forward the opaque `InvokeRequest{capability, payload}` to that workspace's daemon, **preserving
   the `rat-callmeta-bin` envelope** (ADR-007) so the **workspace does its own C5/C7/C8** against
   the original caller identity — the hub does not re-authorize the capability, it routes.

The hub interprets neither the capability nor the payload. Unknown workspace → `NOT_FOUND` listing
the known ones.

### 3. Discovery — auto, from the registry

The hub reads `~/.local/state/rat/instances.json` (the same source as `rat ls`) and builds a
`name → addr` table of every **local** running workspace, pruning dead pids. **Remote** workspaces
(other machines) are added via hub config / the NATS-leaf transport (§5). So locally, `rat hub`
needs **zero config** — it federates whatever is already running.

### 4. Client addressing

`rat call --hub <addr> --workspace <name> rat://… --as <caller>` appends the `rat-workspace` header
and dials the hub instead of a single daemon. A bare daemon ignores an unexpected `rat-workspace`
header, so the flag is harmless when pointed at a non-hub. Same capability URIs, same `--as` — the
workspace name is a **routing prefix**, not a change to the call.

### 5. Transport — local sockets now, NATS-leaf to refine

First cut: the hub dials workspace daemons by their registry `addr` (local unix sockets / TCP). The
**cross-machine** story is the NATS-leaf federation (rat's event bus, core thing #4): each workspace
daemon leaf-connects **outbound** to the hub's NATS; the hub routes over subjects
(`rat.<workspace>.invoke.<cap>`); workspaces never bind public. Deferred to a follow-up (Q01).

### 6. Workspace ≠ tenant

Two **orthogonal** isolation dimensions, not to be conflated:

| Dimension | What | Granularity | k8s analog |
|---|---|---|---|
| **Workspace** | a separate daemon/plane (own process, plugins, code-fs) | coarse, deployment-level | separate **clusters** |
| **Tenant** | within one plane, the tenancy axis isolates state/creds | fine, logical | **namespaces** |

The hub federates **workspaces**; the tenancy axis isolates **within** each. One principal → grants
scoped per-workspace.

## Consequences

**Positive.**
- **One guarded door, not N.** Workspaces stay private; only the hub is reachable + hardened. The
  attack surface stops scaling with workspace count — the security win is structural.
- **Zero-config local federation.** `rat hub` federates whatever `rat ls` shows; the addressing
  model (`--workspace`) is identical from a laptop to a multi-region fleet.
- **No new core, no new contract.** Reuses `CapabilityInvokeService` + the instance registry; the
  hub is a pure forwarder. Six-thing core untouched.
- **Identity composes at the hub** (when added): authenticate once, each workspace authorizes per
  its own C5 against the forwarded identity.

**Negative — accepted.**
- **First cut forwards only unary `Invoke`.** `InvokeServerStream` (e.g. `state.Watch`) +
  `ControlService` (admin) forwarding are deferred (Q02) — a transparent any-method proxy is the
  refinement.
- **No identity-at-hub yet.** The hub forwards the caller's `--as` as-is (trust-asserted, same gap
  as a direct call). It is **localhost/trusted-network only** until the identity plugin + TLS land
  (the security-responsibility-model ADR, Q03). A public hub bind MUST require TLS + identity.
- **Local-only discovery** in the first cut (one machine's registry). Cross-machine = config /
  NATS-leaf (Q01).
- The hub is **another process to run**; for a single workspace it's pure overhead (don't use it).

**Neutral.** The hub is near-stateless — it reads the registry per call (cheap) and holds no plane
state; restarting it loses nothing.

## Open questions

- **Q01 — NATS-leaf transport** for cross-machine, outbound-only federation + global addressing +
  per-workspace NATS accounts (identity/tenancy for free). The real "fleet" mechanism.
- **Q02 — transparent any-method proxy:** forward `InvokeServerStream` / `InvokeBidiStream` /
  `ControlService` via a passthrough codec, so `rat status --workspace`, watches, and admin all
  route through the hub.
- **Q03 — identity-at-hub + TLS:** authenticate the principal once at the hub (OIDC/token), require
  TLS on a public bind — the secure-by-default coupling. Belongs to the security-model ADR.
- **Q04 — workspace ACLs:** which principals may reach which workspaces (the per-workspace grant).

## Alternatives considered

- **Expose each daemon directly** (no hub). Rejected: N exposure points, no addressing, no
  auth-once — the attack surface scales with workspace count.
- **A full P2P mesh** (every client+workspace on a WireGuard tailnet, no hub). Deferred: gets
  reachability + encryption but not rat-level addressing/authz/audit-once; it's a *ring-1*
  (environmental) option that composes *under* the hub, not a replacement for it.
- **Put federation in the per-plane core.** Rejected: federation is **above** planes; baking it into
  the per-project daemon would violate the six-thing discipline and the orchestrator/client split
  (ADR-019).
- **A managed cloud control plane first** (the Teleport/Tailscale-coordination shape). Deferred:
  that's the SaaS endgame (Q01 + Q03 productized); the local `rat hub` is its honest first cut.

## Migration

Phase 10 (this ADR's build): a `rat hub` command (registry-discovery + unary `Invoke` forward) +
a `--workspace`/`--hub` flag on `rat call`; no proto change, `make breaking` clean (the hub reuses
the frozen `CapabilityInvokeService`). Next: Q02 transparent proxy → Q03 identity+TLS → Q01
NATS-leaf for cross-machine.

## Related

- [ADR-019](019-rat-serve-daemon.md) — the orchestrator/client (edge) split the hub
  sits on.
- [ADR-005](005-capability-invocation-model.md) — the `CapabilityInvokeService` the hub reuses as a
  generic relay.
- [ADR-007](007-call-context-transport.md) — the `rat-callmeta-bin` envelope the hub preserves so
  workspaces authorize the original identity.
- [ADR-023](023-rat-as-a-per-project-daemon.md) — per-project daemons + the instance registry the
  hub discovers from.
- The **shared-responsibility security model** (perimeter environmental · identity+C5+audit core) —
  established in conversation, owed its own ADR; the hub is its single-front-door realization.
