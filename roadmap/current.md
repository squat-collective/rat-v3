# Current — what's in flight right now

> **Always read this first when opening a Claude session.** Concise by design — the full
> completion history lives in [`done.md`](done.md); the phase map in [`phases.md`](phases.md).
> Updated: 2026-06-08.

## Status one-liner

**Phases 0–9 are SEALED** (`rat/1.5` contracts → `rat/2.0` core → `rat/2.5`–`6.0` across
platform · surfaces · distribution · authoring · authoring↔runtime · dependency resolution ·
marketplace · live control). Everything ≤ `rat/2.0` is the **frozen wire**; every phase since
has been additive. **Phase 10 (workspace federation + the security model) is IN-FLIGHT**,
consolidated on the `phase-10` integration branch (not yet sealed). `main` is the sealed line
at `rat/6.0`.

## 🔵 Phase 10 — workspace federation + security (in-flight)

Built on `phase-10` (ADRs [029](../docs/architecture/adrs/029-plugin-runtime-sdk.md)/[031](../docs/architecture/adrs/031-durable-local-storage.md)/[033](../docs/architecture/adrs/033-workspace-federation-hub.md)/[034](../docs/architecture/adrs/034-security-responsibility-model.md)/[035](../docs/architecture/adrs/035-state-axis-delete.md); ADR-[036](../docs/architecture/adrs/036-reconciler-hosts-operators.md) is a Proposed sketch):

- **`rat hub`** — workspace federation, a gateway-of-gateways front door (ADR-033).
- **Identity at the edge** — an `identity-token` plugin (frozen `identity/v1`), hub TLS, and
  a secure-by-default binding guardrail: a public bind refuses without TLS + identity (ADR-034).
- **`ratplugin` runtime SDK** (ADR-029) · **durable `/data` mount** (ADR-031) ·
  **`state/v1 Delete`** (ADR-035, additive wire method) · **RatFS** — `rat://` as a native
  editable VS Code folder over the state axis through the hub.

**Still owed in Phase 10:** gateway-level identity enforcement for *direct* (non-hub) access
(today the per-plane gateway still trusts the wire `--as`; the hub closes it at the edge);
subject-stamping onto the forwarded envelope. Then seal → cut a `rat/6.x` tag.

## 🧹 Active: the professionalization restructure

Reducing the repo to the essential + a professional structure. Plan + audit:
[`docs/restructure/`](../docs/restructure/) (AUDIT.md = keep/remove analysis; TARGET.md =
the locked end-state tree). **Locked decisions:** `examples/`→`plugins/`,
`research/`→`docs/research/`, `reviews/` stays top-level with `reviews/archive/`, and the
**data-dev-plane experiment + 5 exploratory plugins** (incl. `vscode-rat`) **graduate to a
separate `rat-data-dev` repo**.

**Execution (steps 1–4 DONE on `phase-10`; 5 = extraction, paused for sign-off):**
1. ✅ Hygiene — `make clean` (reclaimed ~105M), ADR status sweep (021–026 → Accepted), roadmap refresh.
2. ✅ Cuts — dropped the unused TS + Rust SDKs ([ADR-037](../docs/architecture/adrs/037-trim-committed-sdks-to-consumed-languages.md), 89 files) + the superseded `sql-pipeline-py`/`platform/project/`/`pipelines/` (ADR-021).
3. ✅ Archive — the Q02 simulated-review kit + `reviews/board/` → `reviews/archive/` (12 files; links repointed).
4. ✅ Moves — `research/`→`docs/research/` + `examples/`→`plugins/` ([ADR-038](../docs/architecture/adrs/038-reference-plugins-live-under-plugins.md); 343 files, Go modules rebuilt clean, 0 broken links).
5. ✅ Extracted `rat-data-dev` (`~/sandbox/rat-data-dev`): the experiment + 5 exploratory plugins (incl. `vscode-rat`) + `data-dev-*` scripts. The platform's attach-mode engine/catalog services were vestigial (dbt-runner embeds them) → removed. 47→40 plugins.

**Bonus:** a repo-wide markdown link verifier surfaced + fixed **20 pre-existing broken links**; integrity now clean (0 broken across ~1400 links).

## Immediate next concrete step

The restructure (steps 1–5) is **complete**. Remaining: optionally clean `backlog.md` (intermixes
done + live items), then **seal Phase 10** to `main` + tag (`rat/6.x`). The `rat-data-dev` repo is
local/unpushed — push it when ready. After the seal, the Phase-10 follow-on (direct-gateway
identity enforcement) or any backlog item by appetite.

## What's NOT in flight

- **The user-pull gates still bind.** [phases.md](phases.md) **Gate B** (≥10 real solo users)
  before broad new product phases; **Gate C/D** beyond. **Q02** (external *human* peer review)
  is still owed but set aside as impractical for a solo dev — validated practically instead
  (the data-dev-plane experiment, principle #8).
- The freeze stays **local/unpushed** pending the Q02 punch-list (complete) + the real Q02.

## Branching (in force)

`main` is the sealed line (`rat/6.0`). Work on `phase-10` (integration) or `phase-10-<slug>`
(topic) — **never commit to `main`** (a `PreToolUse` hook blocks it). Full rules:
[`.claude/rules/git-branching.md`](../.claude/rules/git-branching.md).

## Maintenance reminder

When a session produces concrete output: update `done.md` → `current.md` → `phases.md` (if a
phase moved) → `backlog.md` (if new work surfaced). Full rules in [CLAUDE.md](CLAUDE.md).
