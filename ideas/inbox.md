# Ideas inbox

Append new ideas at the bottom. Format: `## YYYY-MM-DD — [tags] one-line title` + a few sentences.

---

## 2026-05-30 — [naming] Project codename

`RAT v3` is the working title since the folder is `rat/`. Possible future renames if we want a distinct identity:
- **Pith** (the central core of a stem)
- **Atrium** (the central courtyard everything flows through)
- **Conduit**
- **Sieve**
- **Burrow** (where RATs live — ties to brand)

Decision can wait until we ship. The internal codename matters less than the eventual product name.

---

## 2026-05-30 — [marketplace, distribution] Plugin distribution as a first-class concern

VSCode's marketplace + Cargo's crates.io are why those ecosystems flourished. RAT v3 needs a plugin marketplace from year 1, not as an afterthought. Options:
- GitHub-based: discover via topic tag `rat-plugin`, install via `gh` CLI.
- Dedicated registry: like crates.io, hosted by the project.
- Multi-source: operator points at N registries (one curated, one community, one internal).

Open question: should the marketplace ALSO be a plugin? (Yes, almost certainly. `kind: marketplace`.)

---

## 2026-05-30 — [contracts, schemas] Generate manifest schema from proto?

If proto files define the gRPC service shapes, we could generate the `plugin.yaml` manifest schemas from them. Reduces drift between "what a plugin says it provides" and "what the proto actually defines."

Tradeoff: protobuf and JSON Schema have different expressiveness. Generation works for simple cases; complex constraints (cross-field validation) might not transfer. Worth a spike.

Related: Q03 in [ADR-001](../docs/architecture/adrs/001-everything-is-a-plugin.md).

---

## 2026-05-30 — [event-bus, performance] Event bus failure modes

If the event bus is the coordination substrate, what happens when it's degraded?
- Stale notifications → reconciler converges slowly but eventually
- Lost events → reconciler needs idempotent retries (every loop iteration)
- Out-of-order events → reconciler must be reorder-tolerant

This argues for the reconciler being **the source of truth, not the events**. Events are hints for "you might want to look now"; the reconciler always re-reads state. Same model as K8s.

Future ADR: event-bus durability semantics + reconciler-as-source-of-truth.

---

## 2026-05-30 — [security] Plugin sandboxing

A 3rd-party `rat-plugin-foo` runs *somewhere* in the operator's environment. Trust model:
- Solo: same process as core; full trust. Container model overkill.
- Team: containerized; trust at the network level.
- Enterprise: signed images + capability whitelist + network policy.

Each level is a different `deployment-runtime` plugin doing different isolation. The core doesn't enforce sandboxing — the deployment-runtime does. Worth an ADR when we start implementing the runtime axis.

Related: v2's ADR-017 (Python pipeline trust model) — same pattern.

---

## 2026-05-30 — [migration] Bridge plugins from v2 to v3

**Resolved in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md) D7**: no migration plan now; build a tool reactively if a real production user surfaces. v2 has no production users today, so pre-building optimizes for users who don't exist.

---

## 2026-05-30 — [meta-principle, language-choice] AI-assisted rewriting lowers language-choice stakes

Tom's reasoning during Q01: *"let's go with Go we could rewrite it with AI if we want to go rust someday"*. This is a load-bearing meta-principle worth banking. When picking foundational tech (language, framework, etc.), the cost of "wrong choice" has shifted dramatically — AI-assisted refactoring of a 10k-LOC codebase is genuinely viable. So: bias toward velocity-friendly + ecosystem-aligned choices NOW; accept that re-language is a 2-4 week project later if needed. **Don't over-optimize for "perfect long-term language."** This applies recursively to framework choices, ORM choices, serialization choices, etc.

Save as principle for the project; cite in future ADRs when stuck on "this technology choice is hard."

---

## 2026-05-30 — [v2, strategy] Should v2 keep shipping?

**Q11 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** Implication of D7: if v2 has no production users and v3 is the real target, should we still implement v2's ADR-025 (on-demand planes) and ADR-026 (manifest+registry)? Those ADRs were valuable as *thinking* — they shaped v3's design — but actually building them in v2 might be wasted effort. Worth a separate decision when there's bandwidth.

Open question: when do we declare v2 feature-frozen?

---

## 2026-05-30 — [bundles] Default `rat-bundle-solo` composition

**Q12 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** What exact plugin set ships in the default solo bundle? Probably:
- state-backend: sqlite
- secret-backend: env
- scheduler-backend: in-process
- identity: anonymous
- deployment-runtime: local-process
- ui: web-portal
- engine: duckdb-embedded
- runtime: pyarrow-embedded
- format: iceberg-embedded
- catalog: sqlite-iceberg-catalog (or simpler — file-based catalog?)
- storage: local-fs
- observability: stdout
- marketplace: community-marketplace

But each is a real choice. Becomes ADR-003 (or similar) when Phase 0 lands. Versions matter — bundle pins specific plugin versions for reproducibility.

---

## 2026-05-30 — [security] Plugin authentication to core

**Q13 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** When a plugin contacts the core (or core contacts a plugin), what auth model? Options:
- Mutual TLS (cluster-style)
- Bearer tokens (simple but rotation matters)
- Both (mTLS for production, bearer for dev)
- None for solo (in-process), upgrade for team+

Probably the last — auth model varies by deployment-runtime. Future ADR when core API hardens.

---

## 2026-05-30 — [marketplace, UX] Marketplace plugin's discovery shape

**Q14 in [ADR-002](../docs/architecture/adrs/002-founding-tech-stack.md).** The marketplace plugin needs a UX: search by capability, by name, by author? Trust badges? Reviews? Compatibility checking (does this plugin work on my deployment)?

Worth a dedicated ADR when the marketplace plugin is being built. Look at: VSCode marketplace UX, Cargo's crates.io, Helm Hub, OperatorHub.io for patterns.

---

## 2026-05-31 — [contract, ADR-005] Where does re-stamped identity ride: payload.context or channel metadata? `[promoted → docs/architecture/adrs/007-call-context-transport.md]`

> **Resolved by [ADR-007](../docs/architecture/adrs/007-call-context-transport.md):** the whole cross-cutting envelope (trace + identity) moves out of the payload into a `rat-callmeta-bin` transport-metadata header — option (a), refined. This upholds ADR-005's generic-proxy guarantee (the gateway parses zero payload bytes) and keeps the keystone `RequestContext` shape verbatim, only changing its carrier. The reasoning below is kept as the historical record.

**Surfaced by building the 0d stub invoke-gateway** (`plugins/format/inmemory-go/gateway_test.go`) — exactly the kind of gap ADR-003 predicts a real implementation exposes.

ADR-005 / `core/v1/invoke.proto` says the gateway is a **generic proxy**: it routes by capability and forwards `payload` **without interpreting it**. But two clauses collide:
1. The gateway must **re-derive `identity.caller_plugin`** for the downstream hop and **never trust wire-supplied identity** (keystone, `context.proto`).
2. Every axis request carries `RequestContext` (incl. `identity`) **inside the payload** (field 1).

A proxy that doesn't deserialize the payload **cannot rewrite the embedded `identity`**. So the re-stamped identity has to travel somewhere the proxy *can* set without parsing bytes — i.e. **gRPC metadata** on the downstream call — and the providing plugin would read identity from metadata, not from `payload.context.identity`. That contradicts "RequestContext travels as field 1 of every request" (`context.proto`).

Three resolutions to weigh (→ likely a follow-up ADR amending 005/context):
- **(a) Identity rides in channel metadata; payload.context.identity is advisory/ignored.** Keeps the proxy truly generic. Cost: the "every RPC carries identity in field 1" invariant weakens to "field-1 context carries trace + deadline; identity is in metadata."
- **(b) The gateway DOES splice field 1.** It interprets only the well-known `RequestContext context = 1` prefix (uniform across all axes by construction) and rewrites `identity`, forwarding the rest opaquely. Costs "forwards payload without interpreting it" purity, but only for one structurally-guaranteed field.
- **(c) Two-channel: trace in payload, identity wholly out-of-band (metadata + the signed `SubjectAssertion`).** Most faithful to "never trust wire identity," most plumbing.

The stub does **(a)** (stamps `x-rat-caller-plugin`/`x-rat-tenant` into outbound metadata) and the reference plugin ignores identity entirely, so behavior is correct under any choice — but the **frozen** contract must pick one before `format/v1` (and every axis) is GA. Freeze-blocking-adjacent: touches `context.proto` + `invoke.proto`, both in the `rat/1` surface.

Open question: which of (a)/(b)/(c) — pick before any axis freezes.
Related: [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md), `contracts/proto/rat/common/v1/context.proto`, `contracts/proto/rat/core/v1/invoke.proto`, [ADR-003](../docs/architecture/adrs/003-two-references-before-contract-freeze.md) (the rule that made this surface).

---

## 2026-05-31 — [contract, ADR-005] The core-mediated Invoke is unary-only — server-streaming capabilities have no mediation path `[promoted → docs/architecture/adrs/008-streaming-capability-invocation.md]`

> **Resolved by [ADR-008](../docs/architecture/adrs/008-streaming-capability-invocation.md):** option (a) — add `InvokeServerStream` + `InvokeBidiStream` to the core invoke service (generic byte-relays, enforce-once-at-open, identity stamped into downstream metadata per ADR-007). Streaming is "unary with N frames"; the gateway stays axis-generic; central enforcement (ADR-005) holds for every cardinality. The reasoning below is kept as the historical record.

**Surfaced by building the 0d `runtime` reference** — the 0d forcing function (ADR-003) exposing a contract gap, like the ADR-007 identity-transport finding before it.

`runtime/v1`'s `Execute(ExecuteRequest) returns (stream ExecuteResponse)` is **server-streaming** (interim `ExecuteProgress` + terminal `ExecuteCompleted`). But ADR-005's `core/v1 CapabilityInvokeService.Invoke(InvokeRequest) returns (InvokeResponse)` is **unary** — it cannot carry a streamed response. So a strategy that `requires: rat://runtime/v1/execute` has **no core-mediated way to invoke it**: the gateway can route+enforce a unary call, not a stream. (Every other 0d axis so far — format/engine/storage — is unary and routes cleanly through the stub gateway; runtime had to be driven DIRECTLY, bypassing the gateway, which means its C2/C5/C7/C8 + traceparent seams are currently unenforced.)

This is freeze-relevant: `invoke.proto` is in the `rat/1` surface, and *any* axis with a streaming method (runtime today; future engine/observability streams) hits this.

Three resolutions to weigh (→ a candidate follow-up ADR, "streaming capability invocation"):
- **(a) Add `InvokeStream(InvokeRequest) returns (stream InvokeResponse)` to `invoke.proto`.** The gateway becomes a streaming generic byte-relay (same passthrough-codec trick, but it relays a stream of `result` frames). Enforcement (C2/C5/C7/C8 + traceparent) happens once at stream open, identity stamped into the downstream metadata as today. Cleanest + most consistent with the unary model; the gateway stays axis-generic. Cost: a second RPC in the frozen core surface + streaming relay plumbing.
- **(b) Streaming capabilities are direct-dial with a gateway-issued, capability-scoped token** (like the ArrowStream bulk-data leg, which already bypasses the core). The gateway mints a short-TTL token at a unary "open" call; the caller dials the provider's stream directly with it; the provider validates. Mirrors `storage.VendCredentials` / the bytes path. Cost: distributes enforcement to the callee (the exact honor-system ADR-005 rejected for control calls) — but maybe acceptable for the *streaming* subset since progress is liveness, not authz-bearing.
- **(c) Progress moves to the async event bus (`common/v1 Event`); `Execute` becomes unary** returning only the terminal result, with `ExecuteProgress` re-published as events keyed by `correlation_id`. Keeps the invoke contract unary. Cost: liveness loses request-scoped backpressure + becomes best-effort; couples runtime to the bus.

Leaning **(a)** — it preserves the central-enforcement property ADR-005 is built on and keeps the gateway generic; streaming is just the unary relay with N response frames. But it needs the ADR to weigh (b)'s perf argument for genuinely high-volume streams.

Open question: pick (a)/(b)/(c) before `runtime/v1` (or any streaming axis) routes through the gateway — and before `invoke.proto` freezes.
Related: [ADR-005](../docs/architecture/adrs/005-capability-invocation-model.md), [ADR-007](../docs/architecture/adrs/007-call-context-transport.md) (same "0d reveals the gap" pattern), `contracts/proto/rat/core/v1/invoke.proto`, `contracts/proto/rat/runtime/v1/runtime.proto`, `plugins/runtime/inmemory-go/harness_test.go` (the direct-dial workaround + its header note).

## 2026-06-02 — [experiment, ui] vscode-rat as a multi-environment RAT explorer (many connections)

**Surfaced while building the data-dev plane VS Code UI (step 6).** The extension should
manage **many named RAT connections** — like DBeaver/DataGrip for data planes — each
pointing at a RAT platform endpoint (a gateway today; a real core API gateway later).
One editor, N planes: `local`, `staging`, `prod`, per-tenant, per-region. This is the
"one UI, many planes" scalability story made concrete, and it's the natural shape once a
connection is just a URL.

- **Connection model:** `{ name, url }` (forward-room for `tenant`, `token`/auth, TLS).
  Persisted in the `ratDataDev.connections` setting (discoverable, Settings-Sync-able).
- **Tree:** connection-rooted (multi-root explorer) — connection → tables → snapshots;
  health view connection → plugins. Per-connection actions (run pipeline / query / search)
  via the connection's context menu; unreachable connections degrade gracefully.
- **Remote envs:** each remote RAT env runs its own gateway (or core); the extension just
  needs the URL. Follow-on: a **gateway "remote mode"** (point app.py at a remote
  S3+Postgres stack instead of the local in-proc DuckLake — the run-remote.py plumbing
  already exists) so a connection can target a genuinely remote plane, not just localhost.
- **Auth/tenant (next):** connections carry a tenant + token; the extension stamps them
  (the gateway/core honors identity per ADR-007). Ties into C5/C7 once the real core
  fronts the UI.

Status: **multi-connection model being built now** (connection store + add/remove/edit +
connection-rooted trees). Auth/tenant + gateway-remote-mode are the queued follow-ons.

---

## 2026-06-02 — [core, architecture] runtime plugin self-registration (a `RegisterPlugin` gRPC) `[promoted → docs/architecture/adrs/023-rat-as-a-per-project-daemon.md]`

> **Promoted into [ADR-023](../docs/architecture/adrs/023-rat-as-a-per-project-daemon.md) (2026-06-03):** runtime registration is now the *user model* (`rat add`, poetry-style — imperative writes the external spec, then hot-registers via the `SetProvider` keystone that this entry said was the blocker, now built). The federated *self*-register-by-dialing-in variant stays a future concern (ADR-023 Q07: quarantined `status`, not canonical). The original note is kept below.

**Surfaced in conversation with Tom while building the `rat serve` orchestrator + the
`ratctl` client.** Today the plugin set is **declarative**: `rat serve --plane plane.yaml`
lists the plugins and `rat` launches them (k8s-style desired-state, via the
deployment-runtime + reconciler). Tom floated the inverse model: **plugins register
themselves at runtime** by calling a gRPC command on a running `rat`
(`rat serve --config rat.yaml --port X`, no plugin list up front) — so `rat` is a pure
broker that *everything connects to* (plugins connect up to register; clients connect in
to issue commands). No DinD, plugins can live anywhere.

- **What it'd take (additive to the frozen wire — a new service is not breaking):** a
  `RegisterPlugin(manifest, endpoint) -> ok` gRPC on the core, making the **Registry**
  (one of the six core things) runtime-writable.
- **The blocking core gap — SAME as the Phase-A reconciler-rewire finding:** `gateway.New`
  fixes its provider-connection map at construction; there is no way to add a provider
  while serving. Runtime registration needs a **mutable, concurrency-safe** provider/route
  path (`AddProvider`/`Register`). That one change unlocks BOTH self-registration AND the
  deferred hot reconcile-restart. (See backlog: "wire the reconciler crash-restart loop.")
- **Trust shift:** a launched image is implicitly trusted; a self-registering plugin must
  be authenticated (C2 channel-auth / per-plugin token) before `rat` believes its declared
  capabilities. Localhost-accept for a dev plane; matters for shared planes.
- **Discipline:** touches a core thing → wants an **ADR** before building (CLAUDE.md #2/#3).
  Can **complement** the plane model (declarative for launched plugins + registration for
  external ones) rather than replace it.

Status: **PARKED (decided not to build now).** It's a *scale* feature (multi-host / dynamic
ecosystems); the project is solo + pre–Gate-B, and the "many UIs connect to the orchestrator"
goal is **orthogonal** to it (clients invoke capabilities regardless of how plugins registered
— proven by `ratctl`). Revisit when the launch-only model actually hurts; write the ADR then.

---

## 2026-06-03 — [distribution, ux] Ship rat as a GHCR binary + image — no make, no git clone

**Surfaced reviewing the "raw install → plugin complete" journey** — Stage 0 today is
`git clone` + a hand-cranked `podman run golang build`. That's the wrong front door. The
install story should be **two artifacts, nothing else**:
- **a prebuilt binary** (`rat` + `ratctl`) downloadable from a **GitHub Release / `ghcr.io`** —
  `curl -L … -o rat && chmod +x ./rat` (the founding vision's literal `chmod +x ./rat`), and
- **the daemon container image** on `ghcr.io/rat-dev/rat:<tag>` for the containerized/socket-mount
  + k8s path.

No `make`, no clone, no in-container Go build for a *user*. `make`/source stay for *contributors*
only. Plugin images likewise pulled from `ghcr.io` (ties to the marketplace idea above), not
built locally — so the whole getting-started is: grab `rat`, point it at a `plugins.yaml` whose
images are GHCR refs, done.

- **What it'd take:** a release pipeline — GitHub Actions building static `rat`/`ratctl` for
  linux/amd64+arm64 (+ macOS), publishing to Releases; a multi-arch image build → `ghcr.io`;
  versioned tags matching the `rat/N.M` seal scheme. A one-line install script (`get.rat.dev`-style)
  is the cherry on top.
- **Why it matters:** the architecture already *scales* from solo to cloud (same binary, different
  plugin set) — but the **install UX doesn't yet reflect that**. The binary+image distribution is
  what makes "solo dev tries rat in 60 seconds" real, and it's the natural home for the eventual
  signed-release + SBOM + version-pinning story.

Open question: do `rat` and `ratctl` ship as two binaries or one multi-call binary (`rat serve` /
`rat call` / `rat apply`)? The ADR-019 split says two (orchestrator vs client); but for *distribution*
a single `rat` binary that's both might be the friendlier front door (Tom keeps saying "`rat apply`",
not "`ratctl apply`"). Decide alongside the release pipeline.
Related: the marketplace/distribution idea above (plugin images on GHCR), ADR-019 (rat/ratctl split),
the founding `chmod +x ./rat` vision (docs/vision.md / CLAUDE.md).

---

## `code-fs` — a remote, collaborative code store as a PURE plugin (plug any storage) — 2026-06-04

**What Tom wants:** a thing that stores *code* and behaves like a **remote filesystem**, so work
becomes collaborative — and crucially, **a plugin you can plug *any* storage behind** (minio today,
another S3, "why not fs tomorrow"). It must be **just a plugin** — no new proto, no core change.

**The realization that killed the fs-axis attempt:** a *new axis* (`fs`) needs a new proto **and a
core recompile** (the gateway's `routableDescriptors()` is hardcoded). That's not "just a plugin."
So `fs` is **deferred** ([ADR-032](../docs/architecture/adrs/032-filesystem-axis.md) → Deferred).

**The plan (pure plugin, no proto):**

```
┌─ consumer (an app, an editor) ─┐
│  get/put/list  code/app/main.py│   ← the "filesystem" = the EXISTING state axis
└───────────────┬────────────────┘
                ▼  rat://state/v1/{get,put,list}      (PROVIDES — no new proto)
        ┌───────────────┐
        │   code-fs      │  the plugin we build
        └───────┬────────┘
                ▼  rat://storage/v1/vend-credentials  (REQUIRES — pluggable backend)
        ┌───────────────┐
        │ s3-storage     │  ← minio today; swap for gcs/anything; code-fs UNCHANGED
        └───────┬────────┘
                ▼  reads/writes one object per path in the bucket
            (shared S3 = collaborative; CAS via if_revision = no clobber)
```

- **PROVIDES** the existing `rat://state/v1/{get,put,list}` — `put code/x = bytes` (write a file),
  `get code/x` (read), `list code/` (list a dir). The path→bytes namespace *is* a filesystem.
- **REQUIRES** `rat://storage/v1/vend-credentials-{read,write}` — so the backend is **any storage
  plugin**. That's the "plug any storage" Tom asked for: swap `s3-storage`→`gcs-storage`, `code-fs`
  doesn't change. Same composition trick already proven (keyring↔vault swap under s3-storage).
- **Collaborative:** code lives in **shared** storage; CAS (`if_revision`) detects concurrent edits.
- **Pure plugin:** reuses two frozen axes (state + storage). **No proto, no `routableDescriptors`
  change, no core rebuild.** This is the whole point.

**The one honest caveat:** providing `state/*` makes `code-fs` a **state-backend**, and a plane has
**one** state provider (registry rejects duplicates). So in a code-focused plane, `code-fs` IS the
state-backend (code under `code/`, everything else under its own prefixes). If a plane ever needs a
*separate* fast local state-backend too, that's the duplicate-provider limit — and the real fix is
the **dynamic-descriptor gap** below (which lets `code-fs` provide a distinct `fs` axis cleanly).

**Two things this surfaced, parked for later:**
1. **Dynamic descriptors (the important one).** The gateway should learn axis protos from plugins at
   **runtime**, not from a hardcoded `routableDescriptors()`. Until then, "community can add axes
   without core changes" (ADR-001) is **aspirational** — a new axis = a core rebuild. Closing this
   makes the deferred `fs` axis (and any community axis) a *pure plugin*. This is the unlock.
2. **The `fs` axis itself** (ADR-032) — richer semantics (stat/delete/real dirs, **git-backing** =
   branches/history/merge) — revisit *after* #1, when KV-over-state isn't enough.

**Next step when we build:** `rat plugin init code-fs --kind state-backend --lang go`, implement
get/put/list over storage-vended S3 objects, pack, add to the kitchen plane behind `s3-storage`,
prove a `consumer → code-fs → s3-storage → keyring` write+read chain (all ALLOW + audited).

---

## code-fs spaces + per-user permissions (deferred — KISS for now) — 2026-06-04

**Idea:** evolve code-fs from one namespace into a **multi-space, permission-aware filesystem
manager** (the same shape s3-storage already has for connections). Register N **spaces** into
code-fs; each space = a named prefix on a storage backend + a per-user ACL; access depends on the
authenticated subject; perms map to scoped S3 creds.

**Decision (2026-06-04): defer — keep code-fs simple now (one space, no per-user authz, proxy in
the path). The door stays open; it's all additive.** Captured so it's not lost.

**The shape that formed (for when we pick it up):**
- **code-fs = a permissioned PROXY** (does the I/O → central CAS + audit + authz). This is *why*
  code-fs exists vs the raw storage axis, which is already a *broker* (vend creds, talk S3 directly).
  Model: **code-fs = permissioned proxy for code; storage axis = broker for bulk.** A broker
  escape-hatch (vend a scoped cred for large files) is an open option.
- **A space = a named prefix on a backend** (not its own bucket) — many spaces share infra.
- **code-fs ENFORCES, identity DECIDES:** code-fs owns only the space→backend *registry*; the
  space→user→permission *policy* lives in the **identity axis** (`Authorize(subject, action,
  resource=space)`), so it's swappable (OIDC claims / RBAC / LDAP) and granting access is an
  *identity* operation code-fs never sees. (Don't make code-fs an authz engine — scope creep.)
- **Permissions shape DISCOVERY, not just access:** "my spaces" is an authz-filtered list — the
  check happens at *discovery* (which spaces appear in the RatFS tree) AND at *access* (open/write).
- **Don't reinvent tenancy:** per-tenant isolation is automatic (C3); spaces are the finer,
  *shareable, per-user* layer **within** a tenant. Cross-tenant shared spaces = a separate,
  deliberate, harder feature.

**Dependencies:** the hub stamping the authenticated **subject** into the forwarded envelope (owed
follow-on, backlog #2 area) — per-user authz needs the subject to reach code-fs.

**Why additive (no rewrite):** spaces = key prefixes + a registry; authz = a new `requires` +
an Authorize call; scoped creds = existing storage vending; surface = already generic. Layers on
the current code-fs without touching contracts or RatFS. This is the multi-tenant SaaS code-platform
endgame the federation/hub work points at. Worth its own ADR when picked up.
