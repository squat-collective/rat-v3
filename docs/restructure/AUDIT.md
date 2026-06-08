# RAT v3 — Restructure Audit (keep / good-to-keep / remove)

> Working document for the "reduce to the essential, make it professional" restructure.
> Produced 2026-06-08 on branch `phase-10` (Phase 10 consolidated, `main` untouched).
> **Analysis only — nothing has been deleted.** This is the decision input.

## TL;DR

The **committed** codebase is already disciplined and lean (~71k LOC tracked, six-thing
core held). Most of the "191M on disk" is **untracked build artifacts and caches** — not
in git. The real reduction opportunities, in priority order:

1. **Untracked artifact cleanup** — `bin/` (47M) + `dist/` (12M) + `core/bin/rat` (20M) +
   all `__pycache__`/`node_modules`/`.venv`. **~80M of disk, zero git/code loss.** Already
   gitignored; just needs `make clean` + a `clean` target.
2. **Two dead SDK languages** — `contracts/sdks/typescript/` and `contracts/sdks/rust/`
   have **zero consumers** in the entire repo. ~84 tracked files (~29% of `contracts/`),
   regenerable from proto any time. Highest-value *tracked* cut.
3. **Stale roadmap** — `current.md` documents Phase 9 while the repo is two phases ahead.
   The project's own rule: *"a stale roadmap is worse than no roadmap."* (Content fix, not a cut.)
4. **Superseded pipeline artifacts** — `platform/project/` + `platform/pipelines/` +
   `examples/strategy/sql-pipeline-py` are the pre-dbt-runner generation, explicitly
   superseded by ADR-021. ~7 files + 1 plugin.
5. **Process-doc sprawl** — the Q02 *simulated* external-review kit + per-reviewer `board/`
   raw outputs (~11 review files) are parked process infra → archive, don't delete.
6. **8 "Proposed" ADRs whose code shipped** — status metadata sweep → Accepted.

Realistic tracked-file reduction: **~100+ files** with zero functional regression, plus
**~80M disk** reclaimed.

---

## Region-by-region

### 1. `core/` — the Go core ✅ already lean

83 tracked files, ~12.1k LOC. **No dead code.** Every package maps to one of the six
things or its test harness. Verdict: keep essentially all of it.

- **MUST KEEP:** gateway, registry (+verified), reconciler, lease, manifest,
  deploymentruntime, supervisor, conformance, all of `cmd/rat` (daemon + authoring +
  hub + control), `cmd/ratctl`, `client/`, `composition/`, the testplugins.
- **GOOD TO KEEP (optional cut):** `core/arrowticket/` (4 files, ~476 LOC) — a security
  spike with **no production callers**; it's living-spec for the Arrow bulk-leg (D2/PU-1).
  Keep as documentation-as-code, or cut for ~4% LOC if that decision is captured elsewhere.
- **COULD REMOVE:** `core/bin/rat` (untracked 20M binary — just `rm`); the standalone
  `testplugins/stateplugin/Dockerfile` duplicates the daemon image's inline build.

### 2. `contracts/` — proto + 4 SDKs ⚠️ two dead languages

291 tracked files. The proto (25 `.proto` + CONTRACT.md), schema, and conformance vectors
are the load-bearing source of truth. The four committed SDKs (ADR-006) are the bulk.

| SDK | files | consumers | verdict |
|---|---|---|---|
| **Go** | 53 | 5+ example plugins + `ratplugin` helper + plugin-base image | **KEEP** |
| **Python** | 53 | 9+ plugins/platform + `rat.plugin`/`rat.contrib` + plugin-base image | **KEEP** |
| **TypeScript** | 45 | **none** (vscode-rat uses plain REST, no protobuf) | **REMOVE** |
| **Rust** | 39 | **none** (`Dockerfile.rust` itself says "never compiled, no plugins") | **REMOVE** |

- **COULD REMOVE:** `contracts/sdks/typescript/` (45), `contracts/sdks/rust/` (39),
  `contracts/buf.gen.python.yaml` (dead config — bypassed by `Dockerfile.python`, still
  points at BSR which ADR-018 eliminated). The codegen Dockerfiles for TS/Rust go too.
  **All regenerable from `contracts/proto/` via `make gen-sdks` when a plugin author appears.**
- **Note:** keeps the letter of ADR-006's four-language promise only by committing SDKs
  nothing exercises — removing them is consistent with "reduce to essential"; proto stays.
- **Stale:** `contracts/README.md` still calls SDKs "git-ignored build artifacts" (wrong
  post-ADR-006) and references the old `buf.gen.yaml` name. One-pass fix.

### 3. `examples/` — 47 plugins across 21 axes ✅ mostly justified

Most multi-impl axes are legitimate ADR-003 conformance work (data-plane axes need 2
references; several earn a 3rd "real backend"). Genuinely lean for what it proves.

- **COULD REMOVE (confirmed):** `examples/strategy/sql-pipeline-py` — **explicitly
  superseded** by the dbt-runner (roadmap says so verbatim); no harness test, no ADR-003 role.
- **COULD REMOVE (conditional — tied to the data-dev-plane experiment lifecycle):**
  `strategy/incremental-embed-py`, `catalog/ducklake-py`, `engine/duckdb-ml-py`,
  `storage/minio-s3` — all 🛰️ exploratory. Keep while `experiments/data-dev-plane` is
  "paused-but-alive"; remove (or graduate to their own repo) when it's formally retired.
- **COULD REMOVE (low priority):** `examples/state/postgres-py` — 4th state backend, no
  README/harness, no ADR-003 role the `sqlite-py` round-2 ref doesn't cover.
- **MUST KEEP:** the canonical reference per axis + the ADR-003 pairs + `dbt-duckdb`
  (the current pipeline runner) + `composition/` (the cross-axis gate) + `bench/latency-go`.
- Max reduction if data-dev retired: ~6 plugins (~13%, 40-50 files). Cruft (`__pycache__`,
  `node_modules`) is already gitignored — **no tracked cruft**.

### 4. `platform/` + `experiments/` ⚠️ superseded generations linger

`platform/` carries **three generations** of run infra (attach-mode → launch-mode →
socket-mount) plus pre-dbt SQL artifacts. `experiments/data-dev-plane/` is a frozen,
self-contained, complete artifact.

- **COULD REMOVE:** `platform/project/` (5 SQL files) + `platform/pipelines/medallion.yaml`
  — the old `sql-pipeline-py` medallion, superseded by `platform/dbt-project/`. Nothing in
  the active run paths reads them. Their presence implies the bespoke-SQL approach is still live.
- **CLEANUP (not delete):** `platform/run.py` docstring still says "sql-pipeline strategy."
- **CANONICAL (keep):** `compose.infra.yaml` + `plugins.yaml` (launch-mode, the ADR-022
  direction) + `run-socket-mount.sh` (furthest-proven) + `bff.py` + `manifests/` +
  `dbt-project/` + `landing/`. Attach-mode `compose.yaml`/`plane.yaml` is GOOD TO KEEP as
  the simpler local-demo path (but clean the stale runner wiring).
- **experiments/:** keep as the frozen intellectual record (README is cited from ADRs +
  roadmap; runners are Makefile-wired regression checks). Candidate to **archive** as a
  unit if you want `examples/` to shed the 4 exploratory plugins it spawned.

### 5. `docs/` / `reviews/` / `roadmap/` ⚠️ stale roadmap + status drift + process sprawl

- **`roadmap/current.md` — STALE, HIGH severity.** Documents "Phase 9 sealed" as current;
  Phase 10 (ADRs 029/031/033/034/035 + RatFS + identity + hub TLS) is absent. `phases.md`
  table stops at Phase 7 (missing 8/9/10). `backlog.md` has ~150 lines of `✅ DONE` items
  that belong in `done.md`. **Fix content — don't cut.** (`done.md` is accurate + current.)
- **8 ADRs marked "Proposed" whose code shipped** (021–026, 028 + ADR-017 whose punch-list
  is complete) → status sweep to Accepted. Largest professionalism gap in `docs/`.
- **ADR-032** (Deferred, filesystem axis) — only genuine archive candidate among ADRs.
- **reviews/ — archive ~11 of 29 files** (zero knowledge loss; synthesis captures them):
  - the Q02 **simulated** external-review kit: `Q02-outreach-note`, `Q02-reviewer-shortlist`,
    `Q02-brief-{architect,ecosystem,security,sre}` (parked — real Q02 set aside for a solo dev).
  - `reviews/board/` (5 per-reviewer raw files) — synthesized into `08-post-freeze-board-review.md`.
  - **Keep:** `00-synthesis` + `01–05` (founding adversarial set), `06–10` (phase gates),
    `11-q02-*` simulated outputs (drove the punch-list), `Q02-tracker` (has the synthesis).
- **MUST KEEP untouched:** `vision.md`, `overview.md`, all Accepted ADRs, `.claude/`
  (tight, every file load-bearing), `marketplace/` (live index), `research/`, the live
  `scripts/` quality gates.

---

## Proposed sequencing (when you say go)

| # | Action | Risk | Win |
|---|---|---|---|
| A | `make clean` untracked artifacts + add a `clean` target | none | ~80M disk |
| B | Fix `roadmap/current.md` + `phases.md` + strip done items from `backlog.md` | none | orientation |
| C | ADR status sweep (Proposed→Accepted for shipped ADRs) | none | professionalism |
| D | Remove TS + Rust SDKs + dead `buf.gen.python.yaml`; fix `contracts/README` | low | ~84 files |
| E | Remove `sql-pipeline-py` + `platform/project/` + `pipelines/` (superseded) | low | ~7 files + 1 plugin |
| F | Archive the Q02 process kit + `reviews/board/` → `reviews/archive/` | none | ~11 files |
| G | Decide data-dev-plane experiment: keep / archive-as-unit / graduate | — | up to ~6 plugins |
| H | (optional) cut `core/arrowticket/` if captured elsewhere | low | ~476 LOC |

A–F are safe, high-confidence, no functional regression. G is a judgment call. H is optional.
