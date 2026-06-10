# ADR-042: Channel-authenticated plugin identity — close the C5 forgery gap with a per-launch token

## Status: Accepted (2026-06-10)

## Context

A code-level review of `core/` (not the docs) surfaced that the spike's capability-invoke
gateway built its **C5 authorization decision on a self-asserted caller identity**. The wire
contract is explicit that this is wrong: [`common/v1/context.proto`](../../../contracts/proto/rat/common/v1/context.proto)
says `Identity.caller_plugin` is "DERIVED server-side from this hop's channel credential (C2),
RE-DERIVED every hop, and NEVER trust wire-supplied values." But the gateway read it straight
from the inbound envelope:

```go
// core/gateway/gateway.go (before)
caller := in.GetIdentity().GetCallerPlugin()   // the value the caller put there
d := g.reg.Authorize(caller, capURI)           // … then authorize on it
```

The comment alongside admitted it ("real channel authentication (C2) is deferred"). The
consequence: **any launched plugin could set `caller_plugin` to another plugin's name and
inherit its `requires` grants.** For a single-tenant box that is benign; for the multi-tenant /
multi-plugin posture the platform advertises it voids C5 (capability authorization) and C7
(tenancy), because both are keyed off an identity the caller forges for free.

The full keystone (`SubjectAssertion` signing for the *end-user* principal, and mTLS on the
core↔plugin channel) is a larger build. This ADR takes the **first, load-bearing step**:
authenticate the *plugin* principal (`caller_plugin`) on the channel, so the authorization
spine stops trusting the wire. It deliberately scopes out end-user assertion signing and full
mTLS (see Non-goals).

### The two doors

`rat serve` already exposes the gateway on **two listeners** (see [`core/cmd/rat/main.go`](../../../core/cmd/rat/main.go)):

- the **control listener** — a per-project unix socket (ADR-023) or an operator TCP endpoint —
  where `rat call` and the `ControlService` connect. Trusted by *reachability* (filesystem perms
  / network), exactly as the live control plane already is (ADR-027 / [control.go](../../../core/cmd/rat/control.go)).
- the **plugin-callback listener** — `0.0.0.0:<auto>` — the door launched plugins (drivers like
  the scheduler/bff) dial back on to invoke capabilities. This door is reachable off-host and is
  where the forgery matters.

That split is the seam: the operator door keeps reachability trust; the plugin door gets
authentication.

## Decision

**`rat` mints a secret per-launch bearer token for every plugin it launches, injects it as
`RAT_PLUGIN_TOKEN`, and registers `token → plugin-name` with the gateway. On the plugin door the
gateway authenticates each call by that token and derives `caller_plugin` from it — ignoring the
wire envelope. The operator door is unchanged.**

### 1. Token mint + injection (launch + live-add)

A 128-bit random token is minted per launched plugin and injected as a launch-env default
alongside the existing `RAT_GATEWAY` / `RAT_PLUGIN_NAME` (`injectLaunchEnv`, control.go). Both
entry points do it identically:

- the **initial set** — `launchPlane` mints, injects, and registers every plugin's token, then
  flips `gw.RequirePluginAuth(true)`.
- **live-add** — `ControlService.RegisterPlugin` mints + registers a token on every add, and
  drops it on every rollback / `DeregisterPlugin` (so a revoked plugin's token stops
  authenticating).

A relaunch re-mints; only the launched plugin and the gateway ever hold a given token.

### 2. Gateway derives identity from the channel (C2)

The gateway gains a `token → name` registry and two gRPC interceptors mounted **only on the
plugin-door server**. Each call is authenticated at the interceptor; the derived name is stamped
onto the context, and `openCall` prefers it over the wire envelope:

```go
// core/gateway/gateway.go (after)
caller := authenticatedCaller(ctx)          // channel-authenticated (token) when present
if caller == "" {                           // operator door / attach mode: fall back to the envelope
    caller = in.GetIdentity().GetCallerPlugin()
}
```

On the plugin door (`RequirePluginAuth(true)`): a missing or unknown token is rejected
`Unauthenticated` before any capability decision. Trace, tenant, subject and deadline are still
read from the envelope — only `caller_plugin` becomes channel-derived.

### 3. SDK presents the token

`rat.plugin.Gateway` reads `RAT_PLUGIN_TOKEN` and sends it as a `rat-plugin-token` metadata
header on every `Invoke`. Backward-compatible: a plugin without a token simply omits the header
(and is rejected on an authenticating plugin door — which is the point).

## Consequences

### Positive

- **C5 is sound for plugin-to-plugin calls.** Authorization keys off an identity the caller
  cannot forge: impersonating another plugin now requires *its* secret token, which the gateway
  hands only to that plugin at launch. Proven by tests (`core/gateway/gateway_test.go`): a call
  bearing plugin A's token but an envelope claiming to be B authorizes **as A** (and a forged
  envelope claiming a privileged name is ignored).
- **No wire change, no new core thing.** It's the API gateway + registry doing their existing
  jobs over an authenticated channel. The six-thing count is unchanged (token auth is a
  correctness condition of the *identity gateway*, not a 7th responsibility — see
  plugin-architecture.md "Cross-plugin concerns").
- **The off-host plugin door is no longer anonymous.** A 0.0.0.0 listener that previously
  accepted any well-formed envelope now requires a registered token.

### Negative / costs

- **Bearer token, not mTLS (yet).** The token travels on a plaintext channel; a network attacker
  who can read the callback traffic can replay it. This is strictly better than self-assertion
  (you must *steal* a secret, not just type a name) but it is not the end state — mTLS on the
  core↔plugin channel is the follow-up (and the `SubjectAssertion` M4 note in context.proto still
  requires authenticated transport for multi-tenant).
- **Shared-door dev topology stays unauthenticated.** When control is plain TCP with no separate
  callback listener, plugins share the operator door and are not authenticated; `rat serve` logs
  this posture. The secure default (unix-socket control + 0.0.0.0 callback) authenticates the
  plugin door. Mirrors the hub's secure-by-default binding (ADR-034).
- **Direct plugin↔plugin dialing is still out of scope.** This closes plugin→**gateway** identity
  forgery. A plugin that dials another plugin's port directly (bypassing the gateway) is a
  separate concern that mTLS between core and plugin closes — tracked as the follow-up.

## Non-goals (explicit follow-ups)

1. **mTLS on the core↔plugin channel** — replace/augment the bearer token with channel certs so
   the token can't be replayed and direct plugin-to-plugin dialing is authenticated.
2. **`SubjectAssertion` signing/verification** — the *end-user* principal (PRINCIPAL 2 in
   context.proto) is still an unsigned passthrough; minting + verifying the core signature is the
   second half of the keystone.
3. **Attach mode** — token auth is launch-mode only (rat mints tokens for what it launches). In
   attach mode `RequirePluginAuth` stays false and the envelope fallback is retained.

## Alternatives considered

- **Per-listener trust with no token (derive identity from *which* door).** Rejected: the plugin
  door is shared by all launched plugins, so the door alone identifies "a plugin," not *which*
  one — useless for per-plugin C5.
- **mTLS now.** The right end state, but a materially larger change (cert issuance, rotation,
  plugin-side trust store). The token closes the forgery today and is a clean stepping stone; mTLS
  becomes "swap the credential, keep the `token→name` seam."
- **Require a token on every door (including operator).** Rejected: breaks `rat call`'s
  reachability-trust model and the `ControlService`, which are intentionally operator-authenticated
  by the control listener's perms (ADR-023/027).

## References

- [`common/v1/context.proto`](../../../contracts/proto/rat/common/v1/context.proto) — the keystone this implements (C1/C2/C5/C7).
- ADR-034 — the security responsibility model + secure-by-default binding (the hub edge analogue).
- ADR-023 (per-project unix socket) / ADR-027 (live control plane) — the operator-door trust model.
- reviews: the 2026-06-10 code-level gap analysis (gap #2) that prompted this.
