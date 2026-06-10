# Backlog — queued but not started

Work that's been identified (from reviews, ideas, conversations) but isn't actively in flight. **This isn't a wish list** — every entry should be specific enough that the next Claude session knows what "starting" it means.

When an item moves to active work, promote it: cut it from here, add it to [current.md](current.md), update [phases.md](phases.md) status if applicable.

> **Completed work is NOT kept here** — it moves to [`done.md`](done.md). This file holds only
> *live* queued work. (Cleaned 2026-06-08: the finished `rat serve` build, Phase-0 tail, `(rat.capability)`
> rollout, 0d/0e round-2, the prospective "ADRs to write" table, the Phase-0/1 engineering items, and the
> Q02 ① pre-unfreeze punch-list were all cut — they're done and recorded in `done.md`.)

---

## ② Multi-tenant availability cluster — gates any real multi-tenant use

Core-impl, **no wire change**. From the Q02 simulated panel ([reviews/Q02-tracker.md](../reviews/Q02-tracker.md); **AI personas — a real external review is still owed**). Roughly in order:

- **AV-1 (🔴 Critical; close FIRST — free now)** — `core/lease` `Store.Renew/Acquire` return `(ok, err)`; "renewal-error ≠ lease-lost" (hold leadership through transient backend errors until genuine local-TTL expiry; `state.PutOutcome.UNKNOWN` already models it on the wire). Add a test that injects an erroring `Renew`. A breaking refactor once a durable backend binds the `bool` interface — so do it *before* a durable lease backend lands.
- **AV-2 (High)** — map the already-frozen `LaunchSpec` `limits` → `--memory`/`--cpus`/`--pids-limit` in `podman.go` *and* `localprocess.go` (both drop them today); reject limit-less launches in multi-tenant mode; add a "limit exceeded → contained" vector.
- **AV-3 (High)** — bound the reconciler's runtime RPCs with per-call deadlines; give `Status()`/`Endpoint()` a read path that doesn't share the reconcile mutex (one hung `Healthcheck` pins all tenants + blinds Status today).
- **AV-4 (High)** — `Degraded` → capped-infinite-retry (cap the *interval*, not attempts) + a `Reset/Resume` path; **emit an event/metric on every state-transition edge**. Today it's a silent terminal black-hole (`reconciler_test.go:118` codifies it).
- **AV-5 (Med-High)** — add seccomp to `checkI9Minimum`: refuse `unconfined`/weaker-than-RuntimeDefault (the runtime should impose the max, not honor a caller-supplied weaker value).
- **AV-6 (High)** — Arrow ticket: per-producer key + `key_id`/rotation (mirror the conformance keyring) + a shared/durable single-use store (the lease's state-backend CAS) so restart/replica doesn't reopen replay; or bind tickets to a channel/cert fingerprint.
- **AV-7 (Low-Med)** — `noexec` on `/tmp`+`/data`; map `/data` to the plugin uid instead of `0o777`.

## ③ Tier-0 / observability / selection / discipline

- **T-1 (High)** — design + document the state-backend **degraded mode** (serve last-known-good reads / refuse only writes when the backend is unreachable; pair with AV-1) and **build the real state-backend read path** (the spike reconciler reads a *fixed* slice → the "always re-read state" guarantee is unexercised); specify the bootstrap-seat **recovery** leg (seat crash/restart + re-attach to running plugins).
- **O-1 (Med-High)** — emit a counter/event on every reconcile state-transition edge **now** (it's what AV-4's alert keys off), and pin the `/metrics` golden-signal list + an SLO doc while the core is still paper (this is the old `sre#8`).
- **O-2 (Med)** — pull **upgrade/version-skew** forward (partial upgrades are the *normal* case for a polyglot plugin mesh): a kubelet/apiserver-style N/N-1 policy + `rat preflight` orphan check + dual-advertise rollout; make desired-state git-first to bank half the DR win.
- **P-1 (Med)** — name the **plane/pipeline/binding desired-state language** (where provider *selection* happens) as a first-class contract artifact; document that capability negotiation resolves *eligibility* while plane bindings resolve *selection* (today the selection layer is unspecified + outside the contract triple).
- **K-2 (process)** — before each *real-backend* reference lands (Iceberg/Nessie/postgres), run an explicit **omission-audit** ("what loop can this backend complete that the in-memory refs faked?") — the freeze gate is structurally blind to omission.
- **D-1 (discipline)** — add an **enforcement-layer obligation count** to the temptation ledger (the gateway already performs 6–8 enforcement jobs on one hop while "the core stays six" stays literally true; the metric can't see enforcement-layer accretion — the K8s apiserver lesson).

## ④ Ecosystem on-ramp — some are cold-start-critical, can't wait for Phase 4

- **EC-1 (🔴 Critical-cold-start; P1)** — co-locate a real `plugin.yaml` in every `plugins/<axis>/<impl>/`, ship a **`plugins/README.md`** (promised by ADR-006 D2 / [ADR-038](../docs/architecture/adrs/038-reference-plugins-live-under-plugins.md), still missing — a doc-drift regression), and pull a thin `rat plugin validate <dir>` forward (the JSON Schema already exists). Today there's no walkable `git clone → running plugin` path. *(Now sharper post-restructure: the dir is `plugins/`, the README it promises still isn't there.)*
- **EC-2 (High)** — document a `rat dev` localhost-attach inner loop; reconcile the launch-metadata story (`image`/`command` as manifest field vs operator-config) with ADR-016 / ADR-022.
- **EC-3 (High; P1)** — build the conformance **issuance pipeline** (runner → signer → publish; `conformance.Sign` is test-only today) + a marketplace install-time attestation check; until then render marketplace `conformed_capabilities` as *unverified/self-declared*. *(Core-load D4 enforcement IS built — do not re-flag it.)*
- **EC-4 (High-GTM)** — name **one wedge axis** (`format`/`catalog` on the Iceberg/Delta tailwind) and write the first 3 third-party plugins *with* design partners; treat that as the product, not the 19th axis.
- **EC-5 (Med)** — publish a **governance commitment** before recruiting external authors: who may propose a `rat://` axis/capability, how a community plugin contests a first-party one, the trust-root model for conformance authorities (plural/federated?), and a contract + reference-marketplace relicense pledge.
- **EC-6 (Low)** — a "versioning for authors" CONTRACT.md section (two version axes + `compatible_core`); consider a manifest sidecar resolvable without re-imaging. Plus the architect F8 doc fix: regenerate `overview.md`'s manifest example from a real validating `contracts/examples/*.plugin.yaml`.

## ⑤ DX cluster — engineering residue of the 2026-06-10 frustration review

The review's doc-shaped findings were fixed the same day (`rat/6.14`, see [done.md](done.md)):
docs-truth sweep, QUICKSTART/CONTRIBUTING/AMENDING, the two guides, the project-socket
`rat call` fix, conformance failure output. These are the items that need real engineering.
Cross-refs: **EC-1/EC-2/EC-6** above already track manifest co-location, the dev inner
loop, and author versioning; **federation #5** tracks new-axis-without-recompile.

- ~~DX-1~~ — **done at `rat/6.15`**: `rat validate` + `rat up/serve --strict` (see [done.md](done.md)).
- **DX-2 (High; blocked on the publish decision)** — distribution: push the repo, cut binary releases (un-404 `scripts/install.sh`), publish `plugin-base-{go,py}` to ghcr, the Python SDK to PyPI, the Go SDK as a fetchable module. Until then **external plugin authors are structurally impossible** (the guides say so honestly).
- ~~DX-3~~ — **done at `rat/6.16`**: `rat capabilities [<axis>|<kind>]` (see [done.md](done.md)).
- ~~DX-4~~ — **done at `rat/6.16`**: vector envelope schema + key registry + `make validate-vectors` (in `verify`) + `harness_template.py` + `rat.vectors` helpers. Residual: a full harness *codegen* stays unqueued until a third+ reference per axis makes it pay.
- **DX-5 (Med)** — platform-bundle config dedup: one source of env facts (the Postgres DSN appears ~6×, the gateway address 4× across compose/plugins.yaml/profiles.yml/bff), or generate compose/plane entries from manifests.
- **DX-6 (Med)** — the secrets production story: a Vault/KMS reference secret-backend behind `rat://secret/v1/resolve` + credential rotation without a full-daemon restart (today: edit plugins.yaml `RAT_SECRETS` blob, restart everything).
- **DX-7 (Low-Med)** — `rat plugin dev` watch mode (rebuild + relaunch on change); until then the three-loop pattern in [the authoring guide](../docs/guides/authoring-a-plugin.md#iterating-fast) is the documented workflow.
- **DX-8 (Low)** — settle ADR-018 Q01 (Python codegen shape: buf-native vs the pinned standalone-protoc hybrid) — its failure mode reads as "my proto is wrong" when it's the toolchain image.
- **DX-9 (Low)** — `rat call` flag ergonomics: accept flags before the positional capability (`rat call --as x rat://…` fails today; the platform README shipped that broken order for weeks, which proves the trap).

---

## Remote access, federation & security (from the 2026-06-04 dogfooding session)

Built so far: `rat hub` + the security responsibility model + identity/TLS (ADRs [033](../docs/architecture/adrs/033-workspace-federation-hub.md)/[034](../docs/architecture/adrs/034-security-responsibility-model.md), see [done.md](done.md)). Queued refinements, roughly in order:

1. **Gateway-level identity for *direct* (non-hub) access** — today the per-plane gateway still trusts the wire `--as`; only the hub edge closes it (ADR-034 follow-on). Add the same guardrail to `rat serve` + subject-stamping onto the forwarded envelope.
2. **Transparent any-method proxy** (ADR-033 Q02) — forward `InvokeServerStream` (e.g. `state.Watch`) / `InvokeBidiStream` / `ControlService` through the hub via a passthrough codec, so watches + `rat status --workspace` + admin route through the one door. First cut is unary `Invoke` only.
3. **NATS-leaf federation** (ADR-033 Q01) — the cross-machine, outbound-only transport: each workspace daemon leaf-connects to the hub's NATS; route over `rat.<workspace>.invoke.<cap>`; per-workspace NATS accounts give identity/tenancy. Reuses the event-bus core thing; the real "fleet" + SaaS shape. Optional **`connectivity` axis** (reverse-tunnel/mesh/nats-leaf) as the pluggable reachability concern.
4. **Prior-art doc** — `docs/research/prior-art/remote-access-and-trust.md`: Tailscale/WireGuard, NATS leaf, Teleport, ngrok/Cloudflare Tunnel, SPIFFE/SPIRE, k8s konnectivity — the patterns + the rat mapping ("rat = Teleport-for-data-platforms").
5. **Dynamic descriptors** (the unlock, [`ideas/inbox.md`](../ideas/inbox.md)) — make the gateway learn axis protos from plugins at runtime instead of the hardcoded `routableDescriptors()`, so a **new axis becomes a pure plugin** with no core recompile — turns the deferred `fs` axis ([ADR-032](../docs/architecture/adrs/032-filesystem-axis.md)) into a clean plugin.
6. **Filesystem contribution auto-discovery** (the RatFS last mile) — the hub must forward **`ListPlugins`** (a slice of #2's transparent proxy) AND surface each plugin's **`contributes`** (an *additive* field on `core.v1.PluginStatus` — `make breaking`-clean, but a frozen-contract change → **its own ADR**). Then a surface lists plugins contributing to `rat://ui/v1/filesystem` and mounts each via the fs-capability it `provides`.

## Reconciler hosts operators ([ADR-036](../docs/architecture/adrs/036-reconciler-hosts-operators.md), Proposed/SKETCH)

Generalize the reconciler from one resource kind (plugins) to many via an `operator/v1` axis. Owes the ADR-003 axis obligations + the temptation-ledger verdict before it can move to Accepted. Not a 7th core thing (examined — see the ledger in `done.md`).

---

## GA additive enrichments (no wire break; land toward GA)

From the freeze + board reviews ([reviews/07](../reviews/07-freeze-review.md)/[08](../reviews/08-post-freeze-board-review.md)); all additive (new RPC/fields/enum values), none wire-breaking:

- **Spec polish (cheap):** **E5** `ERROR_MODEL.md` — add `CANCELLED`/`ABORTED` (streaming), pin `TableRef.branch` vs per-RPC `branch` precedence, pin BidiStream empty-first-frame abort. **E6** state engine output-type stability is the caller's responsibility (SUM→CAST). **E8** make C2/mTLS a deployment-conformance item + document the audit keyring trust-root/rotation + tail-drop detection. **F3** secret timing-equivalence note.
- **Message enrichments:** structured `IsolationAttestation` (D1); `WriteResult` insert/update/delete breakdown + `TableRef` snapshot_id/as_of (F2); `bound_capability` on `SubjectAssertion` (F1); `Event` signing (mirror the signed `AuditRecord`).
- **R3 catalog/stream niceties:** watch `caught-up` bookmark, `Event.schema_version`, `ArrowStream` termination signal, `MergeBranchResponse` no-op-vs-replay disambiguation.
- **Accepted v1 residuals (informational):** **R1** `SubjectAssertion` bound to the operation not the hop (bounded confused-deputy); **R2** storage `VendCredentials` tenant-scoping is a per-impl property (core can't inspect an STS blob); write-leg idempotency proven only against the fake (no real idempotent format ref yet); core audit-record signing + hash chain (C4/C8 GA, seeded by D4's ed25519).

---

## Q02 — real external human review (still owed)

The pre-unfreeze punch-list (PU-1..4 + 5a/5b/5c) is **complete** ([ADR-017](../docs/architecture/adrs/017-pre-unfreeze-contract-amendment-gate.md)), and a **simulated** 5-lens panel ran ([reviews/11-q02-*](../reviews/11-q02-architect.md)). The one remaining gate before the freeze leaves local/unpushed is a **real external human review** (min: architect + security). Set aside as impractical for a solo dev → validated practically instead (the data-dev-plane experiment, now graduated to `rat-data-dev`). The recruiting kit is archived in [`reviews/archive/`](../reviews/archive/).

---

## Claude Code config: deferred until proto-authoring patterns settle

| Item | What | Why deferred |
|---|---|---|
| Path-scoped proto/manifest rule | A new `.claude/rules/proto-contracts.md` with `paths: ["**/*.proto", "**/plugin.yaml"]` capturing proto conventions: field naming, message nesting, service naming for Go gRPC, `buf.yaml` layout, capability-URI format (ADR-002 D4). | The always-load `plugin-architecture.md` captures the invariants. This earns its place only once real proto-authoring style patterns emerge that don't belong in the always-load rule. Draft from real experience, then add the `paths:`-frontmatter file, then cut this entry → `done.md`. |

---

## Phase-4 hardening + GTM (deferred until the Phase-4 commitment, [phases.md](phases.md))

- **Engineering hardening:** upgrade safety + reversible state-schema migrations (see O-2); backup/restore + GitOps desired state; plugin publish + Sigstore signing (see EC-3); plugin deprecation governance (`compatible_core`, `rat plugin doctor`, N-1 skew); secret rotation contract.
- **GTM (non-engineering — needs the commitment decision):** reposition message (anti-lock-in / cost-ownership, not "extensible"); hand-pick 3-5 design partners; a public reproducible benchmark (the Polars pattern); a founder-led content/distribution motion; dbt→RAT + Airflow→RAT migration UX; commercial-path planning (managed cloud later).

---

## Ideas that may or may not become work

In [ideas/inbox.md](../ideas/inbox.md) — naming, plugin distribution patterns, manifest-from-proto generation, the runtime self-register idea, etc. Promote here if they sharpen into specific work.

---

## Maintenance

When new work is identified during a session: capture it here with enough specificity to be actionable, and note where it came from. Don't worry about ordering — that's done at promotion time.

When an item **starts**: cut it from here → add to [current.md](current.md) with the immediate next step → update [phases.md](phases.md) if a phase boundary moves.

When an item is **done**: cut it from here → record it in [done.md](done.md). **Don't leave completed items here.**

When an item is **dropped** (decided against / superseded): cut it → note the decision in the relevant ADR or `ideas/inbox.md`.
