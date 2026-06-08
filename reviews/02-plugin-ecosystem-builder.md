# 3rd-party plugin author review — RAT v3

*Reviewer role: a developer with no relationship to the RAT core team, trying to ship `rat-format-deltalake`, `rat-strategy-soft-delete`, or `rat-engine-clickhouse`.*

## Headline

Right now, a 3rd-party developer **cannot** build for RAT v3 — not because the model is bad, but because the entire plugin-author surface (the `.proto` contracts, the `plugin.yaml` JSON Schema, a scaffold, a contract-test kit, a local dev loop, a publish path) does not exist yet. The architecture is genuinely the *most* plugin-author-friendly data-platform design I've reviewed (open-set contracts, any-language, no blessed vendor), but RAT v3 has written down the *philosophy* of an ecosystem and almost none of the *machinery*. Every concrete artifact a plugin author touches is still a sketch. The single biggest risk is that RAT ships the core + first-party bundle, declares victory, and the 3rd-party surface stays "Phase 2+" forever — which is exactly how OSGi became "technically great, ecosystem stillborn" (the failure mode `competitors.md` line 78 names out loud).

## What's strong for plugin authors

These are real, and they're the reason the bet is worth making:

1. **Language freedom is a first-class commitment, not a footnote.** `ADR-001` Consequences ("Plugin authors can use any language. Contract is proto + manifest") + vision.md commitment #3 ("No language-specific SDKs that other languages must replicate"). A Rust author shipping `rat-format-deltalake` against the official `delta-rs` crate doesn't have to fight a Python-shaped SDK. This is strictly better than dbt (Python-only adapters) and on par with K8s/gRPC. **Huge** for a format/engine author whose best library lives in a specific language.

2. **No blessed vendor → a level playing field.** vision.md "What we're rejecting" is explicit: Nessie, S3, DuckDB ship in the *bundle*, not the *platform*. A 3rd-party `rat-catalog-lakekeeper` or `rat-engine-clickhouse` author is a *peer* of the first-party plugin, not a second-class "community connector." Contrast dbt, where `dbt-snowflake` is Labs-blessed and `dbt-mycustomwarehouse` is a maintenance orphan. This is the single most motivating property for an author deciding whether to invest.

3. **Capability-versioning is K8s-style major-only (`ADR-002` D4).** `rat://format-capability/v1/merge` with "backward-compat additions stay in same major; breaking changes = new major; multiple majors coexist." This is *exactly* the right call for plugin authors — no SemVer range-comparator hell (the npm `peerDependencies` swamp), no per-language version-parse drift. An author targets `v1` and knows precisely what "still works" means.

4. **`requires`/`provides`/`suggests`/`contributes` separation (overview.md §contract triple).** A `strategy` author declaring `requires: [format-capability: [scan, merge, append]]` rather than `requires: [format: iceberg]` means `rat-strategy-soft-delete` works on Iceberg *and* Delta *and* Hudi without the author naming any of them. This capability-not-implementation negotiation is the cleanest idea in the whole design and is genuinely better than VSCode's `extensionDependencies` (which name concrete extensions).

5. **The data plane bypasses the core (overview.md §communication, vision.md commitment #4).** A format/engine author isn't writing a plugin that proxies terabytes through a control-plane chokepoint. Bytes go engine↔storage direct. This means a plugin author's performance is bounded by *their* code, not RAT's RPC overhead — a critical trust signal for anyone shipping an engine.

## Journey breakdown — gap analysis

I walked the 9 stages as the author of **`rat-strategy-soft-delete`** (a SCD-adjacent strategy: mark rows deleted instead of removing them; needs `format-capability: [scan, merge]` + a `runtime`) and cross-checked against **`rat-format-deltalake`** (Rust, wraps `delta-rs`) where the gaps differ.

---

### Stage 1: Discovery — *how does the author learn RAT exists and that their idea fits an axis?*

- **Current state:** vision.md, overview.md, and `ADR-001` enumerate ~16–19 axes with one-line descriptions ("strategy — composes format + runtime ops"). `marketplace` is declared a plugin axis with a community plugin in the default bundle (`ADR-002` D9), but it's "Phase 2+" (D6) and has no UX (Q14, deferred).
- **Gap:** There is no canonical, authoritative **axis catalog** an author can read to confirm "soft-delete is a `strategy`, not a `format`." The taxonomy is described 3 times (vision, overview, ADR-001) with **inconsistencies the author will trip on**: the conversation doc itself admits "8 actually — undercount in the conversation" for control-plane axes; `audit-log` appears in overview/ADR-001 but the count says "16-19"; `ide-extension` is sometimes an axis, sometimes "a sub-kind." An author cannot tell whether the list is authoritative or illustrative.
- **Risk:** Authors guess the wrong axis, build `rat-format-soft-delete` when it should be a `strategy`, and the negotiation model silently fails to wire them up. Worse: two authors implement the same idea under different `kind:`s and neither is discoverable. Axis ambiguity is how you get the npm "left-pad" problem of fragmented micro-packages with no canonical home.
- **Recommended addition:** A single source-of-truth `docs/axes/` directory — one file per axis with: the precise responsibility boundary, the proto contract URI, "you are this axis if… / you are NOT this axis if…", and 2–3 canonical example plugins. Plus an **axis-decision flowchart** ("Does your plugin produce bytes on disk? → format. Does it compose format+runtime to write a snapshot? → strategy."). Make the count *exact* and version it.

---

### Stage 2: Onboarding — *where does the author read the contract for their axis?*

- **Current state:** **This is the single largest gap.** overview.md §contract-triple lists proto *filenames* (`strategy/v1/...`? — actually strategy isn't even in the proto example list; only engine/format/catalog/storage/identity/state/tenancy/ui are) and shows one illustrative `plugin.yaml`. `ADR-001` Phase 0 says "Lock the contract triple — proto files for every axis + `plugin.yaml` schema" as *future* work (next 1–2 months).
- **Gap:** **The proto files do not exist. The JSON Schema does not exist.** A `rat-strategy-soft-delete` author has *nothing to compile against*. There's no `strategy/v1/strategy.proto`, no `Write`/`Resolve` method signatures, no definition of what an Arrow batch handoff between strategy↔runtime↔format actually looks like on the wire. The one manifest example in overview.md (lines 113-141) is for `rat-strategy-scd2` and is the *closest thing to documentation* — but it's an illustration in an architecture doc, not a spec, and it references `format-capability` as a `kind` in `requires` while `provides` uses `kind: strategy` — the author can't tell if `format-capability` is a real registrable kind or shorthand.
- **Risk:** Zero plugins can be written until Phase 0 lands. And if Phase 0 ships protos *without* per-axis author-facing prose ("here's what `strategy.Write` must guarantee about idempotency, partial failure, branch isolation"), authors will reverse-engineer semantics from the first-party plugin's source — which makes the first-party impl the *de facto* spec and silently couples the ecosystem to its quirks (the Atom "the bundled package is the only real doc" trap).
- **Recommended addition:** Treat the contract as a **published, versioned product**: `contracts/strategy/v1/strategy.proto` + a prose `contracts/strategy/v1/CONTRACT.md` covering invariants, error taxonomy, idempotency/retry expectations (the inbox already worries about this for the event bus), and the data-plane handoff shape. Generate API reference docs from the protos. Without prose-alongside-proto, the protos are necessary but wildly insufficient — gRPC signatures don't tell you what `Write` must do when the catalog branch was deleted mid-run.

---

### Stage 3: Scaffolding — *is there a `cargo new`/`yo`/`cookiecutter` equivalent + a minimal example?*

- **Current state:** Nothing. No `rat plugin new`, no template repo, no `create-rat-plugin`. CLAUDE.md says "Reference plugin per axis as forcing functions for the contracts" — so reference plugins are *planned*, but as core-team forcing functions, not as author-facing starter kits.
- **Gap:** A `rat-format-deltalake` author starts from a blank directory. They must hand-assemble: a `plugin.yaml`, the gRPC server boilerplate that serves `format/v1`, the manifest-in-image packaging (`/manifest.yaml` or OCI label per `ADR-002` D6), a healthcheck endpoint (the reconciler calls it — overview.md §reconciliation "Verify healthcheck"), and the registration handshake (undefined — Q13 plugin-auth is deferred). That's a day of undocumented yak-shaving *before writing a single line of Delta logic*.
- **Risk:** The friction-to-first-plugin is the #1 predictor of ecosystem critical mass. Cargo's `cargo new` + crates.io is *why* Rust's ecosystem flourished; VSCode's `yo code` generator is why theirs did. RAT having "any language" freedom (the strength) makes scaffolding *harder*, not easier — "any language" means "no default project layout," so without per-language templates the language freedom becomes a cold-start tax.
- **Recommended addition:** Ship `rat plugin new --kind strategy --lang go` (and `--lang rust`, `--lang python`) producing a compiling, registering, healthcheck-passing no-op plugin wired to the contract. At minimum, a `plugins/` repo with one *trivial* but *complete* plugin per axis (the canonical `rat-format-noop`, `rat-strategy-passthrough`) that an author copies. The reference plugins the core team writes anyway should be **dual-purposed as templates from day one**, not internal-only.

---

### Stage 4: Development — *what's the dev loop? Can they iterate without redeploying the whole stack? Test against the contract?*

- **Current state:** Undefined. The solo bundle ("`chmod +x ./rat`", in-process plugins, overview.md §topologies) suggests a lightweight target, but there's no documented "run RAT with *my* plugin swapped in." `ADR-001` Consequence #3 promises `rat diagnose` "from day 1" — good instinct, but it's for *operators* debugging reconciliation, not *authors* iterating on a plugin.
- **Gap:** Several concrete unknowns for the author:
  - **In-process vs out-of-process during dev.** Solo runs plugins in-process (overview.md §topologies); but a 3rd-party plugin is a separate gRPC server. So even the solo author tests their plugin *out*-of-process while the bundle plugins are *in*-process. There's no described "dev mode: point the core at my locally-running `localhost:50051` strategy plugin." How does the registry learn about a not-yet-packaged plugin? (Manifest source is in-image per D6 — but during dev there's no image.)
  - **Hot-reload / iteration speed.** No story for "I changed my strategy, re-run the pipeline without restarting the core / rebuilding the image."
  - **Faking the rest of the platform.** To test `rat-strategy-soft-delete` an author needs a working engine + runtime + format + catalog + storage. Do they have to stand up the *whole* bundle? Is there a `rat-mock-*` set?
- **Risk:** If the dev loop requires building an OCI image and reinstalling for every change, iteration is minutes-per-cycle and authors leave. This is the Eclipse-plugin-churn failure: the dev experience was so heavy that only employed-to-do-it developers persisted.
- **Recommended addition:** An ADR for **plugin dev mode** — a `rat dev --plugin ./my-strategy` that registers a locally-running process via a manifest-on-disk override (D6 already allows operator override; extend it to a dev override), with file-watch reload. Ship `rat-mock-engine` / `rat-mock-format` / `rat-mock-catalog` so a strategy author can test *their axis in isolation* without the full stack. Document the minimal "1 plugin + mocks" loop explicitly.

---

### Stage 5: Validation — *contract-test suite? Linter for `plugin.yaml`?*

- **Current state:** `ADR-002` D3 chose standalone JSON Schema for the manifest specifically *because* "JSON Schema is built for that (IDE autocomplete, inline errors)." That's a strong foundation for manifest *linting*. But the schema itself doesn't exist yet, and there is **no contract conformance suite** mentioned anywhere.
- **Gap:** Two distinct missing tools:
  1. **Manifest linter** — "is my `plugin.yaml` valid for `kind: strategy`?" JSON Schema gives this *for free* once the schema ships, but there's no `rat plugin validate ./plugin.yaml` command and no per-kind schema (`ADR-002` D3 says "one schema for every plugin kind" — but a `strategy` and an `engine` have different `provides`/`requires` shapes; is it one mega-schema with `oneOf` on `kind`, or per-kind schemas? Undefined, and it matters enormously for author-facing error quality).
  2. **Behavioral conformance suite** — "does my `rat-format-deltalake` actually *behave* like a `format/v1`?" This is what made K8s CSI/CNI ecosystems trustworthy: a `csi-sanity` / conformance test an author runs to earn a "certified" claim. RAT has *nothing* here. Capability negotiation (`requires: format-capability: [merge]`) is a **lie waiting to happen** — a format can *declare* `merge` in its manifest and not actually implement it correctly, and nothing checks. The strategy author who trusts the declared capability gets a runtime explosion in *their* plugin's name.
- **Risk:** Without conformance tests, "capability" is an unenforced promise. The strategy↔format negotiation — the *cleanest idea in the design* — is only as good as the weakest format author's honesty. First bad plugin that declares a capability it fakes → users blame the strategy → strategy author rage-quits. This is precisely how trust collapses in a marketplace (the npm/PyPI supply-chain-and-quality erosion).
- **Recommended addition:** A `rat-conformance` harness per axis: feed a plugin the canonical test vectors for each capability it claims, assert behavior. Make "passes conformance for `format/v1` capabilities [scan, merge, append]" a **machine-checkable badge** the marketplace surfaces (ties to Stage 7). Ship the manifest linter as `rat plugin validate` and decide the per-kind-schema question explicitly. This is the highest-leverage missing tool after the contracts themselves.

---

### Stage 6: Distribution — *how do they publish? Signing? Versioning? Backward-compat checks?*

- **Current state:** Manifest-in-image (`ADR-002` D6); marketplace is an *aggregator* of in-image manifests, Phase 2+. Plugin signing is explicitly an **open thread** in the vision conversation (line 198: "Sigstore? Notary v2? In-marketplace review?") and `ADR-002` Q15 (sandboxing/signed images, deferred). Versioning of the *plugin* (vs the *capability*) is unspecified — `plugin.yaml` has `version: 0.3.0` (SemVer-shaped) but D4 only governs *capability* versioning.
- **Gap:**
  - **No publish path.** "Push an OCI image with a manifest label" is implied but never documented. To which registry? Is `ghcr.io/me/rat-format-deltalake` enough? How does the community marketplace plugin *find* it — topic tag (inbox suggests `rat-plugin` GitHub topic), or a submit step?
  - **No signing story → no trust on install.** The vision conversation flags this as load-bearing ("affects how aggressive we can be about the easy install story") but it's deferred to Q15. An author who *wants* to be trustworthy has no way to sign.
  - **No backward-compat check at publish.** `rat-format-deltalake 0.3.0 → 0.4.0` — did the author break a capability? Nothing checks. Cargo has `cargo-semver-checks`; RAT has the *concept* (D4 capability-major-versioning) but no tool enforcing that a plugin's bump respects it.
- **Risk:** Distribution-by-convention (just push an image, tag a repo) produces a discovery free-for-all and zero supply-chain trust. Enterprises (the paying topology in vision.md) **will not install unsigned 3rd-party plugins into their data plane** — so the most valuable users can't consume the ecosystem the author is building for. The author's incentive to publish collapses.
- **Recommended addition:** An ADR for **plugin distribution + supply chain** *before* Phase 2: define the publish command (`rat plugin publish`), the registry contract (OCI + a well-known manifest annotation), Sigstore/cosign signing as the default (not optional), and a `rat plugin compat-check old new` that fails on capability regressions. Pull Q15 forward — signing is a Stage-6 blocker, not a someday-security-nicety.

---

### Stage 7: Discovery-by-others — *marketplace UX, search, trust signals.*

- **Current state:** `kind: marketplace` is a plugin axis (`ADR-002` D9); community marketplace ships in the solo bundle; its UX is entirely deferred (Q14: "search by capability? trust badges? reviews? compatibility checking?"). inbox 2026-05-30 marketplace entry lists the right reference set (VSCode, crates.io, Helm Hub, OperatorHub).
- **Gap:** Everything about *how users find `rat-strategy-soft-delete`* is unwritten. Critically, the **compatibility question** — "does this plugin work on *my* deployment?" — is the hard one for a pluggable-everything platform and the one Q14 hand-waves. A user on the Delta+Glue+ClickHouse topology needs to know `rat-strategy-soft-delete` requires `format-capability: merge` *and that their format provides it*. That's a solvable query *because* of the capability model (the strength!) — but only if the marketplace plugin actually computes it. No one has committed to that.
- **Risk:** "Multiple competing marketplaces can coexist" (D9) sounds principled but is an **author tax**: do I publish to community-marketplace, enterprise-internal, *and* curated? Fragmented discovery = the author's plugin is findable in one place and invisible in others. And without a trust signal (downloads, conformance badge, signing, reviews), users default to first-party plugins and the 3rd-party long tail starves — the exact "adoption concentrated in the default bundle, nobody writes plugins" failure vision.md line 80 names.
- **Recommended addition:** Even pre-build, write the **marketplace contract** (the `marketplace/v1` proto): a plugin *advertises* `{id, kind, provided-capabilities, required-capabilities, conformance-results, signature}` in a standard shape, so *any* marketplace implementation can do capability-aware "works on your deployment?" filtering. Make conformance-badge + signature **mandatory listing fields**. Solve federated-publish ("publish once, syndicate to N marketplaces") so multi-marketplace isn't an author tax.

---

### Stage 8: Maintenance — *when core ships v2, how does the author know? Deprecation timeline? Compat shims?*

- **Current state:** D4 says majors coexist (`v1` and `v2` of a capability run side by side) — genuinely good. But there is **no deprecation policy, no support-window commitment, no breaking-change communication channel**. `ADR-001` says "core releases are infrequent and small" (Consequence) and "the 16-axis taxonomy *will* evolve… expect 2-3 axis-level changes per year" (Neutral) — so axes *will* be merged/split/renamed, which is a far more violent break for an author than a capability major-bump.
- **Gap:** If `strategy` and `runtime` get merged in a future taxonomy revision (the doc explicitly anticipates axis churn), what happens to `rat-strategy-soft-delete`? There's no:
  - **Deprecation timeline** ("`format/v1` supported until date X; `v2` available from Y; overlap window Z").
  - **Notification channel** for authors (how does the `delta-rs`-wrapping author *learn* `format/v2` shipped? RSS? a `rat plugin outdated` command? email?).
  - **Compat-shim policy** (does core provide a `v1→v2` adapter, or is each author on their own?).
- **Risk:** Axis-level churn (2-3/year, by their own estimate) with no deprecation discipline is **exactly the Eclipse/Atom death spiral**: the platform evolves, plugins silently break, authors who aren't paid to maintain them disappear, users find a graveyard of "last updated 2 years ago, incompatible" plugins, and the marketplace becomes a trust desert. K8s succeeds here *because* it has a rigid deprecation policy (N releases, documented, API-review-gated). RAT promising K8s-style versioning (D4) without K8s-style *deprecation governance* takes the upside and skips the discipline.
- **Recommended addition:** A **plugin compatibility & deprecation ADR**: a hard policy (e.g., "a capability major is supported ≥18 months after its successor ships; axis renames ship with a core-provided alias for ≥2 releases"), a machine-readable `compatible_core: rat/1` field in the manifest (the example has `api_version: rat/1` — formalize it as a *checked* compatibility gate, like VSCode's `engines.vscode`), and a `rat plugin doctor` that tells an author "your plugin targets a capability deprecated in core 1.7, EOL 1.9."

---

### Stage 9: Support — *where do plugin users go for help? Who answers? Who owns a break?*

- **Current state:** Completely unaddressed. No CONTRIBUTING, no plugin-author forum/Discord, no issue-routing model, no "first-party vs 3rd-party support boundary."
- **Gap:** The pluggable-everything model creates a **brutal support-attribution problem**. A user runs `rat-strategy-soft-delete` on `rat-format-deltalake` with `rat-catalog-glue` and it fails. Whose bug? The strategy author's? The format author's (faked a capability)? The core's reconciler? `ADR-001` Consequence #3 *names* this ("'Why didn't my pipeline run?' can have answers across many plugins + the core's reconciler") and offers `rat diagnose` — but diagnosis tooling locates the *failing component*, it doesn't answer *who is responsible for fixing it* or *where the user files the issue*. The 3rd-party author will get blamed for failures in capabilities they merely *consumed*.
- **Risk:** Without a support model, every cross-plugin failure lands in the most-visible plugin's issue tracker. Authors of popular *consuming* plugins (strategies, UIs) become unpaid front-line support for *every other plugin's* bugs. That's the fastest way to burn out exactly the high-value authors you most need. Support burden, not technical difficulty, is what killed most of the Atom package ecosystem's long tail.
- **Recommended addition:** A **support & responsibility model** doc: (a) a blame-attribution protocol baked into `rat diagnose` ("the failure originated in `rat-format-deltalake.Write`, capability `merge`; file at <plugin's declared issues URL>") — add a required `support_url` / `issues_url` to `plugin.yaml`; (b) a community channel for authors with core-team presence (Discord/Discourse), because authors evaluating the ecosystem look for "is anyone home?"; (c) an explicit, published **support boundary** ("core supports the contract; authors support their plugins; here's how cross-plugin issues get triaged").

---

## Top 5 things missing

Ranked by how lethal each is to reaching ecosystem critical mass:

1. **The contracts themselves don't exist (Stage 2).** No `.proto` files, no manifest JSON Schema, no per-axis prose contract. Nothing else on this list can start until these land *with author-facing semantics docs*, not just wire signatures. This is the gate on the entire ecosystem. Phase 0 must produce *publishable, documented* contracts — and the author-facing prose is as load-bearing as the proto.

2. **No contract conformance suite (Stage 5).** Capability negotiation is the best idea in the design *and* an unenforced honor system. Without `rat-conformance`, a `format` can lie about providing `merge`, and the *strategy* author who trusted it gets blamed. A capability you can't verify is marketing, not a contract. This single tool is what separates "K8s CSI ecosystem" (trusted, certified) from "OSGi ecosystem" (technically capable, never trusted).

3. **No scaffold + no local dev loop (Stages 3 & 4).** Friction-to-first-plugin is the #1 critical-mass predictor. "Any language" makes cold-start *harder* without per-language templates. If iterating means rebuild-image-reinstall, only paid-to-do-it developers persist. Need `rat plugin new` + `rat dev --plugin` + mock plugins for isolated-axis testing.

4. **No distribution + signing path (Stage 6).** Enterprises — the paying topology — won't install unsigned plugins into their data plane. Deferring signing to Q15 means the most valuable plugin *consumers* can't safely consume, which destroys the author's reason to publish. Pull supply-chain forward; make signing default, not optional.

5. **No deprecation/compat governance (Stage 8).** Self-estimated 2-3 axis-level changes/year with no deprecation policy, notification channel, or compat-shim commitment is the Eclipse/Atom death spiral by construction. K8s-style versioning (D4) without K8s-style deprecation *discipline* takes the upside and skips the part that actually keeps authors alive.

*(Honorable mention — the support/attribution problem (Stage 9) — is a slow-burn version-2 killer rather than a critical-mass blocker, so it sits just below the line.)*

## Top 3 things they got right

1. **Capability-not-implementation negotiation.** `requires: format-capability: [scan, merge, append]` instead of `requires: format: iceberg` is *better than VSCode's `extensionDependencies`* (which name concrete extensions) and better than dbt (where adapters are warehouse-specific). It's the mechanism that lets one `rat-strategy-soft-delete` work across every format without the author knowing any of them. If conformance-tested (Stage 5), this is a genuine moat.

2. **Major-version-only capability versioning (D4).** Refusing SemVer-range semantics is the correct, hard-won lesson from npm's `peerDependencies` swamp and the cross-language version-parse drift that plagues polyglot ecosystems. "Target `v1`, multiple majors coexist, additions don't break you" is exactly the contract an author wants. It's the single most author-friendly *decision already locked*.

3. **Language-agnostic-by-contract + no blessed vendor.** The combination (vision.md commitments #1 and #3, the "What we're rejecting" list) means a 3rd-party author is a *true peer* of the first-party plugin — same contract, same standing, best-library-wins. dbt, VSCode (TS-privileged), and every vertically-integrated platform fail this. It's the strongest *motivational* property in the whole design — authors build where they're first-class.

## Compared to peers

| Dimension | RAT v3 (today) | dbt adapters | VSCode extensions | Cargo crates | K8s operators / CSI |
|---|---|---|---|---|---|
| **Contract published** | ❌ none yet (Phase 0) | ✅ `dbt-core` adapter base class | ✅ `vscode.d.ts` + API docs | ✅ stdlib + std traits | ✅ proto + CRD + conformance |
| **Language freedom** | ✅✅ any (proto+manifest) | ❌ Python only | ⚠️ JS/TS privileged | ❌ Rust only | ✅ any (gRPC) |
| **Scaffold tool** | ❌ none | ⚠️ copy a repo | ✅ `yo code` | ✅ `cargo new` | ⚠️ `operator-sdk`/`kubebuilder` |
| **Local dev loop** | ❌ undefined | ✅ `dbt run` locally | ✅✅ F5 Extension Host | ✅✅ `cargo run`/`test` | ⚠️ kind/minikube, heavyish |
| **Conformance suite** | ❌ none | ⚠️ adapter test macros | ⚠️ partial | ✅ `cargo test` + `semver-checks` | ✅✅ csi-sanity / e2e conformance |
| **Manifest lint** | ❌ (schema TBD) | n/a | ✅ marketplace validation | ✅ `cargo` validates | ✅ CRD/OpenAPI validation |
| **Publish + signing** | ❌ none / deferred (Q15) | ✅ PyPI (unsigned norm) | ✅ marketplace + signing | ✅✅ crates.io | ✅ OCI + cosign norm |
| **Discovery/marketplace** | ⚠️ axis exists, UX deferred | ✅ dbt Hub / packages | ✅✅ Marketplace | ✅✅ crates.io | ✅ OperatorHub / ArtifactHub |
| **Capability negotiation** | ✅✅ best-in-class (if enforced) | ❌ warehouse-coupled | ⚠️ names concrete deps | ⚠️ feature flags | ⚠️ implicit via API groups |
| **Versioning model** | ✅✅ K8s major-only | ⚠️ adapter SemVer | ✅ `engines.vscode` | ⚠️ SemVer ranges | ✅✅ API groups + deprecation policy |
| **Deprecation governance** | ❌ none | ⚠️ ad hoc | ✅ documented | ✅ editions/RFC | ✅✅ rigid, gated |
| **Support model** | ❌ none | ✅ Slack + Labs | ✅ GH + forum | ✅ users.rust-lang | ✅ SIG structure |

**Read of the table:** RAT v3's *design columns* (language freedom, capability negotiation, versioning model) are **best-in-class — frequently ✅✅**. Its *machinery columns* (scaffold, dev loop, conformance, publish, support) are **uniformly ❌, because none of it is built yet**. That's the whole story: RAT has designed the most author-friendly *contract* in the data space and built none of the *tooling* that turns a good contract into a living ecosystem. The gap between the two columns is the work. Cargo and VSCode didn't win on contract elegance — they won on `cargo new`/`F5` and crates.io/Marketplace. RAT has the elegance; it has not yet decided that the *tooling* is a P0 deliverable rather than a Phase-2 afterthought. Make that decision, and the design earns its bet.
