# RAT v3 — Target Structure (the professional end-state)

> Companion to [`RESTRUCTURE-AUDIT.md`](AUDIT.md). This is the *design* we cut
> toward. Produced 2026-06-08 on `phase-10`. **Decisions locked (2026-06-08); not yet applied.**
>
> **Locked choices:** `examples/`→`plugins/` (yes) · `research/`→`docs/research/` (yes) ·
> `reviews/` stays top-level with `reviews/archive/` (the 154-ref move is not worth it) ·
> `vscode-rat` leaves with the data-dev extraction (main-repo UI = `vscode-platform`).

## Design principles

1. **Four code/product domains, clearly named.** A reader should see the architecture in
   the top-level tree: *the wire* (`contracts/`), *the control plane* (`core/`), *the
   plugins* (`plugins/`), *the assembled product* (`platform/`). That IS the thesis —
   "the core does six things, everything else is a plugin" — made visible.
2. **One home for durable knowledge.** Vision, architecture, ADRs, reviews, research,
   conversations are all *documentation*; they belong under `docs/`.
3. **Operational state stays at the surface.** `roadmap/` and `ideas/` are *live working
   state* (read every session, written every session) — they stay top-level by design, per
   the project's own discipline. They are not archival docs.
4. **Don't bulldoze the cross-link web.** `reviews/` is referenced from **154 files**,
   `core/` from 116, `contracts/` from 129. Moves that break hundreds of institutional-memory
   links are a cost, not a win. Where a move is expensive, it must earn its keep — and we do
   it with a mechanical link-rewrite pass, not by hand.
5. **No build output in the tree.** `bin/`, `dist/`, caches → gitignored + a `make clean`.
6. **Standard OSS hygiene.** A repo this mature should have `LICENSE`, `CONTRIBUTING.md`,
   and a `README.md` that is a *repository map*, not a vision essay (that's `docs/vision.md`).

## Target tree

```
rat/
├── README.md               # ← rewrite: what it is · quickstart · repository map · status
├── CLAUDE.md               # AI working agreement (keep)
├── CONTRIBUTING.md         # NEW — how to add a plugin / ADR / run the gates
├── LICENSE                 # NEW
├── Makefile  .gitignore  .claude/
│
│   ── ① THE WIRE ───────────────────────────────────────────────
├── contracts/              # proto (source of truth) · schema · conformance · codegen
│   ├── proto/  schema/  conformance/  codegen/  examples/
│   └── sdks/               # Go + Python ONLY (TS + Rust dropped — zero consumers)
│
│   ── ② THE CONTROL PLANE ──────────────────────────────────────
├── core/                   # the six-thing Go core (already lean — keep as-is)
│
│   ── ③ THE PLUGINS ────────────────────────────────────────────
├── plugins/                # ← renamed from examples/ (these are first-party REFERENCE
│   ├── engine/ state/ …    #    plugins that validate the contracts, not throwaway samples)
│   └── README.md           # the axis map + ADR-003 reference-count rationale
│
│   ── ④ THE ASSEMBLED PRODUCT ──────────────────────────────────
├── platform/               # "v2 rebuilt on v3" — the medallion bundle (cleaned of the
│   │                       #    pre-dbt sql-pipeline generation)
│   └── …
├── marketplace/            # the live plugin-discovery index (official.json) — a deliverable
│
│   ── ⑤ DURABLE KNOWLEDGE ──────────────────────────────────────
├── docs/
│   ├── vision.md
│   ├── architecture/       # overview.md · cross-cutting-coverage.md · adrs/
│   ├── research/           # ← moved from top-level research/ (prior-art · competitors)
│   └── conversations/
│
│   ── ⑥ LIVE OPERATIONAL STATE + REVIEWS (stay top-level) ──────
├── reviews/                # adversarial + gate reviews (154-ref link density — not worth moving)
│   └── archive/            # ← the Q02 simulated-review kit + board/ raw outputs
├── roadmap/                # current · phases · done · backlog (SSOT, refreshed each session)
├── ideas/                  # capture inbox (active)
└── scripts/                # Makefile-driven dev/build helpers
```

**Extracted out of the repo:** `experiments/data-dev-plane/` → its own **`rat-data-dev`**
showcase repo (your call). A one-line pointer stub stays behind. See the extraction plan below.

## Decisions, with cost + recommendation

| Decision | Cost | Recommendation |
|---|---|---|
| `examples/` → `plugins/` | 311 string refs / 104 files (mechanical sed; Go modules unaffected) | **✅ LOCKED — do it.** Highest signal-per-churn: the tree should say "plugins," the whole thesis. |
| `reviews/` → `docs/reviews/` | 154 ref rewrites (mechanical) | **✅ LOCKED — NO.** Stays top-level; 154-ref move isn't worth it. Cruft archived in `reviews/archive/` instead. |
| `research/` → `docs/research/` | 11 refs | **✅ LOCKED — do it.** Cheap, obviously belongs under docs. |
| `roadmap/`, `ideas/` stay top-level | 0 | **Keep.** Live working state, read/written every session — not archival. |
| `marketplace/` stays top-level | 0 | **Keep.** A distinct published artifact (the index). |
| Drop `contracts/sdks/{typescript,rust}/` | low (regenerable) | **Do it.** Zero consumers; ~84 files. |
| Drop `core/arrowticket/` | low | **Defer** — keep as living security-spec unless captured elsewhere. |
| Add `LICENSE` + `CONTRIBUTING.md` | new files | **Do it.** Baseline professionalism. |
| Rewrite `README.md` as a repo map | content | **Do it.** Currently the human overview; make it the navigational front door. |

## data-dev-plane extraction plan (→ `rat-data-dev`)

The experiment is self-contained and "complete + paused." Graduating it:

**Moves to the new repo:**
- `experiments/data-dev-plane/` (README + 3 runners + compose)
- The 5 exploratory plugins it spawned (currently in `examples/`→`plugins/`):
  `engine/duckdb-ml-py`, `catalog/ducklake-py`, `storage/minio-s3`,
  `strategy/incremental-embed-py`, **`ui/vscode-rat`** (✅ locked — leaves too)
- The `scripts/data-dev-*.sh` (5) + their Makefile targets

**Stays behind:** a stub `docs/experiments.md` (or a line in README) — "the data-dev ML
lakehouse showcase lives at `rat-data-dev`; it validated plugins X/Y/Z against the frozen
wire." Main-repo canonical UI becomes **`plugins/ui/vscode-platform`** + `web-portal-py`.

**Net reduction here:** ~6 plugins + the experiment dir + 5 scripts leave the main repo,
which sharpens it to *the platform*, not *the platform + a showcase*. ⚠️ The federation/RatFS
work (ADRs 033/034 + `state/v1 Delete`) is showcased by `vscode-rat`, which is leaving — make
sure `rat-data-dev` carries the Phase-10 federation demo, or add a lighter federation demo to
`vscode-platform` before the extraction so the main repo still proves that work.

## How the audit cuts map onto the target

- **Untracked artifacts** → `make clean`; never in the tree.
- **TS + Rust SDKs** → gone from `contracts/sdks/`.
- **Superseded pipeline** (`sql-pipeline-py`, `platform/project/`, `pipelines/`) → gone.
- **Q02 kit + board/** → `docs/reviews/archive/`.
- **Stale roadmap + ADR status drift** → fixed in place (`roadmap/`, ADR headers).
- **data-dev experiment + 4 plugins** → extracted to `rat-data-dev`.

## Sequencing (each a clean commit on `phase-10`)

1. **Hygiene & docs** (no moves): `make clean` target · fix roadmap · ADR status sweep ·
   `LICENSE` · `CONTRIBUTING.md` · README repo-map. *(Safe, builds confidence.)*
2. **Cuts** (deletions): TS+Rust SDKs · superseded pipeline files · dead `buf.gen.python.yaml`.
3. **Archive**: Q02 kit + `board/` → `docs/reviews/archive/` (or `reviews/archive/` if reviews stays).
4. **Moves** (mechanical link-rewrite passes, one dir at a time, verified green after each):
   `research/`→`docs/`, optionally `reviews/`→`docs/`, then `examples/`→`plugins/`.
5. **Extraction**: carve out `rat-data-dev` (separate repo init), leave the stub.
6. **Seal**: update roadmap, then merge `phase-10`→`main` + tag.

Steps 1–3 are zero-risk. Step 4 is mechanical but touches many files (do it scripted +
`make` green-check between each). Step 5 is the judgment-heavy one.
