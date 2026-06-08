# Board review — Plugin-Author Experience & Ecosystem Honesty (post-freeze)

> Lens: pretend I'm a 3rd-party dev writing a RAT plugin **tomorrow**, from the frozen
> contracts + CONTRACT.md + ERROR_MODEL + a reference, with **no insider help**.
> Tags: `[V2-REGRET]` (wire/shape we'll regret), `[ADDITIVE]` (safe to fix post-freeze),
> `[PROCESS]` (honesty / representation, not a wire bug).
> Evidence is `file:line`. Reviewer: `ecosystem`. Date: 2026-06-01.

Bottom line up front: the **wire contracts are genuinely implementable** and the per-axis
conformance harness is real and good. The honesty problem is one layer up: the artifacts a
3rd-party actually *publishes* (the manifest's `provides`, the marketplace listing's
`conformed_capabilities`) are **self-asserted**, and the component that was promised to check
them — the core — **does not exist yet**. So "declared == conformed" is currently a slogan, not
a mechanism.

---

## Findings (ranked)

### 1. [HIGH] [PROCESS] Conformance is honour-system at the layer that ships — a plugin can lie in its listing and nothing catches it
`marketplace.proto:45-47` makes `conformed_capabilities` a MANDATORY listing field and the proto
comment claims it is "which capabilities have passed their axis golden-data suite (C6)". But it is
a bare `repeated string` the publisher fills in by hand. The reference proves the gap rather than
closing it: `plugins/marketplace/community-py/store.py:45-49,87` simply **hardcodes
`conformed_capabilities = provided_capabilities`**. A `grep` for any linkage between the real
conformance harness's PASS result and either the manifest or the listing returns **nothing** —
there is no signed conformance attestation, no machine artifact a marketplace can verify. A
malicious or sloppy author writes `conformed = [everything]` and the frozen contract has no
defense; the only stated mitigation is "empty set means unverified and the UI must surface that"
(`marketplace.proto:46`) — which does nothing about a *false non-empty* claim.
**Recommendation:** define a **conformance attestation** (signed harness output: axis + vector
hash + pass result + signer) that the marketplace verifies, and make `conformed_capabilities`
*derived from* it, not free text. Additive to the proto (new message), so it can land post-freeze —
but the *honesty story* should be written down now so authors aren't told a self-asserted field is
"verified."

### 2. [HIGH] [PROCESS] The core doesn't exist, yet frozen contracts describe its checks in the present tense — an author will believe gates run that don't
`plugin.v1.json:88` calls `compatible_core` "A CHECKED compatibility gate (like VSCode
engines.vscode), **not advisory**." `plugin.v1.json:98` says `provides` is "what the gateway
enforces at runtime (C5)." Both describe a running core. There is **no core** — no `core/`,
`cmd/`, `src/`, or `*.rs` outside `contracts/`+`plugins/` (verified), and the only invoke-gateway
is explicitly a `// THROWAWAY STUB` (`plugins/state/inmemory-go/gateway_test.go:1-7`). `roadmap/current.md`
is honest internally ("OR start Phase 1 (the core)"), but a 3rd-party reading only the frozen
schema + CONTRACT.md cannot tell **designed** from **working**. They'll assume C2/C5/C7 enforcement,
capability routing, and `compatible_core` checking exist.
**Recommendation:** add a one-line status banner to `plugin.v1.json`'s description and each
CONTRACT.md: "Enforcement described here is the contract the core MUST implement; the core is not
yet built (Phase 1)." Cheap, and it's the difference between a spec and a promise.

### 3. [HIGH] [V2-REGRET-adjacent] The manifest schema is the ONE thing the author hand-writes — and it's the ONE artifact still `v1-preview`
All 18 axes froze (`rat/1`→`rat/1.4`) but `roadmap/current.md` confirms "**the ONLY remaining
`v1-preview` artifact is the manifest schema**." The capability URIs an author drops into
`provides`/`requires` are frozen, but the **envelope around them can still change**
(`schema/README.md:63-67`: "Until the rat/1 freeze… edit freely"). Worse, the schema is
deliberately the *envelope only* — it cannot validate that "a `kind: engine` provides
`rat://engine/v1/scan`" because the **per-kind schemas don't exist** (`schema/README.md:25-40`,
deferred to "0b as each axis proto lands" — which has happened, but the per-kind layer didn't).
So the author's single deliverable can't be fully validated *and* isn't frozen. They cannot
"finalize a manifest" with confidence today.
**Recommendation:** freeze `plugin.v1.json` now (the team's stated next step) **and** ship the 18
per-kind schemas in the same stroke — the protos are frozen, so the required-capability sets are
known and derivable. Without per-kind schemas, the most basic author mistake (wrong/missing
required capability for the kind) ships silently.

### 4. [MED] [PROCESS] Only 6 of 18 axes have an author guide; the other 12 authors are on their own
`conformance/README.md:57` states plainly: "(The control/experience axes get their `CONTRACT.md`
when they're referenced.)" CONTRACT.md exists for state, engine, format, storage, runtime, catalog
only. An author writing an **identity, tenancy, scheduler, billing, secret, observability,
auditlog, ui, notifications, deployment-runtime, or marketplace** plugin gets a `.proto` + one
in-memory reference and no prose guide to the conformance obligations, the error-model specifics,
or the gotchas. Combined with **no `rat plugin validate` CLI** (`INVALID-examples.md:3-6`: "Once
the conformance/validation tooling exists (sub-phase 0f)…" — it doesn't), the negative-test corpus
is aspirational Markdown, not a runnable check.
**Recommendation:** before declaring the ecosystem "open for authors," every frozen axis needs a
CONTRACT.md (they're frozen — the guides can't drift). Ship `rat plugin validate` against the
INVALID-examples corpus so the cardinal sin (#4, coupling to an impl name) is actually caught.

### 5. [MED] [V2-REGRET] The round-1 reference plugins encode stand-ins a beginner will copy wrongly
The 32 refs are two-tier: round-1 "wire" toys + round-2 "real" backends. The toys teach the wrong
data-plane shape. `conformance/README.md:110-114` and `format/v1/CONTRACT.md:77` admit the
in-memory refs carry the bulk leg as an "**in-process row registry rather than a typed Arrow
stream**." `composition/README.md:80-84` is the smoking gun: the engine refs "**ignored
`QueryRequest.tables` and carried results on an in-process stand-in incompatible with the format's
real Flight**" — the *intended* behavior only appeared when composition forced it. A 3rd-party who
starts from `plugins/engine/inmemory-py` (the natural starting template) learns a data path that
**does not interoperate**. The real refs (parquet-py, duckdb-py) exist, but nothing flags "start
here, not there."
**Recommendation:** label round-1 refs `EXAMPLE — WIRE CONTRACT ONLY, NOT A STARTER TEMPLATE` in
their README headers and point authors at the real ref as the copy-from target. Stand-ins are fine
as conformance scaffolding; they're dangerous as the first thing a newcomer reads.

### 6. [MED] [PROCESS] Cross-axis code-consistency of the error model rests on a vector existing for each error
`ERROR_MODEL.md` is a strong artifact and resolves M1/M2 well — the two-layer rule (status vs
domain-outcome field) and the not-found rule are clearly stated. But the doc itself concedes the
enforcement is "gated by golden vectors, not a comment" only *where a vector exists*. Several
mappings are explicitly **reserved / unexercised**: `ALREADY_EXISTS` ("reserved — not yet exercised
by a vector", `ERROR_MODEL.md:47`), and control-plane axes get "one reference + conformance"
(lighter than data-plane's two). So two impls of, say, `identity` could disagree on
`PERMISSION_DENIED` vs `UNAUTHENTICATED` for the same failure and both "conform," because no second
impl and no error vector pins it. The rule is right; the *coverage* that makes it binding is thin
on 12 axes.
**Recommendation:** track which (axis, error-class) pairs have a golden vector vs are prose-only;
the prose-only ones are honour-system and should be labeled as such in each CONTRACT.md.

### 7. [MED] The marketplace "works on my deployment?" filter answers a narrower question than it claims
`store.py:114` implements the filter as a single set-subset test: `required_capabilities ⊆
deployment_capabilities`. That's necessary but **not sufficient** for "works on my deployment." It
does **not** check: `compatible_core` major (a plugin built for `rat/2` passes the filter on a
`rat/1` deployment), capability **version** skew (a deployment providing `rat://format/v1/scan`
"satisfies" a listing requiring it, but there's no v-major reconciliation logic), signature/trust
policy, or whether the required providers are actually *installed and healthy* vs merely
*available as capabilities*. The proto sells this as "the axis's one hard job" (`marketplace.proto:7-14`);
the reference does the easy 60% of it.
**Recommendation:** document the filter's current scope honestly ("capability-URI presence only")
and enumerate the checks a production marketplace must add (core-major, version skew, trust policy).
Otherwise an operator reads a green filter as "this will run" when it means "the URIs line up."

### 8. [MED] [V2-REGRET] The strategy `options` bytes bag pushes all per-run typing into an unvalidated, undiscoverable blob
Per-run strategy parameters (natural key, tracked columns, run timestamp) ride as opaque
UTF-8-JSON bytes. `plugins/strategy/scd2-py/store.py:62` does `json.loads(options.decode("utf-8"))`
then `store.py:63-65` accesses `spec["natural_key"]` / `spec["tracked"]` with raw dict indexing — a
missing or misspelled key is an **uncaught language exception, not the `INVALID_ARGUMENT` the
ERROR_MODEL mandates** for malformed input. The `metadata_schema` that would describe this blob is
just a **path in the manifest** (`plugin.v1.json:135`), never enforced at the wire. So the
contract's "per-run parameter bag" hands the author a stringly-typed payload with no typing, no
validation, no autocomplete, and no discoverability — and this is invisible from the `.proto` alone.
**Recommendation:** at minimum require strategies to validate `options` against their declared
`metadata_schema` and map failures to `INVALID_ARGUMENT` as a conformance obligation; document it in
a strategy CONTRACT.md (which doesn't exist yet — see #4).

### 9. [LOW] [PROCESS] `RequestContext` (identity/tenant/trace) is undiscoverable from the service proto alone
Context rides in the `rat-callmeta-bin` metadata header, not the request body — the request
messages only show `reserved 1; // RequestContext travels in metadata` (e.g.
`marketplace.proto:56`). An author who implements their plain gRPC service from the `.proto` and
skips the CONTRACT.md will **never learn identity/tenant/trace exist**, and will ship a plugin that
ignores the isolation boundary. Not a wire bug — a hard docs-dependency the proto actively hides
behind a `reserved` comment.
**Recommendation:** can't fix on the frozen wire; make "read context from `rat-callmeta-bin`" a
loud, top-of-file obligation in every axis's CONTRACT.md, and ensure all 18 axes *have* one (#4).

### 10. [LOW] [ADDITIVE] Blame-attribution hangs on optional fields enforced by a toy
`support_url` is "Load-bearing for the support-attribution model… `rat diagnose` points blame here"
(`plugin.v1.json:80-84`) but is **optional in the schema**, "enforced at listing time"
(`schema/README.md:60`) — by a marketplace that is itself a reference. So the blame model is only
as real as a non-existent enforcement path. Fine post-freeze (`support_url` is a string already on
both manifest and listing), but the *enforcement* is vapor like #1/#2.
**Recommendation:** none wire-side; fold into the "what the core must enforce" checklist.

---

## Answers to the mandate questions

- **Could I build a conformant plugin without insider help?** For the **6 documented data-plane
  axes — yes**, impressively so: CONTRACT.md + golden vectors + a real reference is a complete kit.
  For the **other 12 axes — not really**: no author guide, one toy reference, no validate CLI.
- **Is conformance honest?** The **harness** is honest (real gRPC against shared golden JSON,
  auto-discovered, `make conformance` 32/32). The **claims** are not: `provides` and
  `conformed_capabilities` are self-asserted and disconnected from any harness result (#1). Nothing
  enforces "declared == conformed."
- **Is the project honest about works-vs-designed?** Internally yes (roadmap is candid); to an
  outside author **no** — frozen artifacts describe core enforcement in the present tense while the
  core doesn't exist (#2).
- **Manifest still v1-preview a blocker?** Yes, minor-but-real: it's the only artifact the author
  hand-writes and the only one unfrozen, and per-kind validation is missing (#3).
- **Are the 32 refs good templates?** The real ones (round 2), yes. The round-1 toys encode
  stand-ins (in-proc Arrow, ignored `tables`) a newcomer will copy wrongly (#5).

Biggest concern: Conformance and every "CHECKED gate" live at a layer (manifest `provides`, marketplace `conformed_capabilities`, `compatible_core`) that is **self-asserted with no enforcer** — the core that was promised to check them doesn't exist — so a 3rd-party plugin can ship a manifest/listing claiming capabilities it never conformed to and the frozen surface has nothing to stop it.
