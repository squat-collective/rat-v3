# Invalid manifest examples (negative test vectors)

These are deliberately-broken manifests. Once the conformance/validation tooling
exists (sub-phase 0f, `rat plugin validate`), these become the negative test
corpus: each must be **rejected**, with the cited error. They live as Markdown
(not `.yaml`) so no validator picks them up as real plugins.

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
NOTE: the envelope schema's regex *cannot* catch this on its own (`iceberg` is a
syntactically-valid capability segment). This is a **semantic** rule the per-kind
schema + `rat plugin validate` lint must enforce — captured here so the gap is
explicit, not forgotten. See README.md "The per-kind schema question".

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
