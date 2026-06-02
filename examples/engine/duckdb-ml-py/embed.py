"""The pluggable `embed` backend — the inference seam of the data-dev plane.

`embed(text, model)` resolves the `model` string to a backend and returns an
embedding vector (a Python list of floats; DuckDB binds it to FLOAT[]). The data
plane never changes when you swap backends — only the `model` argument does:

    model            backend                                  weights   notes
    ---------------- ---------------------------------------- --------- ---------------------
    hash-256         deterministic feature-hashing (stdlib)   none      golden vectors; zero-dep
    minilm           sentence-transformers all-MiniLM-L6-v2   downloads dim 384; CPU/GPU
    ollama:<m>       remote Ollama /api/embeddings (HAL-9000)  remote    LLM-grade, decoupled

Only `hash-256` is wired with no optional deps — it is the CI/demo default and the
source of the deterministic golden vectors (contracts/conformance/engine-embed-v1.json).
`minilm` and `ollama:*` are operator opt-ins (lazy-imported / HTTP); if their deps or
endpoint are absent the call raises a clear error rather than silently degrading.

This is the "ML is an engine extension, not an axis" call (experiment README §3): the
ML mechanism is a UDF inside the engine plugin, invoked as plain SQL through the
existing `rat://engine/v1/{execute,query}` capability — no new proto, no 19th axis.
"""

import hashlib
import math
import os
import re

HASH_DIM = 256  # the `hash-256` name == its dimensionality
_TOKEN = re.compile(r"[a-z0-9]+")


def _l2_normalize(v):
    n = math.sqrt(sum(x * x for x in v))
    if n == 0.0:
        return v
    return [x / n for x in v]


def embed_hash256(text: str):
    """Deterministic feature-hashing into HASH_DIM L2-normalized floats. Pure stdlib,
    so it is reproducible across machines and Python builds — the property that makes
    it usable as a frozen golden vector. Tokenization: lowercase `[a-z0-9]+` runs;
    each token is hashed once into a (bucket, sign) pair and accumulated."""
    v = [0.0] * HASH_DIM
    for tok in _TOKEN.findall((text or "").lower()):
        h = hashlib.sha256(tok.encode("utf-8")).digest()
        bucket = int.from_bytes(h[0:4], "big") % HASH_DIM
        sign = 1.0 if (h[4] & 1) else -1.0
        v[bucket] += sign
    return _l2_normalize(v)


_MINILM = None  # lazily-loaded sentence-transformers model


def embed_minilm(text: str):
    """Real semantic embeddings via sentence-transformers all-MiniLM-L6-v2 (dim 384).
    Lazy-loaded so the plugin starts (and the hash-256 path works) without the model
    or its weights present. Opt-in: requires `sentence-transformers` installed."""
    global _MINILM
    if _MINILM is None:
        try:
            from sentence_transformers import SentenceTransformer  # noqa: heavy, opt-in
        except ImportError as e:  # pragma: no cover — opt-in path
            raise RuntimeError(
                "embed(model='minilm') needs sentence-transformers installed "
                "(operator opt-in; default model is 'hash-256')"
            ) from e
        _MINILM = SentenceTransformer("all-MiniLM-L6-v2")
    return [float(x) for x in _MINILM.encode(text or "")]


def embed_ollama(text: str, model: str):
    """LLM-grade embeddings via a remote Ollama server (e.g. HAL-9000) — decoupled
    inference that scales as its own fleet. `model` is `ollama:<name>`; the endpoint
    comes from $OLLAMA_URL (default http://localhost:11434). Opt-in: needs the network
    endpoint reachable. Kept dependency-light (urllib, no `requests`)."""
    import json
    import urllib.request

    name = model.split(":", 1)[1] if ":" in model else "nomic-embed-text"
    url = os.environ.get("OLLAMA_URL", "http://localhost:11434").rstrip("/") + "/api/embeddings"
    body = json.dumps({"model": name, "prompt": text or ""}).encode("utf-8")
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:  # pragma: no cover — opt-in path
        payload = json.loads(resp.read().decode("utf-8"))
    return [float(x) for x in payload["embedding"]]


def embed(text: str, model: str = "hash-256"):
    """Dispatch `model` to a backend. The whole ML surface is this one seam — every
    higher-level macro (semantic_search, classify, rag — README §3 growth path)
    composes on top of `embed` + the `vss` distance functions, all in SQL."""
    model = model or "hash-256"
    if model == "hash-256":
        return embed_hash256(text)
    if model == "minilm":
        return embed_minilm(text)
    if model.startswith("ollama:"):
        return embed_ollama(text, model)
    raise RuntimeError(f"unknown embed model {model!r} (have: hash-256, minilm, ollama:<m>)")
