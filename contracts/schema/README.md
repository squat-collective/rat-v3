# Manifest schema — design notes

The manifest schema is [`plugin.v1.json`](plugin.v1.json), JSON Schema 2020-12,
chosen per [ADR-002 D3](../../docs/architecture/adrs/002-founding-tech-stack.md)
for operator-editability + IDE autocomplete/inline-error support.

## The per-kind schema question (the one real open decision in 0a)

`reviews/02-plugin-ecosystem-builder.md` Stage 5 raised this and it matters for
author-facing error quality: **one mega-schema with `oneOf` on `kind`, or one
schema per kind?**

`plugin.v1.json` as written is the **envelope** approach: a single
kind-agnostic schema that validates the structure common to *every* axis
(`api_version`, `metadata`, `provides`/`requires` shape, `resources`, `trust`,
capability-URI grammar). It deliberately does **not** encode per-kind rules like
"a `kind: engine` must provide `rat://engine/v1/scan`."

**Why envelope-first:**
- It's the 80% of validation every plugin needs, and it ships now (0a) without
  waiting on the 20 protos (0b) that define each axis's required capabilities.
- It keeps the open-set principle (ADR-001): community axes validate structurally
  even before a per-kind schema exists for them.

**What it cannot catch (documented gaps, not oversights):**
1. **Per-kind capability obligations** — "does this `format` provide the
   capabilities a `format` must?" Needs the axis proto (0b) to define the
   required set. → layered per-kind schemas, written alongside each axis proto.
2. **Semantic capability validity** — the regex accepts `rat://format-capability/v1/iceberg`
   (syntactically fine) even though `iceberg` is an *implementation name*, not a
   capability verb (INVALID-examples.md #4). Catching "you coupled to an impl"
   requires a curated capability registry + lint, not pure schema. → `rat plugin
   validate` semantic pass (0f).

**Decision for 0a:** ship the envelope now; layer per-kind schemas in 0b as each
axis proto lands (the proto *is* the source of truth for that kind's required
capabilities, so the per-kind schema is generated/derived from it, keeping them
in sync). This is recorded here rather than in an ADR because it's a structural
choice within an already-decided approach (D3); promote to an ADR if the per-kind
layering turns out to need its own contract.

## Notable field choices

- **`resources` is required** (C4). Even in-process solo plugins declare asks, so
  the reconciler and operator can always reason about cost. `requests` is the
  required floor; `limits` optional.
- **`trust` is optional** (C8). Solo allows unsigned local plugins; team+
  deployments enforce presence at install time (enforcement is the core's job,
  not the schema's — the schema only validates shape when present).
- **`provides` minItems 1.** A plugin that implements no capability isn't a
  plugin. This is also the C5 anchor: the gateway enforces calls against exactly
  this list.
- **`metadata.version` is SemVer; capability versions are major-only** and live
  in the URI, not here (ADR-002 D4). Two different version systems on purpose —
  the plugin's own lifecycle vs. the contract it speaks.
- **`compatible_core` is an array of `rat/<major>`** and is meant to be a
  *checked* gate (like VSCode `engines.vscode`), per reviews/02 Stage 10.
- **`support_url`** — required for team+ marketplace listing; underpins the
  blame-attribution model (reviews/02 Stage 8). Optional in the schema so solo
  plugins aren't blocked, enforced at listing time.

## Changing this schema

Until the `rat/1` freeze (sub-phase 0h), edit freely. After freeze: additive,
backward-compatible changes only within `rat/1`; anything breaking ships as
`plugin.v2.json` under a new `api_version: rat/2` (mirrors capability
major-versioning, ADR-002 D4).
