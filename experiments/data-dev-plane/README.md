# The Data-Dev Plane — an exploratory end-to-end ML lakehouse on RAT v3

> **Status: EXPLORATORY. Nothing here is fixed.** This is a sandbox to try the RAT v3
> plugin model on *real assets, real compute, and a real UX* — not a spec to implement to
> the letter. Expect the shapes below (schemas, plugin boundaries, even which axes we use)
> to change as we learn from building. The goal is **learning what works**, not shipping.
>
> Branch: `phase-1-data-dev-plane`. Everything here is **additive** — no new frozen axis, no
> contract change, `make breaking` stays clean, the sealed `rat/2.0` surface is untouched.

---

## 0. Why this exists

RAT v3's last pre-unfreeze gate was the **Q02 external human review**. Tom is a **solo dev**
with no external-reviewer network, so that gate is impractical. This experiment is the
**practical substitute**, and it's a *stronger* validation anyway — it's the project's own
principle #8 made real:

> *"Test the deployment topology, not the feature. The test is: can a real pipeline be
> composed from plugins?"*

So instead of asking strangers whether the architecture is sound, we **compose a real,
scalable, end-to-end ML data workflow out of plugins** and find out by using it. If the
plugin model, the contracts, and the UX hold up under a genuine workload — storage to AI/ML
analysis, on remote object storage, through a real editor UI — that's the proof. Where they
*don't* hold up, that's the most valuable finding this project can get.

**What we're proving:** *the sealed core + the frozen contracts can carry a modern lakehouse
+ AI workload, scalably, with a real developer UX — and we can add AI/ML without changing one
byte of the frozen wire.*

---

## 1. The workflow we're building

A real ML pipeline, end to end, every box a plugin, on remote object storage:

```
 raw text data (S3/MinIO)
        │
        ▼  engine: clean / derive features (DuckDB SQL)
 reviews_clean ──────────────────────────────────────────────┐
        │                                                     │
        ▼  engine: embed(text) → FLOAT[]  (ML, a DuckDB UDF)  │ catalog: DuckLake
 + embedding column                                          │ snapshot + branch
        │                                                     │
        ▼  engine: CREATE INDEX … USING HNSW (vss)            │
 vector index ───────────────────────────────────────────────┘
        │
        ▼  query / 🔍 semantic search / classify   ──→   VS Code extension (UX)
```

Everything is mediated by the **sealed core** (capability authz C5 + audit C4 + the keystone
context-stamping that PU-2 just two-impl-conformed). Bulk Arrow/Parquet bytes flow
plugin↔S3 **out-of-band** (the bytes leg bypasses the core), so the core is never the
throughput bottleneck.

---

## 2. The stack at a glance

| RAT axis | plugin | new? | role |
|---|---|---|---|
| **storage** | `minio-s3` | 🆕 ✅ | remote S3-compatible object store; vends short-TTL, prefix+tenant-scoped STS creds (built — first 5c read/write split impl) |
| **catalog** (+format) | `ducklake-py` | 🆕 | [DuckLake](https://ducklake.select/docs/stable/) lakehouse: SQL metadata + Parquet/S3, snapshots, time-travel, ACID — **subsumes the `format` axis** |
| **engine + ML** | `duckdb-ml-py` | 🆕 | DuckDB + extensions: `ducklake`, `httpfs`/S3, `vss` (vectors), and an `embed()` UDF → **compute *and* AI, no new axis** |
| **strategy** | `incremental-embed-py` | 🆕 ✅ | a *real* ELT (built): watermark-incremental load → merge → embed-only-new → flush → snapshot; idempotent (C1) |
| **ui** | `vscode-rat` | 🆕 | a VS Code extension (client of the core via the generated **TypeScript SDK** — the ADR-018 connectionless codegen payoff) |
| deployment-runtime | `podman` | reuse | each plugin a container → horizontal scale |
| runtime | `subprocess-py` | reuse (maybe) | exec units if the strategy needs them |

Reuse where possible; the only genuinely new code is the five 🆕 plugins. Notably **no
`format` plugin** — DuckLake fills the catalog *and* format roles (see §4).

---

## 3. The big decision: **ML is an engine extension, not an axis**

We deliberately do **NOT** add a `kind: ml` axis or a new proto. AI/ML "analysis" is
*compute over data*, which is exactly what the `engine` axis already is
(`rat://engine/v1/query` → Arrow). So ML lives as **DuckDB extensions inside the engine
plugin**, invoked as plain SQL through the existing engine capability:

- **`embed(text VARCHAR, model VARCHAR) → FLOAT[]`** — a scalar UDF that calls the pluggable
  backend (local model / Ollama) and returns an embedding vector.
- **`vss`** — DuckDB's vector-similarity-search extension (HNSW index) for nearest-neighbour
  / semantic search (`array_cosine_distance`, `array_distance`).
- *(growth path — "other extensions based on this one")* further DuckDB macros/UDFs that
  **compose** on `embed` + `vss`: `semantic_search(query, k)`, `classify(text, model)`,
  `rag(question)`. All SQL, all in the engine plugin, all extensions — nothing in the core.

**Why this is the right call (not just the cheap one):**
- **Parsimony** — no 19th axis, no contract-surface change, the temptation ledger stays at 0.
- **A purer proof of the thesis** — *"we added AI/ML and didn't change one byte of the sealed
  wire."* Composition within the existing model beats expanding the model.
- **Native fit** — DuckDB extensions *are* the ML mechanism in this stack (`vss` exists;
  `embed` is a UDF). DuckLake + DuckDB + ML is one coherent DuckDB-centric world.

**Honest tradeoff (revisit if it bites):** C5 authorizes at `rat://engine/v1/query`
granularity, not a distinct `embed` capability, and the marketplace can't discover
"embedding providers" as a first-class capability. **If/when first-class ML discovery or
per-op authz matters, we promote `embed`/`predict` to a real `ml` axis** (the proto design is
already sketched in the conversation that produced this doc). For an exploratory
DuckDB-centric stack, extensions win.

---

## 4. Catalog — DuckLake (🦆) and why it subsumes `format`

[**DuckLake**](https://ducklake.select/docs/stable/) (DuckDB Labs, 2025) is a lakehouse
format that puts **all** table metadata — schema, snapshots, statistics, file lists — in a
**SQL database** (DuckDB / SQLite / Postgres / MySQL), with data as **Parquet on object
storage**. No JSON/Avro manifest sprawl; metadata ops are SQL transactions → ACID, snapshots,
and time-travel come for free. A client attaches it: `ATTACH 'ducklake:…' AS lake;`.

**Topology for us:**
- **Metadata** → a SQL catalog DB. Demo: SQLite or DuckDB file on a volume. Scale: Postgres
  (shared, concurrent).
- **Data** → Parquet on **S3/MinIO** (via the `minio-s3` vended creds).

**Why it subsumes the `format` axis:** in RAT's model, `catalog` = metadata/snapshots and
`format` = file-layout (write Parquet, resolve refs). DuckLake does *both*: DuckDB-via-the-
`ducklake`-extension writes the Parquet **and** records it in the catalog DB in one
transaction. So this stack has **no separate `format` plugin** — a deliberate simplification
that also demonstrates the axes aren't dogma (a real lakehouse format maps to catalog+format
together). *(If we later want to exercise the `format` axis explicitly, an Iceberg/Delta
format plugin re-introduces it.)*

**Mapping DuckLake → the RAT `catalog` axis** (to firm up against `catalog/v1` when building):

| RAT catalog RPC | DuckLake realization |
|---|---|
| `RegisterTable` | `CREATE TABLE lake.<t> (…)` |
| `CommitTable` | the DuckLake snapshot produced by the write transaction (record/tag it) |
| `GetTable` | resolve to current / a branch / `AT (VERSION => n)` (time-travel) |
| `CreateBranch` / `MergeBranch` | a thin layer over DuckLake snapshots (a branch = a named snapshot lineage; candidate: separate attached lake per branch, or a branch-tag column) |

> ⚠️ **Open design tension (resolve while building):** DuckLake *unifies* write+commit in one
> DuckDB transaction, while RAT *separates* `engine` (compute) from `catalog` (metadata). So
> "the engine writes data" and "the catalog commits a snapshot" are the same DuckLake
> transaction. Candidate resolutions, in §10.

---

## 5. The plugins, in detail

### 5.1 `minio-s3` — remote, scalable storage (`kind: storage`)

A Python gRPC `StorageService`. Implements `VendCredentials` and the 5c
`VendReadCredentials` / `VendWriteCredentials` (the read/write split we just added): it issues
**short-TTL, prefix-scoped, tenant-scoped** S3 credentials — a MinIO STS `AssumeRole` policy
on `s3://<bucket>/<tenant>/<prefix>`, read-only or read-write per the RPC. The engine then
uses those creds (DuckDB `CREATE SECRET … TYPE S3`) to read/write Parquet on S3 **directly** —
the bytes never touch the core (the D3 model; storage-cred isolation is vector-tested).

```yaml
# examples/storage/minio-s3/plugin.yaml
api_version: rat/1
kind: storage
metadata: { name: rat-storage-minio, version: 0.1.0,
            description: "S3/MinIO remote storage; vends scoped, short-TTL creds" }
provides:
  - { capability: "rat://storage/v1/vend-credentials" }
  - { capability: "rat://storage/v1/vend-credentials-read" }   # 5c
  - { capability: "rat://storage/v1/vend-credentials-write" }  # 5c
resources: { requests: { cpu: "100m", memory: "128Mi" } }
```

**Scalable because:** S3/MinIO is the horizontally-scalable remote store; many engine
containers read/write the same bucket concurrently with independently-scoped creds.

### 5.2 `ducklake-py` — the catalog (`kind: catalog`)

A Python gRPC `CatalogService` backed by a DuckLake. Drives DuckDB-with-the-`ducklake`-
extension to manage the lake's metadata: attach the catalog DB, create/register tables, record
snapshots on commit, resolve refs (incl. time-travel + branch), and the branch model. Owns the
*metadata* side of the lake; the engine owns *compute*. Both attach the **same** DuckLake
(shared metadata DB + the same S3 data via `minio-s3` creds).

```yaml
# examples/catalog/ducklake-py/plugin.yaml
api_version: rat/1
kind: catalog
metadata: { name: rat-catalog-ducklake, version: 0.1.0,
            description: "DuckLake lakehouse catalog: SQL metadata + Parquet on S3" }
provides:
  - { capability: "rat://catalog/v1/register-table" }
  - { capability: "rat://catalog/v1/commit-table" }
  - { capability: "rat://catalog/v1/get-table" }
  - { capability: "rat://catalog/v1/create-branch" }
  - { capability: "rat://catalog/v1/merge-branch" }
# (exact capability set to be aligned with contracts/proto/rat/catalog/v1 + ADR-010)
resources: { requests: { cpu: "250m", memory: "256Mi" } }
```

### 5.3 `duckdb-ml-py` — the engine **and** the ML (`kind: engine`)

The heart of the stack. A Python gRPC `EngineService` (the existing `duckdb-py` ref, extended)
running DuckDB with extensions loaded: **`ducklake`** (read/write lake tables), **`httpfs`/S3**
(read/write S3 with the vended creds), **`vss`** (HNSW vector index), and a registered
**`embed()`** scalar UDF. It serves `rat://engine/v1/{execute,query,preview}` — and ML is just
SQL inside those.

**The ML surface (SQL, not proto):**

```sql
-- embedding: a scalar UDF resolving `model` to a pluggable backend (see table below)
embed(text VARCHAR, model VARCHAR) -> FLOAT[]        -- e.g. embed('great battery', 'minilm')

-- vector search (vss extension):
CREATE INDEX rev_hnsw ON lake.reviews USING HNSW (embedding) WITH (metric = 'cosine');
SELECT id, text, array_cosine_distance(embedding, embed(:q, 'minilm')) AS dist
FROM lake.reviews ORDER BY dist LIMIT :k;            -- semantic search

-- growth path (further extensions composing on embed + vss):
--   classify(text VARCHAR, model VARCHAR) -> VARCHAR        (sentiment / label, e.g. via Ollama)
--   semantic_search(query VARCHAR, table VARCHAR, k INT)    (macro over the SELECT above)
--   rag(question VARCHAR) -> VARCHAR                         (retrieve-then-prompt)
```

**The pluggable `embed` backend** (resolved from the `model` string — the inference seam):

| `model` | backend | weights? | scales by | use |
|---|---|---|---|---|
| `hash-256` | deterministic feature-hashing (stdlib) | none | — | **golden vectors** (deterministic) + zero-dep demo |
| `minilm` | sentence-transformers `all-MiniLM-L6-v2` (dim 384) | downloads once | CPU/GPU per container | real semantic embeddings |
| `ollama:<m>` | POST a remote Ollama `/api/embeddings` (e.g. **HAL-9000**) | remote | a separate inference fleet | LLM-grade, decoupled inference |

Default for CI/demo: **`hash-256`** (runs anywhere). `minilm` and `ollama:*` are operator-config
opt-ins — set via the plugin's config (e.g. `EMBED_BACKENDS`, `OLLAMA_URL`). **The data plane
never changes when you swap backends.**

```yaml
# examples/engine/duckdb-ml-py/plugin.yaml
api_version: rat/1
kind: engine
metadata: { name: rat-engine-duckdb-ml, version: 0.1.0,
            description: "DuckDB engine + ducklake/httpfs/vss + embed() UDF (compute + AI)" }
provides:
  - { capability: "rat://engine/v1/execute" }
  - { capability: "rat://engine/v1/query" }
  - { capability: "rat://engine/v1/preview" }
resources: { requests: { cpu: "1", memory: "1Gi" }, limits: { cpu: "4", memory: "4Gi" } }
```

### 5.4 `incremental-embed-py` — a *real* strategy (`kind: strategy`)

Not a toy `full-refresh`. A realistic incremental embedding ELT that the strategy axis drives.
Per run (idempotent via the run id; branch-isolated):

1. **Watermark** — read only source rows newer than the last committed DuckLake snapshot
   (incremental, not a full reload).
2. **Transform** — DuckDB SQL: clean + derive features into a staging set.
3. **Merge** — upsert into the target DuckLake table on a business key
   (`MERGE INTO … ON id`), idempotent under at-least-once retry.
4. **Embed** — compute `embed(text, 'minilm')` **only for new/changed rows** → write the
   `embedding FLOAT[]` column.
5. **Index + snapshot** — refresh the `vss` HNSW index; commit a new DuckLake snapshot
   (= the catalog `CommitTable`).

Exercises incrementality, upsert, idempotency (C1), embeddings, vector indexing, and
snapshotting — a genuine pipeline pattern, not a hello-world.

### 5.5 `vscode-rat` — the UX (a VS Code extension, `kind: ui`)

A VS Code extension that's a **client of the core's API gateway via the generated TypeScript
SDK** (`contracts/sdks/typescript`, now connectionless per ADR-018). It's the developer's
window into the data-dev plane:

| surface | what it does | core calls |
|---|---|---|
| **Sidebar tree** | browse DuckLake catalog → tables → snapshots/branches | `catalog.GetTable` / list |
| **Run pipeline** | a command/button that triggers the strategy; live status | strategy invoke + reconciler `Status()` |
| **Query editor** | run SQL against the engine; results in a grid; **Preview** a table | `engine.Query` / `engine.Preview` |
| **🔍 Semantic search** | a box → `embed()` the query → `vss` nearest-neighbour → ranked rows | `engine.Query` (SQL above) |
| **Plugin health** | reconciler view: Healthy / Degraded per plugin | reconciler `Status()` |

How it connects: the extension (TypeScript) instantiates the TS SDK client against the core's
gateway endpoint, and every action is a capability call the core routes + audits. It is a
`ui`-plugin *in spirit* (a UI client of the platform), and the cleanest demonstration of the
multi-UI story (CLI / web-portal / **VS Code**) the vision names.

> Lives in `examples/ui/vscode-rat/` (a standard VS Code extension: `package.json` +
> `src/extension.ts` + views/commands). Build last — it sits on top of a working data plane.

---

## 6. The composition — the exact end-to-end flow

The `incremental-embed` strategy drives this; the core mediates every control hop; bulk bytes
move plugin↔S3 out-of-band. Concretely, in DuckDB/DuckLake SQL the engine executes:

```sql
-- 0. creds from minio-s3 (vended per hop, scoped to tenant+prefix+mode)
CREATE SECRET s3 (TYPE S3, KEY_ID :k, SECRET :s, ENDPOINT 'minio:9000', URL_STYLE 'path');
ATTACH 'ducklake:sqlite:/meta/catalog.db' AS lake (DATA_PATH 's3://rat/<tenant>/lake/');

-- 1. transform (incremental: only new source rows)
CREATE OR REPLACE TEMP TABLE staging AS
  SELECT id, lower(trim(text)) AS text, rating
  FROM read_csv('s3://rat/<tenant>/raw/reviews.csv')
  WHERE ingested_at > (SELECT max(_ingested_at) FROM lake.reviews);   -- watermark

-- 2. merge (upsert, idempotent)
MERGE INTO lake.reviews t USING staging s ON t.id = s.id
  WHEN MATCHED THEN UPDATE SET text = s.text, rating = s.rating, embedding = NULL
  WHEN NOT MATCHED THEN INSERT (id, text, rating, _ingested_at) VALUES (s.id, s.text, s.rating, now());

-- 3. embed only the rows that need it (ML, via the UDF → pluggable backend)
UPDATE lake.reviews SET embedding = embed(text, 'minilm') WHERE embedding IS NULL;

-- 4. index + (DuckLake auto-snapshots the transaction = catalog CommitTable)
CREATE INDEX IF NOT EXISTS rev_hnsw ON lake.reviews USING HNSW (embedding) WITH (metric='cosine');
```

Then, interactively (from the VS Code extension):

```sql
-- 🔍 semantic search
SELECT id, text, rating,
       array_cosine_distance(embedding, embed('how is the battery life', 'minilm')) AS dist
FROM lake.reviews ORDER BY dist LIMIT 10;
```

The RAT framing: the strategy calls `engine.Execute` for steps 1–4 and `catalog.CommitTable`
to seal the snapshot; the VS Code search is an `engine.Query`. Each is a capability the core
authorizes (C5) and audits (C4), with the keystone context-stamping (PU-2) on every hop.

---

## 7. Scalability — concretely

- **Remote object storage** (MinIO/S3) — data lives off-box, horizontally scalable; the
  metadata DB scales to Postgres for concurrent writers.
- **Containerized plugins** (podman deployment-runtime) — each plugin is an independent
  container; the reconciler manages lifecycle/health; scale = more containers.
- **Bytes leg bypasses the core** — bulk Parquet/Arrow moves plugin↔S3 directly; the core
  routes only small control metadata, so it's not a throughput bottleneck (the whole reason
  the data-plane-bypasses-control design exists).
- **Pluggable inference** — `embed`/`classify` backends (local model / remote model server /
  **Ollama fleet on HAL-9000**) scale independently of the data plane.
- **DuckLake snapshots + branches** — isolated parallel/experimental runs; cheap time-travel.

---

## 8. How we want to *use* it (the UX intent)

The dev loop we're trying to make feel good:

1. Open VS Code → the **RAT Data-Dev** sidebar shows the DuckLake catalog (tables, snapshots).
2. Point the pipeline at a **real dataset** (your assets — see §9). Hit **Run pipeline**.
3. Watch it go: transform → merge → embed → snapshot, with live plugin health.
4. **Explore**: write SQL in the query editor; **🔍 semantic-search** the text ("find reviews
   about X"); preview tables; time-travel to a past snapshot; branch for an experiment.
5. Re-run later → it's **incremental** (only new rows transform + embed). Branch, compare,
   merge.

The whole thing on containers + remote S3, swappable inference backend. **The exploratory
question is whether this actually feels like a real, scalable data-dev experience — and where
the plugin model, the contracts, or the UX get in the way.** That's the finding we want.

---

## 9. Real assets (the dataset)

The point is to run on **real data**, not synthetic. A good starting asset is a **text corpus
with a label** — product/app reviews (text + rating), or a documents/notes corpus for
RAG-style retrieval. Properties we want: enough rows to make embeddings + `vss` meaningful
(10k–1M), a text column to embed, optionally a label to classify. **Swap in whatever real data
is on hand** — the pipeline is column-driven (`text`, `id`, an optional label), so pointing it
at a new CSV/Parquet on S3 is a config change, not a code change.

---

## 10. Open questions / things to revisit (this is exploratory)

### Findings from building step 2 (the DuckDB heart) — 2026-06-02

The local end-to-end works; the build surfaced concrete frictions worth recording (this
is exactly the "where they *don't* hold up" value §0 is after):

- **F1 — DuckLake rejects DuckDB's fixed-size `ARRAY` (`FLOAT[N]`).** That is *the* type
  `vss`'s HNSW index requires. So embeddings are stored as a **variable `LIST` (`FLOAT[]`)**
  and cast to `FLOAT[N]` at query time for `array_cosine_distance`. Consequence: **you cannot
  both lake-store an embedding column AND HNSW-index it in place.** Brute-force cosine works
  directly on the lake (what `run-local.py` does); an HNSW *index* needs a **derived (non-lake)
  fixed-array table** materialized from the lake — i.e. the ANN index is a derived structure,
  not lake state. (Partly answers Q2/Q7 below; reframes "vector index" as a build-from-lake
  artifact.)
- **F2 — list-returning UDFs need `numpy`.** DuckDB marshals Python list results via numpy, so
  the `embed()` UDF requires numpy even though the engine is otherwise Arrow-native. Added to
  the engine's `requirements.txt` and the conformance dep union.
- **F3 — DuckLake metadata sqlite is single-writer.** The engine *and* the catalog attach the
  same DuckLake; a catalog connection held open **locks out the engine's write COMMIT**
  ("database is locked"). Fix for the local sqlite demo: the catalog opens **short-lived**
  read connections (the engine is idle at read time). ✅ **Resolved at remote scale by
  Postgres metadata** (step 3): with `ducklake:postgres:…`, engine + catalog are genuine
  concurrent writers, no lock. The catalog store now takes an `extensions` list so it loads
  `httpfs`/`postgres` for the remote lake.
- **F4 — DuckLake inlines small writes** into the metadata DB; Parquet materializes only on
  flush. ✅ **Resolved:** the remote pipeline calls **`CALL ducklake_flush_inlined_data('lake')`**
  to force the Parquet out to S3 (verified: files land under `s3://rat/<tenant>/lake/`).
- **F6 — the catalog needs NO S3 credentials.** At remote scale the catalog resolves snapshots
  + table existence from **Postgres metadata only** — it never touches bytes — so it attaches
  the lake (with the `s3://` data path) without an S3 secret. This is the engine/catalog split
  (bytes vs metadata) falling out *cleanly* in practice, and it sharpens least-privilege:
  the catalog plugin never even holds storage creds.
- **F7 — STS cred isolation is real, and least-privilege works.** MinIO `AssumeRole` with an
  inline policy scoped to `s3://bucket/<tenant>/<prefix>/*` gives creds that physically cannot
  cross the tenant boundary (read `acme/*` ok, `globex/*` denied) — and READ-vended creds are
  denied writes. The 5c read/write split is enforced by real object-store policy, not just by
  the RAT capability layer.
- **F8 — a strategy in a DuckLake world writes through the ENGINE, not a format plugin, and
  addresses tables by lake-qualified name.** Because DuckLake subsumes `format`, the
  incremental-embed strategy `requires` no `format` capability — it composes `engine.execute`
  (CTAS/stage/merge/embed/flush) + `catalog.commit-table`. It stays plugin-agnostic in
  *binding* (capability URIs, no names) but is DuckLake-aware in *addressing* (`<alias>.<id>`
  in SQL, since the engine attaches the lake) — vs the `format.scan` indirection the generic
  full-refresh/scd2 strategies use. A clean illustration that the axes aren't dogma. Also:
  the **watermark is computed server-side** (a subquery over the target's max), so the
  strategy needs no Arrow round-trip to read it — pure `execute` calls + the final snapshot.
- **F5 — `snapshot_time` pulls a `pytz` dep.** Selecting the timestamp column from
  `lake.snapshots()` triggers a timestamptz conversion needing pytz. The catalog only selects
  `snapshot_id`, so it stays pytz-free.

### Still open

1. **Catalog/engine boundary in a DuckLake world** (§4 tension). ✅ **Resolved by building
   (b):** engine + catalog stay separate, both attach the lake, the engine's write IS the
   snapshot (returned in `WriteResult.snapshot_id`) and the catalog `CommitTable` records it;
   `GetTable` resolves the real snapshot from `lake.snapshots()`. Works end-to-end. (a)/(c)
   not needed. The only residual is F3's single-writer discipline (→ Postgres at scale).
2. **Branch model** — map RAT branches onto DuckLake snapshots: branch-tag column vs separate
   attached lake vs snapshot lineage. Still needs a spike — the catalog ships a **thin tracker**
   (branch tips in its own sqlite) so the surface is complete while the model is decided. This
   is why the catalog is on a `selftest.py`, not yet in the frozen `catalog-v1` golden suite.
3. **`format` axis** — subsumed by DuckLake here. Keep it that way, or add an Iceberg/Delta
   format plugin to exercise the axis? (Lean: subsumed, simpler.)
4. **ML granularity** — extensions vs a first-class `ml` axis (§3). Revisit if discovery/authz
   on `embed` specifically starts to matter.
5. **Default embed backend** — `hash-256` everywhere, or wire straight to HAL-9000 Ollama for
   the real demo? (Lean: `hash-256` default for portability + golden vectors; `ollama:*` for
   the real run. *Open per Tom.*) The seam is built (`embed.py` dispatch); only config changes.
6. **Where the strategy runs** — in-process vs a `subprocess`/`podman` unit; how the reconciler
   schedules pipeline runs.
7. **Conformance** — ✅ **Done for `embed`:** [`engine-embed-v1.json`](../../contracts/conformance/engine-embed-v1.json) freezes deterministic `embed(hash-256, …)` golden vectors; the engine
   harness asserts them (dim + exact nonzero buckets + L2-norm). Vector-search/HNSW conformance
   is gated on the F1 derived-index question.
8. **UX scope** — how much of VS Code's API to lean on (tree views, webview grid, notebooks?).

---

## 11. Build order (for a fresh session)

1. **This doc** ✅ — the agreed (changeable) shape.
2. **`ducklake-py` catalog + `duckdb-ml-py` engine** ✅ **DONE** — the DuckDB heart: attach a
   DuckLake, the `embed()` UDF + `vss`, `engine.{execute,query,preview}`,
   `catalog.{register,commit,get}`. A *local* (no S3) end-to-end transform→embed→search runs
   green over real gRPC: [`run-local.py`](run-local.py) / `make data-dev-local`. The engine
   joins the conformance suite (engine-real-v1 + a new embed golden, [`engine-embed-v1.json`](../../contracts/conformance/engine-embed-v1.json)); the catalog has a `selftest.py`
   (frozen catalog-v1 parity deferred to the branch-model spike). Findings folded into §10.
3. **`minio-s3` + S3 wiring** ✅ **DONE** — the [`minio-s3`](../../examples/storage/minio-s3/)
   storage plugin vends short-TTL, tenant+prefix-scoped **STS** creds (first impl of the 5c
   read/write split); the engine reads/writes Parquet on **S3/MinIO** with them while DuckLake
   metadata moves to **Postgres**. [`run-remote.py`](run-remote.py) / `make data-dev-remote`
   runs the SAME flow distributed — **search distances byte-identical to local** (the data
   plane is unchanged when storage goes remote), Parquet lands on S3, D3 cross-tenant isolation
   holds. Stack: [`compose/compose.yaml`](compose/compose.yaml) or the dependency-free
   `scripts/data-dev-remote.sh`. Resolves findings F3 + F4 (see §10).
4. **`incremental-embed-py` strategy** ✅ **DONE** — the real ELT (§5.4) as a `kind: strategy`
   plugin ([`examples/strategy/incremental-embed-py`](../../examples/strategy/incremental-embed-py/))
   composing capabilities through the invoke gateway (names no concrete plugin).
   [`run-strategy.py`](run-strategy.py) / `make data-dev-strategy` proves it across 3 runs:
   run 1 embeds the full corpus, run 2 embeds **only the newly-landed delta**
   (incrementality), run 2 replay embeds **0** (idempotent, C1). Watermark is server-side,
   merge is an upsert, embed touches only new rows. Notably requires **no `format`
   capability** — the engine writes the lake directly (finding F8, §10).
5. **The composition** — a runner + a `make data-dev-plane` target that boots the stack
   (podman) and runs the pipeline on a real dataset.
6. **`vscode-rat`** — the VS Code extension on top, via the TS SDK.

Land each on a `phase-1-<slug>` sub-branch, `make breaking` clean (it will be — all additive),
and keep this doc fresh as the shape changes.

---

## 12. References

- DuckLake — https://ducklake.select/docs/stable/
- DuckDB `vss` (vector similarity search) — DuckDB extension docs
- RAT axes this reuses: `contracts/proto/rat/{engine,catalog,storage,strategy,ui}/v1/` +
  their `CONTRACT.md`
- Why ML-as-extension is safe: the sealed surface is `rat/2.0`; this adds plugins + SQL
  extensions only — no proto, no `make breaking` impact.
- The pre-unfreeze gate this experiment substitutes for: `reviews/Q02-tracker.md`,
  ADR-017 (PU-2 keystone conformance just landed).
