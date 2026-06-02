"""One-shot generator for contracts/conformance/engine-embed-v1.json.

Emits deterministic golden vectors for embed(text,'hash-256') in a compact SPARSE
form (only nonzero buckets), plus metadata the harness re-derives and asserts. Run
once; the output is frozen. NOT part of the test suite (underscore-prefixed)."""

import json
import os

from embed import HASH_DIM, embed_hash256

INPUTS = [
    "great battery life and screen",
    "battery dies too fast",
    "the screen is gorgeous and bright",
    "",                       # empty -> zero vector
    "Battery BATTERY battery",  # case-folding + repetition (accumulates)
]


def sparse(vec):
    return {str(i): round(x, 12) for i, x in enumerate(vec) if x != 0.0}


def main():
    cases = []
    for text in INPUTS:
        v = embed_hash256(text)
        cases.append({"text": text, "model": "hash-256", "nonzero": sparse(v)})
    doc = {
        "axis": "engine/embed/v1",
        "model": "hash-256",
        "dim": HASH_DIM,
        "note": "deterministic feature-hashing golden vectors; embed(text,'hash-256') "
                "MUST reproduce these exactly (README §10 Q7). Dense vector = dim floats, "
                "zero except at the listed bucket indices.",
        "cases": cases,
    }
    out = os.path.join(os.path.dirname(__file__), "..", "..", "..",
                       "contracts", "conformance", "engine-embed-v1.json")
    out = os.path.abspath(out)
    with open(out, "w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2)
        f.write("\n")
    print("wrote", out, "with", len(cases), "cases")


if __name__ == "__main__":
    main()
