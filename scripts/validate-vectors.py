#!/usr/bin/env python3
"""validate-vectors.py — the conformance-vector lint gate (backlog DX-4).

The golden vectors (contracts/conformance/*-v1.json) are the cross-language proof of
axis conformance — but they are hand-written JSON, and every harness SILENTLY SKIPS a
key it doesn't recognize. A typo'd 'rows_afected' means the vector passes while testing
nothing. This gate makes that impossible to miss:

  LAYER 1 — envelope: every vector validates against
            contracts/schema/conformance-vector.v1.json (a declared `axis`; sections
            stay free-form, because each axis's steps are intentionally different).
  LAYER 2 — key registry: every "step object" (any object carrying step/op/expect)
            may only use keys registered for its file in the schema's
            $defs.keyRegistry; same for the keys inside `expect`. Unknown key → FAIL,
            with a did-you-mean. A new vector file containing step objects must
            register itself.

Run via `make validate-vectors` (containerized; part of `make verify`). Exit 0 iff
every vector is clean.
"""

import difflib
import glob
import json
import os
import sys

import jsonschema

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCHEMA_PATH = os.path.join(ROOT, "contracts/schema/conformance-vector.v1.json")
VECTOR_GLOB = os.path.join(ROOT, "contracts/conformance/*-v1.json")

STEP_MARKERS = {"step", "op", "expect"}


def step_objects(node, path="$"):
    """Yield (path, obj) for every dict that looks like a test step."""
    if isinstance(node, dict):
        if STEP_MARKERS & node.keys():
            yield path, node
        for k, v in node.items():
            yield from step_objects(v, f"{path}.{k}")
    elif isinstance(node, list):
        for i, v in enumerate(node):
            yield from step_objects(v, f"{path}[{i}]")


def suggest(key, allowed):
    near = difflib.get_close_matches(key, allowed, n=1)
    return f" (did you mean {near[0]!r}?)" if near else ""


def main():
    schema = json.load(open(SCHEMA_PATH))
    registry = {k: v for k, v in schema["$defs"]["keyRegistry"].items() if isinstance(v, dict) and "step_keys" in v}
    validator = jsonschema.Draft202012Validator(schema)

    files = sorted(glob.glob(VECTOR_GLOB))
    if not files:
        print("no vectors found — wrong directory?", file=sys.stderr)
        return 2

    failures = 0
    for f in files:
        stem = os.path.basename(f).removesuffix(".json")
        problems = []
        try:
            doc = json.load(open(f))
        except json.JSONDecodeError as e:
            print(f"  ✗ {stem}: not valid JSON — {e}")
            failures += 1
            continue

        # Layer 1 — the envelope.
        for err in validator.iter_errors(doc):
            problems.append(f"envelope: {err.message}")

        # Layer 2 — the per-file key registry.
        steps = list(step_objects(doc))
        entry = registry.get(stem)
        if steps and entry is None:
            problems.append(
                f"{len(steps)} step object(s) but no keyRegistry entry — register "
                f"{stem!r}'s step/expect keys in {os.path.relpath(SCHEMA_PATH, ROOT)}"
            )
        elif entry:
            step_keys, expect_keys = set(entry["step_keys"]), set(entry["expect_keys"])
            for path, obj in steps:
                for k in obj:
                    if k not in step_keys:
                        problems.append(f"{path}: unregistered step key {k!r}{suggest(k, step_keys)}")
                exp = obj.get("expect")
                if isinstance(exp, dict):
                    for k in exp:
                        if k not in expect_keys:
                            problems.append(f"{path}.expect: unregistered expect key {k!r}{suggest(k, expect_keys)}")

        if problems:
            failures += 1
            print(f"  ✗ {stem}")
            for p in problems:
                print(f"      {p}")
        else:
            n = len(steps)
            print(f"  ✓ {stem} ({n} step object(s))" if n else f"  ✓ {stem} (no step objects — data table)")

    print()
    if failures:
        print(f"✗ {failures}/{len(files)} vector file(s) failed the lint")
        return 1
    print(f"✓ all {len(files)} vector files conform to the envelope + key registry")
    return 0


if __name__ == "__main__":
    sys.exit(main())
