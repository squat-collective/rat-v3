# Invalid manifest examples (negative test vectors)

These are deliberately-broken manifests. Each must be **rejected**, with the cited
error. They live as Markdown (not `.yaml`) so no validator picks them up as real
plugins. The **static** half (envelope + per-kind schema, #1–#3, #5, #6) is enforced
today by [`scripts/validate-manifests.py`](../../scripts/validate-manifests.py); the
**semantic** half (#4) still needs `rat plugin validate`'s curated-capability lint
(sub-phase 0f).

## 1. Missing mandatory `resources` (violates C4)

```yaml
api_version: rat/1
kind: strategy
metadata: { name: rat-strategy-bad, version: 0.1.0 }
provides:
  - capability: rat://strategy/v1/apply
# no resources block
```
**Expected rejection:** `resources` is required.

## 2. Empty `provides` (a plugin must implement at least one capability)

```yaml
api_version: rat/1
kind: format
metadata: { name: rat-format-empty, version: 0.1.0 }
provides: []
resources: { requests: { cpu: "100m" } }
```
**Expected rejection:** `provides` must have at least 1 item.

## 3. Malformed capability URI (missing version segment)

```yaml
api_version: rat/1
kind: strategy
metadata: { name: rat-strategy-baduri, version: 0.1.0 }
provides:
  - capability: rat://strategy/apply
resources: { requests: { cpu: "100m" } }
```
**Expected rejection:** `provides[0].capability` does not match the
`rat://<axis>/v<major>/<capability>` grammar.

## 4. Coupling to a concrete peer plugin (the cardinal sin)

```yaml
api_version: rat/1
kind: strategy
metadata: { name: rat-strategy-coupled, version: 0.1.0 }
provides:
  - capability: rat://strategy/v1/apply
requires:
  - capability: rat://format/v1/iceberg   # naming an impl, not a capability
resources: { requests: { cpu: "100m" } }
```
**Expected rejection:** `iceberg` is an implementation name, not a capability verb.
NOTE: neither the envelope **nor** the per-kind schema can catch this — `iceberg` is a
syntactically-valid capability segment, and per-kind schemas only constrain `provides`
(not `requires`), and only by URI string, not by a curated set of real capability verbs.
This is a **semantic** rule for `rat plugin validate`'s lint — captured here so the gap is
explicit, not forgotten. See README.md "The per-kind schema question" + ADR-011.

## 5. Wrong `api_version`

```yaml
api_version: rat/2
kind: strategy
metadata: { name: rat-strategy-futureapi, version: 0.1.0 }
provides:
  - capability: rat://strategy/v1/apply
resources: { requests: { cpu: "100m" } }
```
**Expected rejection:** `api_version` must be `rat/1` for this schema.
(A future `rat/2` ships its own `plugin.v2.json`.)

## 6. Wrong / missing required capability for the `kind` (per-kind schema, ADR-011)

```yaml
api_version: rat/1
kind: format
metadata: { name: rat-format-nocaps, version: 0.1.0 }
provides:
  - capability: rat://format/v1/append   # a "format" that cannot be READ
resources: { requests: { cpu: "100m" } }
```
**Expected rejection:** a `kind: format` MUST provide its mandatory core
`rat://format/v1/scan` (a table format you can't read isn't one). Caught by the per-kind
schema [`schema/kinds/format.v1.json`](../schema/kinds/format.v1.json) (ADR-011) — the
**envelope alone passes this** (it only checks `provides` *shape*, not per-kind
obligations), which is exactly why the per-kind layer exists. The same schema rejects a
manifest whose `provides` are all the **wrong axis** for its `kind` (e.g. a `kind: format`
providing only `rat://engine/v1/query`). Both are exercised by
[`scripts/validate-manifests.py`](../../scripts/validate-manifests.py).
