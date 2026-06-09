#!/usr/bin/env python3
"""Manifest-validation harness — the static half of `rat plugin validate` (ADR-011).

Validates that:
  1. the envelope (contracts/schema/plugin.v1.json) and all 18 per-kind schemas
     (contracts/schema/kinds/<kind>.v1.json) are themselves valid JSON Schema 2020-12;
  2. the example manifests pass BOTH the envelope and their per-kind schema;
  3. the negative vectors are REJECTED — the existing INVALID-examples classes the
     envelope catches (missing resources, empty provides, malformed URI, wrong
     api_version) PLUS the new per-kind class this layer adds: wrong/missing required
     capability for the kind, and a kind/provides mismatch.

Run in a container (no host installs):

  podman run --rm -v "$PWD":/work:Z -w /work python:3.12 bash -lc \
    'pip install -q jsonschema pyyaml >/dev/null 2>&1 && python scripts/validate-manifests.py'

Exit 0 iff every expectation holds. This is the static schema gate; the SEMANTIC pass
(e.g. "iceberg is an impl name, not a capability verb" — INVALID-examples #4) needs a
curated capability registry and is NOT enforced here (documented gap, ADR-011 / README).
"""
import json
import os
import sys

import yaml
from jsonschema import Draft202012Validator
from referencing import Registry, Resource

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SCHEMA_DIR = os.path.join(ROOT, "contracts", "schema")
KINDS_DIR = os.path.join(SCHEMA_DIR, "kinds")
EXAMPLES = os.path.join(ROOT, "contracts", "examples")

ENVELOPE = json.load(open(os.path.join(SCHEMA_DIR, "plugin.v1.json")))
KIND_SCHEMAS = {
    os.path.basename(p)[: -len(".v1.json")]: json.load(open(os.path.join(KINDS_DIR, p)))
    for p in sorted(os.listdir(KINDS_DIR))
    if p.endswith(".v1.json")
}

# Registry so each per-kind schema's `$ref` to the envelope $id resolves.
REGISTRY = Registry().with_resource(
    ENVELOPE["$id"], Resource.from_contents(ENVELOPE)
)


def passes(schema, instance):
    v = Draft202012Validator(schema, registry=REGISTRY)
    errs = sorted(v.iter_errors(instance), key=lambda e: e.path)
    return (not errs), "; ".join(e.message for e in errs[:2])


def load_yaml(path):
    with open(path) as f:
        return yaml.safe_load(f)


results = []  # (name, ok)


def expect(name, schema, instance, want_valid):
    ok, msg = passes(schema, instance)
    good = ok == want_valid
    results.append((name, good))
    verb = "PASS" if ok else "FAIL"
    exp = "valid" if want_valid else "rejected"
    detail = "" if good else f"  <-- wanted {exp}; {('err: ' + msg) if msg else 'unexpectedly valid'}"
    print(f"  [{'ok' if good else 'XX'}] {name}: schema says {verb}{detail}")


# --- 1. schemas are valid JSON Schema 2020-12 -------------------------------------
print(">> per-kind schemas are valid JSON Schema 2020-12")
for kind, sch in KIND_SCHEMAS.items():
    try:
        Draft202012Validator.check_schema(sch)
        results.append((f"meta:{kind}", True))
        print(f"  [ok] meta:{kind}")
    except Exception as e:  # noqa
        results.append((f"meta:{kind}", False))
        print(f"  [XX] meta:{kind}: {e}")
assert len(KIND_SCHEMAS) == 18, f"expected 18 per-kind schemas, found {len(KIND_SCHEMAS)}"

# --- 2. example manifests pass envelope + their per-kind --------------------------
print(">> example manifests pass envelope + per-kind")
scd2 = load_yaml(os.path.join(EXAMPLES, "rat-strategy-scd2.plugin.yaml"))
delta = load_yaml(os.path.join(EXAMPLES, "rat-format-deltalake.plugin.yaml"))
expect("scd2 -> envelope", ENVELOPE, scd2, True)
expect("scd2 -> kinds/strategy", KIND_SCHEMAS["strategy"], scd2, True)
expect("deltalake -> envelope", ENVELOPE, delta, True)
expect("deltalake -> kinds/format", KIND_SCHEMAS["format"], delta, True)

# --- 3. negative: the per-kind class this layer ADDS ------------------------------
print(">> per-kind rejections (the new validation this layer adds)")
base = lambda kind, provides: {
    "api_version": "rat/1", "kind": kind,
    "metadata": {"name": f"rat-{kind}-neg", "version": "0.1.0"},
    "provides": [{"capability": c} for c in provides],
    "resources": {"requests": {"cpu": "100m"}},
}
# format that forgot its mandatory core (scan)
expect("format missing scan", KIND_SCHEMAS["format"],
       base("format", ["rat://format/v1/append"]), False)
# format that provides only a WRONG-AXIS capability
expect("format wrong-axis (engine cap)", KIND_SCHEMAS["format"],
       base("format", ["rat://engine/v1/query"]), False)
# state-backend that provides get but forgot put
expect("state-backend missing put", KIND_SCHEMAS["state-backend"],
       base("state-backend", ["rat://state/v1/get"]), False)
# kind/schema mismatch: a strategy manifest checked against the format schema
expect("strategy manifest -> kinds/format (kind const)", KIND_SCHEMAS["format"], scd2, False)
# a valid minimal format (scan only) passes
expect("format scan-only is valid", KIND_SCHEMAS["format"],
       base("format", ["rat://format/v1/scan"]), True)

# --- 4. negative: the existing INVALID-examples classes the ENVELOPE catches ------
print(">> envelope rejections (INVALID-examples #1,#3,#5)")
expect("#1 missing resources", ENVELOPE,
       {"api_version": "rat/1", "kind": "strategy",
        "metadata": {"name": "rat-strategy-bad", "version": "0.1.0"},
        "provides": [{"capability": "rat://strategy/v1/apply"}]}, False)
# ADR-039: empty `provides` is VALID at the envelope — it's the DRIVER shape (provides nothing,
# only requires). The envelope relaxed minItems 1->0; the "provides>=1 OR requires>=1" floor is the
# CLI authoring gate, not the envelope. (Was INVALID-examples #2; reclassified by ADR-039.)
expect("#2 empty provides is a valid driver (ADR-039)", ENVELOPE,
       {"api_version": "rat/1", "kind": "ui",
        "metadata": {"name": "rat-ui-driver", "version": "0.1.0"},
        "provides": [], "requires": [{"capability": "rat://strategy/v1/apply"}],
        "resources": {"requests": {"cpu": "100m"}}}, True)
expect("#3 malformed capability URI", ENVELOPE,
       {"api_version": "rat/1", "kind": "strategy",
        "metadata": {"name": "rat-strategy-baduri", "version": "0.1.0"},
        "provides": [{"capability": "rat://strategy/apply"}],
        "resources": {"requests": {"cpu": "100m"}}}, False)
expect("#5 wrong api_version", ENVELOPE,
       {"api_version": "rat/2", "kind": "strategy",
        "metadata": {"name": "rat-strategy-futureapi", "version": "0.1.0"},
        "provides": [{"capability": "rat://strategy/v1/apply"}],
        "resources": {"requests": {"cpu": "100m"}}}, False)

# --- 5. documented GAP: semantic impl-naming is NOT caught by schema (#4) ----------
print(">> documented gap: semantic capability validity is NOT a schema check (#4)")
iceberg = {"api_version": "rat/1", "kind": "strategy",
           "metadata": {"name": "rat-strategy-coupled", "version": "0.1.0"},
           "provides": [{"capability": "rat://strategy/v1/apply"}],
           "requires": [{"capability": "rat://format/v1/iceberg"}],
           "resources": {"requests": {"cpu": "100m"}}}
# It PASSES schema (iceberg is syntactically a valid segment) — this is the known gap
# that needs `rat plugin validate`'s semantic pass, not the static schema.
expect("#4 iceberg passes schema (gap, expected)", KIND_SCHEMAS["strategy"], iceberg, True)

# --- summary ----------------------------------------------------------------------
passed = sum(1 for _, ok in results if ok)
total = len(results)
print(f"\n>> {passed}/{total} manifest validation checks behaved as expected")
if passed == total:
    print(">> MANIFESTS VALID ✅")
    sys.exit(0)
print(">> MANIFEST VALIDATION FAILED ❌")
sys.exit(1)
