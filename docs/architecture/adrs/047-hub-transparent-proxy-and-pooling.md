# ADR-047: Hub as a transparent proxy with connection pooling

## Status: Accepted (2026-06-10) — resolves [ADR-033](033-workspace-federation-hub.md) Q02

## Context

The code-level review of `core/` found **gap #5** in the federation hub (`rat hub`, ADR-033):

- **It dialed a fresh connection per call.** Every forwarded `Invoke` did `grpc.NewClient(addr)` +
  `defer conn.Close()`, and every edge auth check dialed the identity provider the same way. A full
  connection setup per request is a throughput wall and wasted handshakes.
- **It forwarded only unary `Invoke`.** The hub implemented the typed `CapabilityInvokeService.Invoke`
  and nothing else, so `InvokeServerStream` (`state.Watch`, streaming engines), `InvokeBidiStream`, and
  the entire `ControlService` (remote register / deregister / `rat status --workspace`) **did not cross
  the federation boundary.** Remote admin and watches simply didn't work through a hub.

ADR-033 already anticipated this: its **Q02** named a "transparent any-method proxy … via a passthrough
codec" as the intended shape, deferred from the first cut. This ADR implements it.

## Decision

**Make the hub a TRANSPARENT gRPC proxy: an `UnknownServiceHandler` + a passthrough codec that relays
every method — unary, server-stream, bidi, and `ControlService.*` — frame-for-frame to the selected
workspace, over POOLED connections.**

### 1. Transparent proxy (no per-method code)

The hub registers **no service**. Its gRPC server is built with `grpc.UnknownServiceHandler(proxyStream)`
+ `grpc.ForceServerCodec(proxyCodec{})`, so every inbound RPC lands in one generic handler. The handler
reads `rat-workspace` (and, when an identity provider is configured, authenticates `rat-token`),
resolves the workspace addr, opens a client stream to the **same** method, and relays opaque frames in
both directions (one goroutine per direction; half-close on caller EOF; the workspace's trailer + status
propagate back). The `proxyCodec` is named `"proto"` and set per-server / per-call (never globally), the
same trick the per-plane gateway's relay uses — so the hub never deserializes a frame and real proto
servers in the same process keep their codec.

The payoff: a new service crosses the hub with **zero hub changes**. `ControlService` works today
because of this, not because anyone wrote `ControlService`-forwarding code — matching the project's
"add anything, adapt quickly" steer ([[prefer-extensible-primitives]]).

### 2. Connection pooling

A `connPool` caches one long-lived `*grpc.ClientConn` per address (workspaces + the identity provider).
`grpc.NewClient` conns are safe for concurrent use and manage their own reconnection, so the hub reuses
them across calls instead of dialing per request. Discovery stays per-call (a workspace started after the
hub is picked up); the pool keys on the resolved address. Closed on hub shutdown.

### 3. Testability

Workspace resolution is an injectable `resolve(name) (addr, ok)` (the daemon wires it to the instance
registry, the same source as `rat ls`). Tests inject a resolver pointing at a local fake workspace.

## Consequences

### Positive

- **Federation is complete.** Watches, streaming engines, bidi, and remote admin all route through the
  one door. Proven by a test: unary `Invoke`, `InvokeServerStream` (3 frames), and
  `ControlService.ListPlugins` all forward through the hub, an unknown workspace is `NotFound`, and the
  pool holds exactly one reused conn after several calls. `-race` clean.
- **Throughput: no dial-per-call.** Pooled conns amortize setup across all calls to a workspace.
- **Extensible by construction.** The transparent proxy forwards future services/methods with no hub
  change — the most future-proof shape, and exactly what ADR-033 Q02 intended.
- **Security model unchanged.** The rat-callmeta-bin envelope is forwarded verbatim so the workspace
  authorizes the original caller; edge auth (ADR-034) and the secure-by-default bind guardrail are kept.

### Negative / costs

- **Response headers aren't propagated (only frames + trailer + status).** rat doesn't rely on custom
  response headers today; first-frame header propagation is a small additive refinement if needed.
- **A generic stream relay is subtler than a typed method.** Half-close, trailer, and error propagation
  must be exactly right — covered by the `-race` test, but it is more careful code than `Invoke`-only.
- **Pool conns are never evicted within a run.** A workspace that permanently moves address leaves a
  stale idle conn (cheap; grpc reconnect handles transient moves). LRU/TTL eviction is a later nicety.
- **No drain of in-flight proxied streams on hub stop.** The hub `GracefulStop`s its server; long-lived
  watches are cut at shutdown (acceptable — clients reconnect).

## Alternatives considered

- **Implement the three typed `Invoke` methods + a typed `ControlService` relay.** More explicit, but
  more code, and every *new* service would need new hub code — the opposite of the extensibility goal.
  Rejected for the transparent proxy.
- **Keep dialing per call.** Rejected: the throughput wall is the gap.
- **Register a global passthrough codec named "proto" (`encoding.RegisterCodec`).** Rejected: it would
  clobber the real proto codec process-wide, breaking ordinary proto servers in the same process. The
  per-server / per-call `ForceCodec` keeps it scoped.
- **NATS-leaf transport now (ADR-033 Q01).** Out of scope — this ADR is the local-socket transparent
  proxy + pooling; cross-machine NATS-leaf remains Q01.

## Related

- [ADR-033](033-workspace-federation-hub.md) — the federation hub; this resolves its **Q02** (transparent any-method proxy). Q01 (NATS-leaf) remains open.
- [ADR-034](034-security-responsibility-model.md) — edge auth + secure-by-default bind, preserved.
- ADR-005 — the per-plane gateway's opaque byte-relay (the passthrough-codec pattern reused here).
- [[prefer-extensible-primitives]] — the steer behind the transparent (forward-anything) proxy.
- reviews: the 2026-06-10 code-level gap analysis (gap #5).
- Prior art: mwitkow/grpc-proxy (UnknownServiceHandler + passthrough codec stream relay).
