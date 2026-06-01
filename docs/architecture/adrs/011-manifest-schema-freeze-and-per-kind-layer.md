# ADR-011: Freeze the plugin manifest schema at `v1` + add the per-kind schema layer

## Status: Accepted (2026-06-01)

## Context

The manifest is one leg of the contract triple (manifest + proto + capability URI) and
**the only artifact a plugin author hand-writes**. [ADR-009](009-data-plane-contract-freeze-v1.md)
froze the protos + cross-cutting types at `rat/1` but deliberately left the manifest
schema (`contracts/schema/plugin.v1.json`) at `v1-preview`:

> **The plugin manifest schema (`plugin/v1`)** — iterate until it stabilizes across the
> remaining reference work (ADR-003).

That reference work is done: all 18 axes are referenced + frozen (`rat/1`→`rat/1.4`), 32
references conform. The envelope schema has been stable across all of it. The post-freeze
board review ([reviews/08](../../../reviews/08-post-freeze-board-review.md)) flagged the
two remaining manifest gaps as **E2**:

> Manifest schema is the **only** unfrozen artifact **and** the only thing an author
> hand-writes, **and** per-kind schemas don't exist — an author can't finalize or fully
> validate a manifest. **Fix:** freeze `plugin.v1.json` + ship the 18 per-kind schemas in
> one stroke (the protos are frozen, so required-capability sets are derivable).

The envelope (`plugin.v1.json`) validates the structure common to *every* axis
(`api_version`, `metadata`, `provides`/`requires` shape, `resources`, `trust`,
capability-URI grammar) but — by design (`schema/README.md`, "the per-kind schema
question") — encodes **no** per-kind rule. So today the most basic author mistake ships
silently: a `kind: format` manifest that declares `provides: [rat://engine/v1/query]`, or
one that forgets `rat://format/v1/scan` entirely, passes the envelope.

One wrinkle surfaced while scoping the per-kind schemas: only the **6 data-plane axes**
carry the `(rat.common.v1.capability)` annotation on their RPCs; the other **12** have
their capability URIs only in header comments. The `schema/README.md` intent is that
per-kind schemas are **derived from the proto and kept in sync** — which is only possible
if every axis's capability set is machine-readable in one place. Rolling the annotation
across the remaining 12 axes was already queued (backlog, "roll `(rat.capability)` across
remaining axes" — additive, needed anyway for the Phase-1 C5 gateway + C6 conformance), so
this ADR folds it in as the enabling step.

## Decision

**Freeze `plugin.v1.json` at `v1` (additive-only thereafter), and add a per-kind schema
layer with minimal-mandatory-core `provides` rules.** Three parts:

### 1. Roll `(rat.common.v1.capability)` across all 18 axes (the enabling step)

Add `import "rat/common/v1/annotations.proto";` + `option (rat.common.v1.capability) =
"rat://<axis>/v1/<cap>"` to each RPC of the 12 not-yet-annotated axes (URIs taken verbatim
from each proto's header comment). This is **additive** — a method option is
`buf breaking` FILE-compatible — and makes every axis's capability set machine-readable in
one place, so the per-kind schemas (and a future drift-check) derive from the proto rather
than duplicating it.

### 2. Freeze the envelope `plugin.v1.json` → `v1`

The envelope is unchanged in shape (it has been stable across all 32 references); freezing
flips its governance: **within `rat/1`, only additive backward-compatible changes**;
anything breaking ships as `plugin.v2.json` under `api_version: rat/2` (mirrors capability
major-versioning, ADR-002 D4). `schema/README.md` "Changing this schema" is updated to say
so.

### 3. Author 18 per-kind schemas with a *minimal mandatory core*

One schema per axis at `contracts/schema/kinds/<kind>.v1.json`, JSON Schema 2020-12. Each
`allOf`-references the envelope and adds exactly two per-kind constraints:

- `kind` is `const` the kind;
- `provides` MUST `contain` each capability in that kind's **mandatory core**.

Strictness is **minimal mandatory core**, not "all capabilities" — because capability
negotiation means a plugin legitimately implements a *subset* (a Glue catalog provides no
branch capabilities; a read-only format provides no writes). The schema requires only the
irreducible set that makes a plugin *that kind at all*; everything else stays optional and
is validated structurally by the envelope. The cores (a judgement call per multi-capability
axis — **vetoable**; single-capability axes are trivial):

| kind | capability axis (URI segment) | all capabilities | **mandatory core** | rationale for the core |
|---|---|---|---|---|
| `engine` | `engine` | execute, query, preview | **query** | the result-returning transform path; read-only engines are common, write-only/preview-only ones are not |
| `runtime` | `runtime` | execute | **execute** | its only capability |
| `format` | `format` | scan, append, merge, overwrite, maintain | **scan** | a table format you can't read isn't one; read-only formats are valid (writes optional) |
| `strategy` | `strategy` | apply | **apply** | its only capability |
| `catalog` | `catalog` | get-table, create-branch, merge-branch, register-table, commit-table | **get-table** | resolve is the irreducible catalog function; branch/merge/register/commit are optional (Glue) |
| `storage` | `storage` | vend-credentials | **vend-credentials** | its only capability |
| `deployment-runtime` | `deployment-runtime` | launch, terminate, healthcheck | **launch, terminate** | the lifecycle pair; healthcheck is strongly recommended but a minimal runtime can omit it |
| `state-backend` | `state` | get, put, list, watch | **get, put** | the irreducible kv; list/watch are optional |
| `secret-backend` | `secret` | resolve | **resolve** | its only capability |
| `scheduler-backend` | `scheduler` | schedule, cancel, watch-due | **schedule, watch-due** | you schedule work and surface when it's due (the reconciler consumes watch-due); cancel optional |
| `identity` | `identity` | authenticate, authorize | **authenticate** | authn is the core; authz can be a separate concern/plugin |
| `tenancy` | `tenancy` | decide | **decide** | its only capability |
| `billing` | `billing` | record | **record** | its only capability |
| `observability` | `observability` | ingest | **ingest** | its only capability |
| `audit-log` | `auditlog` | append | **append** | its only capability |
| `ui` | `ui` | describe, render-slot | **describe, render-slot** | describe advertises slots, render-slot produces them; both are load-bearing |
| `notifications` | `notifications` | send | **send** | its only capability |
| `marketplace` | `marketplace` | search, get | **search, get** | both are needed to be a marketplace |

**The kind↔axis-segment mapping is NOT always identity** and is a documented footgun the
per-kind schemas must get right: `state-backend`→`state`, `secret-backend`→`secret`,
`scheduler-backend`→`scheduler`, `audit-log`→`auditlog`; `deployment-runtime`→
`deployment-runtime` (hyphenated). Because the cores are taken from the proto's *documented
URIs*, the mapping is baked in correctly.

## Consequences

**Positive.**

- The author surface is **complete and stable**: the one hand-written artifact is frozen,
  and a `kind`-specific schema catches the wrong/missing-required-capability mistake the
  envelope can't (the board's E2). Every axis's capability set is machine-readable in the
  proto (the annotation roll), so the schemas derive from one source.
- Closes the last `v1-preview` artifact — the contract surface is now uniformly frozen.
- The annotation roll also unblocks the Phase-1 C5 gateway + C6 conformance for the 12
  control/experience axes (they need the annotation to route/enforce), so it is not
  schema-only throwaway.

**Negative — accepted.**

1. **The mandatory cores are a judgement call.** For multi-capability axes (engine, format,
   catalog, deployment-runtime, scheduler-backend, state-backend, identity, ui, marketplace)
   "what is irreducible" is opinionated; a reasonable author could draw the line one
   capability over. Mitigation: per-kind schemas are **author tooling, not the frozen wire**
   — tightening or loosening a single kind's core later is non-breaking. The table is
   reviewed + vetoable.
2. **Per-kind schemas can drift from the protos** if a capability URI changes. Mitigation:
   protos are frozen (`rat/1`); a derive/drift-check script is a cheap future addition (the
   annotation roll makes it possible).
3. **The envelope freeze locks the manifest shape.** Additive-only within `rat/1`. This is
   the intended cost (same as ADR-009 for the protos); the envelope is board-reviewed and
   has been stable across 32 references.

**Neutral.**

- Semantic capability validity (e.g. `rat://format/v1/iceberg` — a syntactically valid URI
  naming an *implementation* not a verb) is still **not** caught by schema; that needs a
  curated capability registry + lint (`rat plugin validate`, deferred to Phase 1/0f). The
  per-kind `contains` checks catch *missing* core capabilities and *wrong-axis* capabilities,
  not *invented* capability verbs.

## Open questions

- **Q01 — Generate the per-kind schemas from the annotations, or hand-author?** This ADR
  hand-authors them (18 small files) from the now-uniform annotations. A generator
  (`proto annotations → kinds/*.json`) would keep them in lockstep and is a natural follow-up
  once a second consumer of the capability sets exists (the C5 gateway). Deferred.
- **Q02 — `requires` per-kind rules?** This layer constrains `provides` only. Some kinds
  have a natural `requires` (a `strategy` requires format/catalog capabilities; a
  branch-using strategy requires `merge-branch`) but those are *per-plugin*, not *per-kind*
  — a kind doesn't mandate a `requires`. Left to `suggests` + the reconciler's
  install-time satisfaction check.
- **Q03 — `rat plugin validate` CLI.** The board's optional E2 tail (semantic pass +
  INVALID-examples corpus). Deferred to 0f/Phase 1; the schemas are the static half.

## Alternatives considered

1. **Strictness: require *all* of an axis's capabilities.** **Rejected** — rejects valid
   subset plugins (a non-branching catalog, a read-only format), directly contradicting the
   capability-negotiation principle (ADR-001) the whole architecture rests on.
2. **Strictness: require only ≥1 capability of the kind's axis (fully mechanical).**
   **Rejected as the primary** — it catches wrong-axis/typo'd capabilities but *not* a
   plugin that forgot its core capability (a "format" with no `scan`), which is exactly the
   silent author mistake E2 names. (It is the safe fallback if a core turns out
   controversial; `≥1`→`mandatory-core` is a non-breaking tightening.)
3. **One mega-schema with `oneOf`/`if-then` on `kind`** instead of 18 files. **Rejected** —
   `schema/README.md` already chose "layered per-kind schemas"; separate files give a clean
   per-author target (`$ref` the one for your kind), better error messages, and let a
   community axis ship its own per-kind schema without editing a shared file. The envelope
   stays the single shared structural base via `allOf`.
4. **Author per-kind schemas from the header comments; skip the annotation roll.**
   **Rejected** — leaves the capability set in two places (comment + schema) with no
   machine-readable link, against the README's derive-and-sync intent, and still leaves the
   annotation roll (needed for Phase-1 enforcement) undone.

## Migration

This is the design from `rat/1.1` onward. Landing sequence: (1) annotation roll across the
12 axes + `make gen-sdks`; (2) flip `plugin.v1.json` + `schema/README.md` to frozen; (3)
author `contracts/schema/kinds/<kind>.v1.json` × 18; (4) validate the two example manifests
against envelope + per-kind, and a new wrong-capability-for-kind INVALID vector against the
per-kind schema; `make conformance` stays 32/32 (the annotation roll is additive). Folded
into the `v1.1` cut.

## Related

- [ADR-009](009-data-plane-contract-freeze-v1.md) — froze the protos; explicitly deferred
  the manifest schema, which this ADR now freezes.
- [ADR-002](002-founding-tech-stack.md) D3 (JSON Schema for manifests) / D4 (capability
  major-versioning in the URI).
- [ADR-001](001-everything-is-a-plugin.md) — capability negotiation (the subset principle
  that makes "minimal mandatory core" the right strictness).
- [reviews/08](../../../reviews/08-post-freeze-board-review.md) **E2** — the finding this
  ADR closes.
- [`contracts/schema/plugin.v1.json`](../../../contracts/schema/plugin.v1.json) +
  [`schema/README.md`](../../../contracts/schema/README.md) — the envelope + the "per-kind
  schema question" this ADR resolves.
