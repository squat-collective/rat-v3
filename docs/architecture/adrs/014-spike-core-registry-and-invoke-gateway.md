# ADR-014: The spike core — a manifest-driven registry + capability-invoke gateway (C5 made real)

## Status: Accepted (2026-06-01)

## Context

[ADR-013](013-phase-1-spike-and-commitment-gate.md) set Phase 1 to begin as a time-boxed
contract-de-risking spike whose job is to turn **C5** (capability enforcement) from a
self-asserted stub into a *real, manifest-derived* enforcer — and, in doing so, to try to break
a frozen contract while the freeze is still local/cheap.

A repo sweep grounded the gap precisely. What already exists:
- **The wire.** `core/v1` `CapabilityInvokeService` (`Invoke` + `InvokeServerStream` +
  `InvokeBidiStream`, ADR-005/008); the `(rat.common.v1.capability)` method annotation on every
  axis RPC (`annotations.proto`, field 70001); frozen manifests with `provides`/`requires`
  capability lists (real examples at `contracts/examples/*.plugin.yaml`).
- **A faithful non-test gateway** — `plugins/bench/latency-go/gateway.go` — that implements
  `Invoke` + `InvokeServerStream`, routes `capability → (service, method)` by reading the proto
  annotation across multiple file descriptors, re-stamps the `rat-callmeta-bin` identity
  envelope, and relays opaque frames via a passthrough codec. **But it skips C5 entirely** (no
  authorization — it's a latency probe).
- **Seven per-axis test stubs** (`plugins/*/gateway_test.go`) that *do* enforce C5 — but against
  a **hardcoded allowlist** passed in by the test (`newGateway(conn, caller, allowed []string)`;
  `if !g.allowed[cap] { PermissionDenied }`). Single-provider, in `_test.go`, the allowlist
  fabricated by the test — it never reads a manifest.

What does **not** exist: any **registry**; any **Go structs for manifests** (they're YAML,
validated in Python against `plugin.v1.json`); any gateway whose authorization is *derived from
declared manifests*. That last clause is the whole of C5: today `declared == provided` is
asserted by a test, not enforced from the plugin's own declaration.

## Decision

**Build the minimum real core that makes C5 real — two of the six things — and run it against
the existing reference plugins + composition pipeline. Authorization is derived from loaded
manifests, never hardcoded.**

### 1. Registry (subset of core thing #1)

A new Go package that:
- **Loads manifests** from a manifest directory — parse the frozen `plugin.v1.json` YAML shape
  into hand-written Go structs (`kind`, `metadata.{name,version}`, `provides[]`, `requires[]`,
  `compatible_core`). No manifest codegen yet (deferred; small, acceptable schema dup for a spike).
- **Indexes** `(kind, name, version) → plugin` and builds two capability maps from the manifests:
  `capability URI → providing plugin` (from each `provides`) and per-plugin `requires` sets.
- **Builds the route table** `capability URI → (service, method)` by reading the
  `(rat.common.v1.capability)` annotation off the proto service descriptors (exactly as
  `bench/latency-go/gateway.go` does, generalized over all axis file descriptors).
- Validates capability URIs against the `rat://<axis>/v<major>/<capability>` grammar.

### 2. Capability-invoke gateway (subset of core thing #6, the API gateway)

Implement `CapabilityInvokeService`, **seeded from `plugins/bench/latency-go/gateway.go`**, with
the one change that matters: the **C5 decision is derived from the registry**, not an allowlist.
A call to capability `X` by caller `P`, resolved to provider `Q`, is allowed **iff**
`X ∈ P.requires` **and** `X ∈ Q.provides`. Otherwise `PERMISSION_DENIED`. Keep the existing
traceparent check (C1), identity re-stamp (ADR-007), and passthrough relay. **Emit one audit
record per decision, including denials** (C4 seed).

### 3. Explicitly DEFERRED (not the spike)

Reconciler, event bus, full identity gateway (real authN — caller identity is taken from the
authenticated channel as today's stubs do), full state gateway + per-plugin namespacing,
deployment-runtime process launch (plugins are started as local gRPC servers, as the composition
and bench already do), tenancy/billing/observability. Boot = *load manifests from a directory +
dial already-running plugin servers*; no tier-0 plugin machinery. The spike proves the
**enforcement spine**, not a production core.

### 4. Where it lives

A new Go module `core/` — module `github.com/rat-dev/rat/core`, with
`replace github.com/rat-dev/rat/gen => ../contracts/sdks/go` (the established example pattern).
Packages: `core/registry`, `core/gateway`; a Go test harness that boots the reference plugins +
the registry-backed gateway. A thin `cmd/rat` main is **not** part of the spike (the harness is
the entry point), but the package boundary leaves room for it.

### 5. The de-risking test — the actual point

- **Composition-on-Go:** re-run the existing composition pipeline
  (`catalog.get-table → register-table → engine.query → format.scan → format.overwrite →
  catalog.commit-table`) through the **real registry-backed Go gateway**, with a manifest per
  plugin, and assert it produces the same target the Python composition does. The wire is
  identical; only the gateway is now Go + manifest-driven.
- **C5 negative:** a plugin invoking a capability **not in its manifest `requires`** is denied by
  the core (`PERMISSION_DENIED`) — and the denial emits an audit record.
- **C1 crash-mid-strategy:** kill the strategy mid-`Apply`; the at-least-once re-run is a no-op
  (`idempotency_key`, `already_applied`) — no double-apply.
- **C2 truncated stream:** a producer that ends early (fewer than `expected_rows`) fails the
  write rather than committing partial.
- **Freeze-reopen trigger (the prize):** if deriving routing + enforcement from real manifests +
  the frozen `invoke.proto`/annotation/URI grammar reveals the wire shape is **insufficient**
  (e.g. the strategy axis needs a commit/abort shape to make crash-mid-`Apply` safe; or the
  annotation can't disambiguate a route), that is a **freeze-reopen**, surfaced while local —
  exactly the cheap-fix this spike exists to buy.

## Consequences

**Positive.**
- C5 stops being self-asserted: the authorization decision is read from the plugin's *own
  declared* manifest, enforced by the core.
- The registry + gateway are **Phase-1 seed code, not throwaway** (unlike the 7 test stubs).
- The composition gains a Go, manifest-driven enforcement path alongside the Python conformance
  harness; any wire regret surfaces while the freeze is still local.

**Negative — accepted.**
1. **Two of six things only.** The spike proves the enforcement spine, not the reconciler/bus.
   A registry that loads from a directory (no state-backend plugin) is *not* the production
   registry (which will persist/index via the state-gateway). Accepted: the spike's question is
   "does C5 enforce against real manifests, and does the frozen wire suffice?", not "is the core
   production-ready?"
2. **Hand-written manifest structs** (no codegen from `plugin.v1.json`) — a small schema dup.
   Accepted for a spike; manifest-from-schema codegen is a full-build item.
3. **Plugins as local gRPC servers**, not launched by a deployment-runtime. Accepted: process
   isolation (D1) is a separate Phase-1 finding; the spike targets C5/C1/C2.

**Neutral.** Identity is taken from the authenticated channel (no real authN plugin yet), exactly
as the current stubs do; the honesty banner on `plugin.v1.json` + `CONTRACT.md` stays until the
full enforcer lands.

## Alternatives considered

1. **Start with the full six-thing core.** Rejected ([ADR-013](013-phase-1-spike-and-commitment-gate.md)):
   the spike's job is the C5 enforcement spine, not the whole core.
2. **Extend the Python composition gateway to be manifest-driven** instead of writing Go.
   Rejected: the core is Go ([ADR-004](004-core-language-go.md)); the spike must prove the *Go*
   enforcement path that becomes the real core, not a Python stand-in. The Python composition
   stays as the cross-axis conformance harness.
3. **Reuse a test stub, swap the allowlist for a manifest read.** Rejected as insufficient: the
   stubs are single-provider, in `_test.go`; the spike needs a multi-provider registry + a
   standalone gateway package. The right seed is the bench `gateway.go`, not a stub.
4. **Hardcode the registry index (skip manifest parsing).** Rejected — deriving authorization from
   the *declared* manifest IS the self-assertion this spike kills. Hardcoding would re-create the
   stub.

## Migration

New `core/` module → seed the gateway from `plugins/bench/latency-go/gateway.go` → add
`core/registry` (manifest loader + capability/route index) → wire the gateway's C5 decision to the
registry → add the Go composition-equivalent test + the C5/C1/C2 cases → **CI from commit 1**
(`buf breaking` + `make {conformance,composition,validate-manifests}` + `go test ./core/...`).
No change to frozen contracts unless the spike surfaces a regret — then a new ADR + an additive
bump (or a `v2` for the affected axis), decided while the freeze is still local.

## Related

- [ADR-013](013-phase-1-spike-and-commitment-gate.md) — the spike decision this implements.
- [ADR-005](005-capability-invocation-model.md) · [ADR-008](008-streaming-capability-invocation.md) — the core-mediated invoke model + streaming variants the gateway realizes.
- [ADR-001](001-everything-is-a-plugin.md) — the six things (registry + API gateway are the two this spike seeds).
- [ADR-011](011-manifest-schema-freeze-and-per-kind-layer.md) — the manifest schema the registry parses.
- [`core/v1/invoke.proto`](../../../contracts/proto/rat/core/v1/invoke.proto) · [`common/v1/annotations.proto`](../../../contracts/proto/rat/common/v1/annotations.proto) — the frozen wire.
- `plugins/bench/latency-go/gateway.go` — the seed gateway · `plugins/*/gateway_test.go` — the C5 stub pattern · `plugins/composition/` — the pipeline the Go gateway re-runs.
- [reviews/09](../../../reviews/09-phase-1-gate-review.md) — the gate review that made C5 the spike's centerpiece.
