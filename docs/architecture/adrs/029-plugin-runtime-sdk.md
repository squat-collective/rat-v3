# ADR-029: The plugin runtime SDK — `ratplugin` (Serve · Call · CallerTenant)

## Status: Accepted (2026-06-04) — built (Phase 10)

## Context

A plugin author currently hand-writes two repeated chunks of boilerplate:

- **Serving** (to *provide* a capability): parse `RAT_PLUGIN_ADDR`, `net.Listen`, `grpc.NewServer`,
  register the servicer, graceful drain. ~25 lines, identical in every plugin.
- **Consuming** (to *require* one): dial `RAT_GATEWAY`, build the `rat-callmeta-bin` envelope
  (identity + traceparent, [ADR-007](007-call-context-transport.md)), `Invoke` the capability,
  marshal/unmarshal. The exact line `[("rat-callmeta-bin", rc.SerializeToString())]` is copy-pasted
  across `dbt-runner`, `state-postgres`, and every other consumer.

This is the cross-cutting machinery a plugin author should **never** hand-write — and getting it
*wrong* (a malformed envelope, a missing trace) silently breaks C1/C5/C7. There is precedent for
the fix: `rat/contrib.py` is already a **hand-written helper layer** alongside the generated stubs
(`contribute_ui()`). We generalize that into a proper plugin **runtime SDK**.

This is one rung of the authoring DX ladder:

```
  Layer 3  FRAMEWORK   capabilities/ folder = wiring   (ADR-030, a later spike)
  Layer 2  RUNTIME     ratplugin.Serve / Gateway().Call   ← THIS ADR
  Layer 1  DISTRIB.    plugin-base-{go,py} (SDK ships)   (ADR-026 update, done)
  Layer 0  CONTRACTS   generated stubs                  (frozen rat/2.0)
```

The deliberate sequence is **primitives before conventions**: the explicit SDK ships first; the
filesystem framework ([ADR-030], if it earns its place) is built on top and *compiles down to*
these primitives. Conventions that emerge from + compile to explicit primitives (Rails on Ruby,
Next on React) age well; conventions that hide the primitives are cages. The closest analogs —
Kubernetes `controller-runtime`, Terraform `terraform-plugin-sdk` — are **explicit SDKs, not
filesystem frameworks**, for exactly this class of "plugins for an orchestrator."

## Decision

Ship a thin, **hand-written `ratplugin` SDK per language**, *inside* the SDK packages so it rides
in the `plugin-base-{go,py}` images automatically — **zero extra setup for the author**. Three
primitives, plus small env helpers.

### 1. `Serve` — the whole serving dance, one call

```go
func main() {
    k := &keyring{secrets: ratplugin.EnvMap("RAT_SECRETS")}
    ratplugin.Serve(func(s grpc.ServiceRegistrar) {
        secretv1.RegisterSecretServiceServer(s, k)
    })
}
```

`Serve` reads `RAT_PLUGIN_ADDR`, listens, lets the author register one *or many* servicers in the
closure, serves, and drains gracefully on SIGTERM. The closure (not a fixed single-service
signature) keeps multi-axis plugins trivial.

### 2. `Gateway().Call` — consume a capability, one call

```go
gw := ratplugin.Gateway()                         // RAT_GATEWAY + RAT_PLUGIN_NAME, once
var resp secretv1.ResolveResponse
gw.Call(ctx, "rat://secret/v1/resolve",
        &secretv1.ResolveRequest{SecretRef: "ref://state/dsn"}, &resp)
```

`Call` builds the `rat-callmeta-bin` envelope (caller identity = `RAT_PLUGIN_NAME`, a fresh random
traceparent), marshals the request, `Invoke`s through the gateway, and unmarshals the result. The
cross-cutting correctness (C1 trace, the identity the gateway authorizes from) is done **once,
right**, not per plugin.

### 3. `CallerTenant` + `EnvMap` — the small repeated reads

`CallerTenant(ctx)` reads the calling tenant out of the *incoming* `rat-callmeta-bin` (for C7
tenant-scoping, which every backend does by hand today). `EnvMap("RAT_SECRETS")` parses the
`k=v,k=v` env convention.

### 4. The scaffold emits the slim form; the escape hatch stays

`rat plugin init` generates a plugin that *uses* `ratplugin` (so fresh plugins are
idiomatic-by-default, not boilerplate-by-default). A plugin that needs full control just writes a
plain `main.go`/`main.py` against the raw stubs — the SDK is a convenience, never a requirement.

## Consequences

**Positive.**
- A plugin collapses to *its handler methods + a 3-line `main`* (`keyring`: ~45 → ~12 lines).
- Consuming a capability is one call, not a copy-pasted envelope — and the envelope is correct by
  construction (the #1 place hand-rolling silently breaks C1/C5/C7).
- The cross-cutting machinery lives in **one audited place** per language, so a fix (e.g. trace
  format) lands once for all plugins, not in N copies.
- Lowers the floor for the community without raising a ceiling (escape hatch intact).

**Negative — accepted.**
- A hand-written SDK to maintain **per language** (Go + Python now; TS/Rust next), and it must
  track the frozen wire (callmeta shape, Invoke). Mitigated: it's thin, and it sits *on* the
  generated stubs.
- One more thing in the base image (small).
- Generic `Call(cap, req, resp)` is stringly-typed on the capability URI; typed per-axis clients
  (`gw.Secret.Resolve(...)`) are nicer but need generation — deferred (Q01).

**Neutral.**
- `ratplugin` lives in the SDK module, so it versions with the contracts.

## Open questions

- **Q01 — Typed per-axis clients.** Generate `gw.Secret.Resolve(ctx, ref)` wrappers (capability +
  request/response types inferred from the axis) over the generic `Call`. A codegen convenience;
  the generic `Call` is the floor.
- **Q02 — The filesystem framework ([ADR-030]).** `capabilities/<axis>/vN/<method>` folders that
  generate the registration glue, compiling down to `ratplugin.Serve`. A *spike-first* bet,
  validated on real plugins before it's blessed.
- **Q03 — Streaming capabilities.** `Call` is unary→unary. The server-stream / bidi variants
  ([ADR-008](008-streaming-capability-invocation.md)) get their own `Stream`/`BidiStream` helpers.

## Alternatives considered

- **No SDK (status quo) — generic `Invoke` only.** Rejected: it leaves the cross-cutting envelope
  for every author to hand-roll (and mis-roll). This ADR exists to remove that.
- **Build the filesystem framework first.** Rejected: that ships the magic before the primitives.
  The SDK is the substrate the framework would compile *to*; build the substrate first, validate
  it, then (maybe) the framework — see the K8s/Terraform precedent.
- **A heavy per-language framework (annotations, DI, lifecycle hooks).** Rejected for v1: three
  primitives cover the real boilerplate; more is speculative.

## Migration

Additive; **Phase 10**, SDK + scaffold + base-image only (no proto change). Steps: this ADR →
`contracts/sdks/go/ratplugin/` + `contracts/sdks/python/rat/plugin.py` → rebuild
`plugin-base-{go,py}` so the SDK ships → scaffold emits the slim form → refactor `keyring` (and the
example plugins, opportunistically) onto it as the first consumer. `make breaking` stays clean.

## Related

- [ADR-026](026-plugin-authoring-and-packaging.md) — the authoring toolkit + (its Phase-10 update)
  the SDK base images this SDK rides in.
- [ADR-007](007-call-context-transport.md) — the `rat-callmeta-bin` envelope `Call` builds.
- [ADR-005](005-capability-invocation-model.md) — the `Invoke` gateway `Call` targets.
- `contracts/sdks/python/rat/contrib.py` — the hand-written-helper precedent this generalizes.
- ADR-030 (future) — the filesystem framework that would compile down to these primitives.
