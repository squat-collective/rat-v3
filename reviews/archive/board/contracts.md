# Board review — `contracts` lens: the frozen wire shapes

> **Lens:** the wire contracts themselves, now that all 18 axes are FROZEN at `v1` (tag `rat/1`,
> ADR-009 — no breaking changes allowed). What did we just make immutable that a real
> implementation will regret? Evidence is quoted from the frozen proto/vector text.
>
> **Buf config note (load-bearing for the regret/additive split):** `contracts/buf.yaml`
> sets `breaking: use: FILE` — the *strictest* ruleset. So "additive" here means strictly:
> **a new field (new tag), a new enum value, a new RPC on a service, or a new message.**
> Anything else — changing a field's type, its presence/cardinality (`string`→`optional string`),
> a `bool`→`enum`, an RPC's cardinality (unary→stream), or removing/renaming — needs a **v2**.
> That line is what separates the two finding classes below.

Findings ranked by freeze regret. **[V2-REGRET]** = cannot be fixed without a v2 (the worst
kind). **[ADDITIVE]** = a real gap, but closable post-freeze with a new field/value/RPC.
**[PROCESS]** = spec/doc only; the wire shape already permits it.

---

## 1. [V2-REGRET] `WriteResult.snapshot_id` keeps the exact `-1`-sentinel bug that `rows_affected` was fixed to remove — on the sibling field

**Evidence:** `common/v1/data.proto:74-84`.
```
optional int64 rows_affected = 1;   // ABSENT == cannot report (replaces -1 sentinel, API-13)
string snapshot_id = 2;             // "empty if none"  ← NOT optional
```
The freeze deliberately gave `rows_affected` `optional` to split three states — *absent=unknown*,
*0=zero rows*, *n=n* (data.proto:76-77, "replaces the old -1 sentinel — reviews/06 API-13").
The **adjacent field on the same message got no such treatment.** `snapshot_id` is a bare
`string` whose empty value conflates **two distinct states**: "this write produced no version
(unversioned format — legitimate)" vs. "this engine/format *cannot report* a version id."
That is precisely the unknown-vs-real ambiguity `optional` was introduced to kill, left standing
one field over.

**Why it's a true v2 regret (not additive):** under `breaking: use: FILE`, changing
`string snapshot_id = 2` → `optional string snapshot_id = 2` trips `FIELD_SAME_CARDINALITY`
(implicit→explicit presence is wire-observable for the empty case) — it is a **breaking change**.
So presence can never be added to this field in `v1`. If a consumer ever needs to distinguish
"no version" from "couldn't tell me," it needs a v2 or a parallel field.

**Severity: MEDIUM.** Only bites a consumer that must branch on that distinction — but that's the
same predicate that justified `optional` on `rows_affected`, so the inconsistency is real and
permanent. **Recommendation:** accept as a known v1 wart; if/when it bites, add a *new* field
(e.g. `optional string resulting_version = 3`) rather than touching field 2 — that path stays additive.

---

## 2. [ADDITIVE — but high-severity functional hole] Catalog cannot register a NEW table or learn what a write landed — the pipeline loop has no wire path to close

**Evidence:** `catalog/v1/catalog.proto:40-57` — the entire `CatalogService` is `GetTable`,
`CreateBranch`, `MergeBranch`. **There is no `CreateTable` / `RegisterTable` / commit RPC.**
`GetTable` only *resolves an existing* identifier (and errors `NOT_FOUND` otherwise,
catalog.proto:66-74). The proto itself admits the gap (catalog.proto:27-30):
> "the separate gap — how the catalog learns what format.Write put on a branch — is the additive
> commit-linkage RPC (reviews/06 I-18), GA-deferred."

So the headline feature — *branch-isolated pipeline runs with a pre-merge gate* — **cannot complete
its own loop on the frozen surface**: a strategy can create a branch and merge it, but nothing in
the contract lets it (a) register a freshly-created output table into the catalog, or (b) tell the
catalog which snapshot `format.Append/Merge` produced on the branch before `MergeBranch`. The
golden `composition-v1.json` pipeline only works because the harness wires this linkage *out of
band*; the wire contract doesn't express it.

**Why additive:** adding an RPC to a service is backward-compatible under `FILE`. The fix is a new
`RegisterTable`/`Commit` method, exactly as the proto anticipates. **Severity: HIGH** despite being
additive — we froze a catalog that ships functionally incomplete for its own stated purpose, and
R3 quietly accepted it. **Recommendation:** prioritize the commit-linkage RPC as the *first*
post-freeze additive (v1.1), not GA-distant; until it lands, the pipeline model is "works in the
test harness, not in the contract."

**Corroboration (ecosystem + composition harness):** this is the **documented composition
casualty**, not a hypothetical — `plugins/composition/README.md:85-90` finding #3: *"The catalog
has no create-table RPC. `GetTable` only resolves pre-existing tables, so the harness registers the
source+target out-of-band."* `ecosystem` ranks it the #1 wart they hit: a strategy writes via
`format.overwrite/merge` but has no wire path to tell the catalog what snapshot landed, so catalog
and format **silently drift** — for an axis whose whole premise is "branches sit on format
snapshots," the load-bearing RPC is the one missing.

---

## 3. [ADDITIVE] Engine substitutability — the whole ADR-003 thesis — rests on hand-written defensive `CAST`s, with NO output-schema contract on the wire

**Evidence:** `composition-v1.json` `transform_sql` + `sql_note`:
> `SELECT region, CAST(SUM(amount) AS BIGINT) AS total ...`
> "CAST(... AS BIGINT) is deliberate: DuckDB's SUM(int) yields a 128-bit decimal/hugeint while
> DataFusion's yields int64 ... The CAST pins the output type so any conformant engine produces an
> identical int64 column; **without it, an engine-substitution would change the result schema.**"

The engine contract (`engine/v1/engine.proto:64-67`) returns `QueryResponse{ ArrowStream stream }`
— the result schema is *whatever the engine emits*, carried in `ArrowStream.ipc_schema`
(data.proto:66). There is **no field on `QueryRequest` to declare an expected output schema, and
no coercion contract.** Two conformant engines can therefore return **different-typed Arrow** for
the identical SQL, and nothing on the wire flags it. The composition test only goes green because
the golden SQL was *patched* with a CAST — i.e., engine-swap safety is a **strategy-author
discipline**, not a contract guarantee. The cross-engine divergence the freeze is supposed to prove
it survived is real (DuckDB hugeint vs DataFusion int64); the contract just doesn't carry the fix.

**Why additive:** a future `expected_schema` / `coerce` field on `QueryRequest` is a new tag —
additive. **Severity: MEDIUM-HIGH** — a latent correctness footgun the freeze *blesses*: a strategy
that forgets the CAST silently changes results on engine substitution, which is exactly the failure
mode ADR-003 exists to prevent. **Recommendation:** document loudly in engine `CONTRACT.md` that
output-type stability is the *caller's* responsibility in v1; queue an additive output-schema
declaration for v1.x.

---

## 4. [ADDITIVE] `WriteResult` is too thin for a real MERGE — `rows_affected` conflates insert/update/delete

**Evidence:** `data.proto:74-84` (the whole message: `optional int64 rows_affected`, `string
snapshot_id`) is reused by **every** data-plane write — `engine.Execute` (engine.proto:55),
`format.Append/Merge/Overwrite/Maintain` (format.proto:85-118). For `format.Merge` (an upsert
matched on `merge_keys`, format.proto:89-95) a real Iceberg/Delta MERGE reports
*inserted / updated / deleted* counts **separately** — `WriteResult` can only return a single
conflated `rows_affected`. A real merge impl therefore **loses information at the contract
boundary**; a caller can't tell a 1000-insert from a 999-update+1-insert.

**Why additive:** add `optional int64 rows_inserted/updated/deleted = 3..5` — new tags, backward
compatible (engine.Execute simply leaves them unset). **Severity: MEDIUM.**
**Recommendation:** accept for v1; enrich additively when a real format needs the breakdown.

---

## 5. [ADDITIVE] `ArrowStream` has no completeness/termination signal — a consumer can't distinguish "done" from "producer died cleanly mid-stream"

**Evidence:** `data.proto:53-71` — `ArrowStream{ endpoint, ticket, ipc_schema, transport, role }`.
The control-plane RPC (`QueryResponse`/`ResolveResponse`) returns this **descriptor and completes
*before* a single byte flows** (the bytes leg is out-of-band, data.proto:5-7). Arrow Flight's own
channel signals *transport* end-of-stream, but it carries **no RAT-level completeness assertion**
(expected batch/row count, checksum). So a consumer that reads N batches and sees the Flight stream
close cannot tell **"all data delivered"** from **"producer crashed after batch N and closed
cleanly."** R3 flagged this and accepted it; frozen, it's now a permanent property of the v1 stream
descriptor.

**Why additive:** a future `optional int64 expected_batches`/`expected_rows` on `ArrowStream` is a
new tag. **Severity: upgraded to MEDIUM-HIGH** on `ecosystem`'s evidence — this is a *silent
data-corruption* path, not mere ergonomics. The SCD2 reference strategy is both producer and
consumer: it pulls `target_rows` (`plugins/strategy/scd2-py/store.py:69-70`) then treats any
current key **not** present in the pulled source as DELETED and closes its version
(`store.py:90-92`). A truncated scan (producer died mid-stream, consumer can't tell done-vs-crashed)
delivers fewer rows → SCD2 **closes versions that should stay open → silent history corruption**.
So *every* incremental/diffing consumer is one network blip from wrong data with no wire signal to
detect it. **Recommendation:** queue the additive completeness field with priority second only to
#2; document the corruption hazard in the meantime.

---

## 6. [ADDITIVE] `TableRef` collapses branch / snapshot / as-of-time into one `branch` string — thin for a coupling type shared across 7 axes

**Evidence:** `data.proto:17-24` — `TableRef{ identifier, uri, branch }`, where `branch` is
commented "catalog branch/**snapshot** selector" (data.proto:22). Real catalogs/formats
(Iceberg, Delta, Nessie) distinguish **three** time-travel coordinates: a named **branch** (mutable
ref), an immutable **snapshot/version id**, and an **as-of timestamp**. v1 crams all into one string,
forcing ad-hoc overloading ("is `branch` a name or a snapshot id?"). Because `TableRef` is the
frozen coupling type shared by engine, format, catalog, strategy, runtime, storage, and the
composition path, every axis inherits the ambiguity.

**Why additive:** add `optional string snapshot_id`/`as_of_unix_ms` fields — new tags.
**Severity: LOW-MEDIUM.** **Recommendation:** accept; enrich additively when a catalog needs
snapshot-or-timestamp distinct from branch.

---

## 7. [PROCESS] `TableRef.branch` vs the per-RPC `branch` precedence is undefined — and the duplication is now permanent

**Evidence:** `TableRef.branch` (data.proto:23) AND `catalog.GetTableRequest.branch`
(catalog.proto:59-64, "Optional branch to read from (empty == main)") are **both** frozen. When a
caller passes a `TableRef` carrying `branch=X` *and* sets the request-level `branch=Y`, **which
wins is unspecified.** R3 named this; it never got pinned. The two fields can't be removed
(frozen), so the *structural* duplication is permanent — only the **precedence rule** is fixable,
and only in spec.

**Why process (not v2):** the wire already permits the conflict; pinning "request-level `branch`
overrides `TableRef.branch`" (or vice-versa) is a doc change, wire-safe. **Severity: LOW.**
**Recommendation:** pin the precedence in catalog `CONTRACT.md` + ERROR_MODEL-adjacent prose now,
before two impls pick opposite rules and *that* becomes the regret.

---

## 8. [PROCESS] Two streaming/robustness spec gaps left open at freeze

**(a) `InvokeBidiStream` first-frame robustness (S2 only half-pinned).** `invoke.proto:128-138`
pins "non-empty `capability` on a *non-first* frame → ABORT `INVALID_ARGUMENT`" (the S2 fix landed,
good). But the symmetric case — an **empty `capability` on the FIRST frame** (nothing to resolve,
malformed open) — is **not** pinned. The first frame is dual-purpose (carries both `capability`
*and* `payload`, invoke.proto:135-137), so a malformed first frame is a real attack/footgun
surface. *Fix:* pin "empty `capability` on the first frame → ABORT `INVALID_ARGUMENT`," symmetric
to S2. Wire-safe.

**(b) ERROR_MODEL.md doesn't cover streaming cancellation.** `ERROR_MODEL.md:36-55` enumerates the
**closed** set of allowed gRPC codes ("Codes not in this table MUST NOT be used") — but it was
written for unary RPCs and **omits `CANCELLED` / `ABORTED`**, which the ADR-008 streaming variants
(`InvokeServerStream`/`InvokeBidiStream`, invoke.proto:55-61) *will* surface when a client cancels a
stream. Taken literally, a gateway propagating `CANCELLED` on a client-cancelled stream is
**non-conformant**. *Fix:* add `CANCELLED` (and clarify `ABORTED` vs the S2 `INVALID_ARGUMENT`
abort) to the table — additive doc expansion on a frozen doc.

**Severity: LOW** (both spec-only). **Recommendation:** fold both into a v1.0.1 spec-text pass;
neither touches the wire.

---

## 9. [PROCESS] The bulk-data plane's cross-tenant isolation is impl-asserted and **untested** on BOTH ends at v1 (consult: `security`)

The `ArrowStream.ticket` SHAPE is fine to freeze — `bytes ticket [debug_redact]` (data.proto:56-63)
is maximally flexible (a signed blob, an STS-style handle, a macaroon all fit; deliberately no
on-wire `expiry`/`nonce` to ossify), so adding structure later is additive. **Not a v2 regret.**
The defect is **conformance, not wire:** the bytes leg bypasses the core (ADR-005), so the ticket is
the *only* gate on cross-tenant bulk reads — yet TTL / single-use / tenant-binding are
**MUST-in-prose** (data.proto:56-62) with **zero conformance vectors** exercising them. Two impls
can both be 20/20-conformant while issuing guessable, non-expiring, replayable tickets.

`security` adds the aggregate that the per-finding footnotes understate: this is the **same
honor-system class as storage `VendCredentials`** (residual R2 — tenant-scoping tested only via a
JSON stand-in "scope receipt"). Ticket + vended-cred together mean the **bulk-data plane's tenant
isolation is impl-trusted on both ends at v1**, and the two gaps are documented separately so the
combined exposure reads smaller than it is. **Severity: MEDIUM (security-weighted).**
**Recommendation (additive, no wire change):** add a Flight conformance vector asserting
TTL-expiry + single-use-rejection + cross-tenant-ticket-rejection against `parquet-py` (already
produces real Flight); until then, label the guarantee **"impl-asserted, untested"** in storage +
data-plane `CONTRACT.md`.

---

## 10. [PROCESS] The per-run `options` bag is stringly-typed — all validation/typing/discoverability pushed into an opaque blob the author hand-parses (consult: `ecosystem`)

Invisible from the service protos alone, so flagging it explicitly. Strategy per-run params ride as
`bytes options = 4`, "ENCODING PINNED … UTF-8 JSON, validated against the plugin's own
metadata_schema (manifest)" (`strategy/v1/strategy.proto:37-41`). But that schema is a **path in the
manifest** (`plugin.v1.json:135`), **never enforced at the wire**. The SCD2 reference does raw dict
access — `json.loads(options.decode("utf-8"))` then `spec["natural_key"]` / `spec["tracked"]`
(`store.py:62-65`) — so a missing/misspelled key is an **uncaught language exception, not an
`INVALID_ARGUMENT`** per ERROR_MODEL.md. The contract's "parameter bag" thus pushes *all* per-run
typing, validation, and discoverability into a hand-parsed blob with no wire help. `ecosystem` rates
this the worst *pure-ergonomics* wart, and it's structurally adjacent to `Invoke`'s own opaque
`payload` design — the generic-proxy/`bytes`-bag pattern trades type-safety for core-simplicity
everywhere it appears.

**Why process (not regret):** the `bytes` shape is deliberate and additive-friendly; the gap is that
nothing *enforces* the declared schema. **Severity: LOW-MEDIUM (ergonomics).**
**Recommendation:** make "options that fail the manifest `metadata_schema` → `INVALID_ARGUMENT`" a
stated conformance obligation (mirrors the M1 error-model rule), so the validation isn't optional
per-impl. (Honorable mention from `ecosystem`: `RequestContext`-in-metadata is undiscoverable from a
service proto alone — request messages show only `reserved 1; // travels in metadata` — so an author
who skips `CONTRACT.md` never learns identity/tenant exist. Docs-dependency the proto hides; not a
wire bug.)

---

## Separation summary

| # | Finding | Class | Severity |
|---|---|---|---|
| 1 | `snapshot_id` empty-sentinel (the un-fixed twin of the `rows_affected` fix) | **V2-REGRET** | MED |
| 2 | Catalog has no CreateTable/commit-linkage RPC — pipeline loop can't close on the wire | ADDITIVE | **HIGH** |
| 3 | Engine output-schema divergence; substitutability rests on hand-written CASTs | ADDITIVE | MED-HIGH |
| 4 | `WriteResult` too thin for MERGE (conflated `rows_affected`) | ADDITIVE | MED |
| 5 | `ArrowStream` no completeness/termination signal → SCD2 silent corruption | ADDITIVE | **MED-HIGH** |
| 6 | `TableRef` collapses branch/snapshot/as-of into one string | ADDITIVE | LOW-MED |
| 7 | `TableRef.branch` vs per-RPC `branch` precedence undefined | PROCESS | LOW |
| 8 | BidiStream empty-first-frame + ERROR_MODEL missing CANCELLED/ABORTED | PROCESS | LOW |
| 9 | Bulk-plane tenant isolation impl-asserted + untested on both ends (ticket + VendCredentials) | PROCESS | MED |
| 10 | Stringly-typed `options` bag — schema declared but unenforced at wire | PROCESS | LOW-MED |

**The reassuring part:** only **one** finding (#1) is a true v2 regret, and it's medium-severity.
The freeze's most dangerous-sounding items (engine type divergence, ArrowStream termination,
catalog commit-linkage) are all **closable additively** — the contract was frozen at a shape that
*leaves room*, which is the right outcome for a `v1`. The cross-cutting keystone types
(`RequestContext`/`Identity`/`SubjectAssertion`) are tight; the `proto3 optional` audit found the
sentinel hygiene clean **except** the `snapshot_id`/`rows_affected` asymmetry in #1.

**The cross-consult sharpened two things the proto text hid.** (a) Five of the ten findings are
**[PROCESS]** — and that's the real pattern: the wire shapes are largely fine, but the *guarantees
they're supposed to carry* (ticket isolation #9, options validation #10, branch precedence #7,
stream completeness #5) are **prose MUSTs with no conformance vector**, so two impls can be
20/20-conformant while violating them. The freeze locked the shapes but not the obligations.
(b) The bytes-bag pattern (`Invoke.payload`, `ArrowStream.ticket`, strategy `options`) recurs by
design — it buys the six-thing core its simplicity, but each instance exports typing/validation/
trust to the impls, and at v1 **none of it is enforced because the core that would enforce it
doesn't exist yet** (`ecosystem`'s closing point). That's not a wire regret; it's the standing risk
the contract freeze can't retire on its own.

**Biggest concern:** Finding #2 — we froze a `CatalogService` with no way to register a table or
record what a write landed, so the *branch-isolated-pipeline* headline feature literally cannot
close its loop on the frozen wire; it works only because the test harness fakes the linkage
out-of-band, and "additive, GA-deferred" understates that the contract ships functionally
incomplete for its own stated purpose.
