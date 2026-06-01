# Board review — Architectural coherence (post-freeze)

> Reviewer: `architect`. Lens: architectural coherence after the `rat/1…rat/1.4`
> freeze (18 axes, 32 references, cross-axis composition). Tags: **[V2-REGRET]**
> frozen wire/shape flaw needing a v2; **[ADDITIVE]** fixable without breaking;
> **[PROCESS]** docs/discipline. Evidence cited `file:line`. Prior reviews 00–07
> not repeated unless the freeze changed the picture.

## Headline

The freeze is, on the **wire**, genuinely regret-light: I found **no clean
[V2-REGRET]** — every structural gap below is patchable additively or is a
docs/marketing-honesty issue. That is a real achievement of the two-reference +
composition gate. But composition did **reveal hidden coupling the capability
model was supposed to hide**, and the answer to "swap anything, code unchanged"
is *true for the strategy axis, true-with-asterisks for everything else.* The
asterisks are concentrated in the **engine↔format↔catalog data-plane seam**, and
two of them sit *between* already-frozen axes — frozen endpoints, un-frozen span.

---

## Findings (ranked)

### 1. [ADDITIVE — interop-risky] The engine's input-binding choreography is unspecified in frozen `v1`
**Severity: HIGH.** `QueryRequest.tables` is `repeated TableRef`
([engine.proto:61](../../contracts/proto/rat/engine/v1/engine.proto#L61)), and the
engine CONTRACT specifies only how an engine *returns* results (producer-hosted
Flight, [CONTRACT.md:66-67](../../contracts/proto/rat/engine/v1/CONTRACT.md#L66)) —
**not how it resolves and binds its inputs.** Composition itself surfaced that "the
engine references ignored `QueryRequest.tables`" and the test had to *force* the
intended behaviour — resolve each ref via `format.scan`, bind it, stream over real
Flight ([examples/composition/README.md:80-84](../composition/README.md#L80)). So the
single most important engine interaction (reading inputs from a format) lives as
*composition convention*, with no normative text and no conformance vector in the
frozen contract. Two conformant engines can legitimately disagree on whether
`tables` arrive pre-resolved or must be `format.scan`-ed — an interop hazard baked
into `v1`.
**Escalation risk toward [V2-REGRET]:** `TableRef`
([data.proto:17-24](../../contracts/proto/rat/common/v1/data.proto#L17)) carries no
*format discriminator*. When a query mixes two formats (Iceberg source + Delta
sink), the engine must pick the right `format/v1/scan` provider per ref — but the
invoke gateway resolves `capability→provider` and **multiple** formats provide
`scan`. Nothing in the frozen `TableRef` says which. Additive today (add a field);
regret-prone if real multi-format pipelines ship before it's addressed.
**Recommendation:** before any multi-format use, pin the engine input-binding
choreography in engine CONTRACT.md + a conformance vector, and decide the
per-`TableRef` format-provider selector (additive field) *now* while it's free.

### 2. [ADDITIVE] The format→catalog commit-linkage — the middle of the write→register→merge loop — is deferred between two frozen axes
**Severity: HIGH.** `format.Write` produces a snapshot and returns a
`WriteResult.snapshot_id`
([data.proto:74-84](../../contracts/proto/rat/common/v1/data.proto#L74)); the catalog
owns branches and gates publishes via `MergeBranch`
([catalog.proto:54](../../contracts/proto/rat/catalog/v1/catalog.proto#L54)). But
**how a freshly-written snapshot gets registered into the catalog is un-contracted** —
catalog.proto itself flags it: "the separate gap — how the catalog learns what
format.Write put on a branch — is the additive commit-linkage RPC, GA-deferred"
([catalog.proto:27-30](../../contracts/proto/rat/catalog/v1/catalog.proto#L27)). Worse,
there is **no create-table RPC at all** — composition had to register source+target
*out-of-band* (R3, [composition/README.md:86-90](../composition/README.md#L86),
[ADR-009:86](../../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md#L86)).
Net: the canonical pipeline loop (create → write → **register** → merge) has its two
load-bearing middle steps living *outside* the frozen contracts, even though both
endpoints (format, catalog) are frozen. It's additive (new RPCs), so not a wire
break — but it means "the data-plane is frozen" overstates what a plugin author can
actually build against today.
**Recommendation:** prioritise the commit-linkage + create-table RPCs as the first
post-freeze additive work; until they land, document in ADR-009/backlog that the
write→register path is admin/out-of-band.
**Cross-consult amplification (`sre`):** this same seam also carries the
reconciler's *atomicity* story. The publish step is crash-safe — `MergeBranch` has
`idempotency_key` + `expected_into_snapshot` CAS
([catalog.proto:92-100](../../contracts/proto/rat/catalog/v1/catalog.proto#L92)) — but
the *write* step has no idempotency key (`strategy.Apply`,
[strategy.proto:31-42](../../contracts/proto/rat/strategy/v1/strategy.proto#L31);
`WriteResult`, [data.proto:74-84](../../contracts/proto/rat/common/v1/data.proto#L74)).
Convergence under at-least-once therefore rests **entirely on the v2 branch-isolation
convention** (write to a run branch; merge is the only publish gate) — yet
`strategy.Apply` takes a target `TableRef` whose `branch` is *optional*
([data.proto:23](../../contracts/proto/rat/common/v1/data.proto#L23)), so the frozen
contract **does not mandate** run-branch isolation. A strategy that writes directly
to main has no atomicity and a crash leaves a visible half-write. The freeze locked a
saga whose saga-primitive (branch isolation) is convention, not contract.

### 3. [PROCESS] "Swap the engine, code unchanged" is false for user SQL — the SUM-type divergence pushes portability onto the user
**Severity: MEDIUM.** Composition found DuckDB's `SUM(int)`→128-bit decimal vs
DataFusion's→`int64`; the fix was to pin `CAST(SUM(amount) AS BIGINT)` in the golden
SQL ([composition/README.md:73-79](../composition/README.md#L73)). That means an
engine substitution **does** change result schema unless the *user* pre-CASTs — the
engine axis guarantees no portable result type system. This is arguably inherent
(engines bring their own SQL semantics; cross-engine federation is explicitly plugin
territory, [overview.md:244](../../docs/architecture/overview.md#L244)), but it
directly contradicts the "swap anything, code unchanged" framing. The composition
proves the *strategy* is invariant under substitution — it does **not** prove user
pipelines are.
**Recommendation:** state honestly in overview.md that engine-portability of result
*schemas* is a user-SQL responsibility (or require engines to declare a type-mapping
as a conformance obligation). Don't let "code unchanged" imply user SQL is portable.

### 4. [PROCESS] Tier-0 is under-marketed in overview.md vs the rule that admits it
**Severity: MEDIUM.** `plugin-architecture.md` honestly names a "tier 0"
(state-backend + deployment-runtime + embedded bus are bootstrap-selected, *not*
hot-swappable). But overview.md's main narrative doesn't carry that caveat: the
deployment-topology table presents state/auth/deploy/storage/engine as freely
swapped per-topology ([overview.md:193-202](../../docs/architecture/overview.md#L193))
under "same core, different plugin sets," and "The core itself doesn't change between
scales" ([overview.md:210](../../docs/architecture/overview.md#L210)) — with no
tier-0 footnote. A reader of the architecture's own front-door doc would believe all
axes are equivalently swappable at runtime. The DynamoDB footnote
([overview.md:200](../../docs/architecture/overview.md#L200)) shows the team *can* be
honest about backend constraints; tier-0 deserves the same treatment in the headline
doc, not only in the rules file.
**Recommendation:** add a tier-0 callout to overview.md §"The six core things" /
§"Deployment topologies."

### 5. [PROCESS] The reconciler spec names a non-existent "plane-manager-plugin" and contradicts its own "core never commands" claim
**Severity: MEDIUM.** overview.md:176 has the reconciler "ask plane-manager-plugin
to spawn missing axes" ([overview.md:176](../../docs/architecture/overview.md#L176)) —
but **no `plane-manager` axis exists** in the 18-axis list; it's actually
`deployment-runtime` + the reconciler's plane lifecycle
([ADR-001:131](../../docs/architecture/adrs/001-everything-is-a-plugin.md#L131)). As
written it reads like a hidden 19th axis / smuggled core responsibility. Same passage
also dents the "**The core never tells anyone to do anything**"
([overview.md:189](../../docs/architecture/overview.md#L189)) thesis: "ask … to spawn"
is imperative command, not declarative convergence. Small, but it's the load-bearing
reconciler narrative and it's internally inconsistent.
**Recommendation:** rename to `deployment-runtime`; reconcile the wording (the
reconciler writes *desired plane state*; deployment-runtime reacts) so the
declarative claim holds.

### 6. [PROCESS] Six-thing-core discipline HELD — but the mandated temptation counter is not actually kept
**Severity: LOW (positive + gap).** The discipline genuinely held through the build:
the capability-invoke gateway was kept **generic** specifically to avoid a 7th core
thing ("no per-axis core knowledge → no 7th core thing",
[roadmap/done.md:631](../../roadmap/done.md#L631);
[ADR-005:47](../../docs/architecture/adrs/005-capability-invocation-model.md#L47)
"Temptation count unchanged"). No "plugin" I traced is secretly core. **But** CLAUDE.md
principle #2 and `done.md` mandate *tracking the temptation count* as a drift
indicator, and there is **no counter anywhere** — `grep -rni temptation roadmap/`
finds only prose, never a running tally. The discipline is observed ad hoc, not
measured. That's the one place the architecture's own meta-rule isn't being followed.
**Recommendation:** add an explicit temptation ledger to `roadmap/done.md` (date,
what was tempted, why it stayed a plugin), even if the count is currently 0.

### 7. [PROCESS — positive] The strategy axis is the clean capability showcase as advertised
**Severity: LOW (confirmation).** Confirmed: `StrategyService.Apply` couples to peers
**only by capability** — it `requires` format-capabilities + a runtime and reaches
them through the core invoke gateway, naming no plugin
([strategy.proto:20-28](../../contracts/proto/rat/strategy/v1/strategy.proto#L20)), and
composition shows the *identical* strategy code across all four substitutions
([composition/README.md:36-40](../composition/README.md#L36)). This is real and it's
the design's best moment. **Honest caveat:** the strategy is this clean partly because
it *offloads* the format-coupling down a layer — the engine absorbs the
`format.scan`+Flight binding (Finding 1). The showcase is genuine; it isn't free.

### 8. [PROCESS] The freeze locks the wire but leaves three trust points to unconformanced prose
**Severity: MEDIUM (cross-cutting; surfaced with `security`).** Three of the
architecture's load-bearing trust guarantees are **honor-system relative to the frozen
wire**, and they form a coherent *set*, not three unrelated footnotes:
- **C2 transport-auth** — the two unsigned principals (`caller_plugin`, `tenant`) rest
  on mTLS/per-plugin-token, stated in prose only and *structurally inexpressible in
  proto3* ([context.proto:148-153](../../contracts/proto/rat/common/v1/context.proto#L148)).
- **ArrowStream ticket** — the only gate on the core-bypassing bytes leg; "short-TTL,
  single-use, bound to {caller_plugin,tenant,stream}" is a MUST in a comment, not a
  conformance vector ([data.proto:57-63](../../contracts/proto/rat/common/v1/data.proto#L57)).
- **Storage `VendCredentials` tenant-scoping** — R2, an explicit per-impl honor-system
  property the core can't inspect
  ([ADR-009:84-85](../../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md#L84)).
Two of the three sit on the core-bypassing bytes leg, where the core has *no* visibility
to enforce. The coherence point: `v1` froze the **wire shape** thoroughly but left
**trust enforcement** to prose the conformance suite doesn't exercise. None is a wire
flaw (so none is [V2-REGRET]) — but "the data plane is frozen and validated" implies a
security posture the conformance coverage doesn't actually back.
**Recommendation:** treat the three as one workstream — add conformance vectors that
*test refusal* (unauthenticated channel rejected; replayed/expired ticket rejected;
cross-tenant cred-vend rejected). Additive, no wire touch; converts prose MUSTs into
tested obligations. (`security` is carrying the per-item fixes; this is the
architectural framing.)

---

## Cross-consults
- **`contracts`** asked: any frozen proto that structurally forces a *specific peer
  plugin* (vs an abstract capability)? My own read: **no hard peer-naming** —
  coupling is by capability URI throughout. The real exposure is *under*-specification
  (Finding 1: the engine↔format binding choreography + `TableRef` format-selector are
  too loose, not too tight). Awaiting their reply; will defer to contracts on proto
  specifics if it changes this.
- **`sre`** asked: is the reconciler runnable as specced? My contribution: the
  `plane-manager-plugin` reference (Finding 5) is a doc artifact for
  `deployment-runtime`; flagging it so sre doesn't model a missing axis. Awaiting reply
  on leader-election/optimistic-concurrency runnability.
- Open to `security`/`sre` questions on the tier-0 trust model (state-backend +
  deployment-runtime + embedded bus are authenticated-transport-dependent per
  `plugin-architecture.md`'s cross-cutting list; the bytes-leg ticket in
  [data.proto:57-63](../../contracts/proto/rat/common/v1/data.proto#L57) is the only
  gate once data bypasses the core).

---

**Biggest concern:** the format↔catalog **commit-linkage** (Finding 2) — the
freeze locked both ends of the write→register→merge loop while its load-bearing
middle (snapshot registration + create-table) sits deferred outside the contract, so
"the data plane is frozen" promises more than a plugin author can actually build today.
