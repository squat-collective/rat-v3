# Amending the frozen contracts — the procedure

> The wire is **frozen** at `rat/1` (ADR-009; everything ≤ `rat/2.0` is the sealed
> surface). It still evolves — through **additive, capability-gated amendments**. This is
> the written procedure that was previously only implicit in the precedents:
> [ADR-035](../docs/architecture/adrs/035-state-axis-delete.md) (`state/v1 delete`) and
> [ADR-049](../docs/architecture/adrs/049-state-v1-create-if-absent.md)
> (`state/v1 create-if-absent`). Read this before touching anything under `proto/`.

## What "additive" means (enforced, not honour-system)

Allowed: a **new RPC** (with its own capability annotation), a **new message**, a new
field on a new message, reusing existing enums/messages (ADR-049 reuses `PutOutcome`).

Not allowed: removing/renaming anything, renumbering or retyping fields, changing
semantics of existing fields. `make breaking` (buf breaking vs the `main` baseline)
rejects these — run it before you push; note the per-commit hook only runs `make check`
(lint), so a breaking change is caught at `make verify`/`make breaking` time, **not** at
commit time.

⚠️ **The subtle trap — new fields on existing methods.** A new field on an *existing*
RPC's response is wire-additive but semantically dangerous: proto3 gives it a default
value, so an old backend "answers" with `false`/`0`/`""` and a new consumer can't tell
"not supported" from "supported, answered negative" (the ADR-049 Q02 `already_applied`
hazard). Prefer a **new RPC + capability** — then capability *presence* is the
negotiation and an old backend can never be silently misused.

## The capability-gating rule

**Optionality is expressed by capability presence in `provides`, never by a proto flag
or sentinel value.**

- A backend that implements the new RPC declares its capability URI in the manifest
  `provides:`; one that doesn't, doesn't. Consumers feature-detect via the registry (or
  handle `UNIMPLEMENTED`) and fall back.
- **Per-kind schema:** only capabilities **required of every implementation** of a kind
  belong in `schema/kinds/<kind>.v1.json`'s required `provides` set. Optional amendments
  (like `delete`, `create-if-absent`) are *not* schema-required — presence is the signal.
  If your amendment is required-for-kind, that's a much bigger conversation: it breaks
  every existing implementation; the ADR must say so.

## The checklist

1. **ADR first** (`docs/architecture/adrs/NNN-*.md`, one commit, status Proposed →
   Accepted). Must contain: why, the exact proto shape, consequences (incl. the cost
   table below), rejected alternatives, the rollout staging.
2. **Amend the proto** — new RPC + `option (rat.common.v1.capability) = "rat://…";`
   annotation + a doc comment stating it's an additive amendment, optional, and how
   consumers negotiate (copy ADR-049's `CreateIfAbsent` comment as the template).
3. **Regenerate SDKs** — `make gen-sdks` (Go + Python regenerate together; commit the
   output with the proto). `make gen-check` is the CI freshness gate. Expect ~100–300
   generated LOC per language.
4. **Gate it** — `make check` (lint), `make breaking` (must pass = additive).
5. **Extend the golden vector** — `conformance/<axis>-v1.json`: lifecycle steps for the
   happy path, the conflict/edge path, and the "old data unchanged" assertion (ADR-049
   added create→COMMITTED, re-create→CONFLICT, get→no-overwrite). The vectors are
   convention-JSON with **no schema validation** — re-run a reference harness and watch
   your new steps actually execute (a typo'd `op`/`expect` key is silently skipped).
6. **Implement in the references** — at least one per language
   (`plugins/<axis>/<impl>/`); concurrency-sensitive amendments need a `-race` test
   (ADR-049: N racers → exactly one `COMMITTED`).
7. **Update the axis `CONTRACT.md`** — capability table row + RPC semantics bullet.
   (The contract guide is author-facing truth; an undocumented amendment doesn't exist.)
8. **Wire the consumers** — feature-detect, fall back, one commit per consumer
   (ADR-049: the lease bootstrap at `6.9`, the ticket store at `6.11`).
9. **Run the suites** — `make conformance` + `make validate-manifests` (+
   `make composition` if the axis participates).
10. **Manifests** — add the capability to the `provides:` of every reference that now
    implements it.
11. **Land it** — topic branch → `--no-ff` merge to `main` with a `seal(rat/N.M): …`
    message + annotated `rat/N.M` tag per
    [git-branching.md](../.claude/rules/git-branching.md).
12. **Roadmap** — `done.md` entry (one per stage), `current.md` refresh.

## The measured cost (set expectations honestly)

ADR-049 — **one optional, additive RPC** — cost, from the git history:

| | |
|---|---|
| Commits (merges to `main`) | 6 (`rat/6.8` → `rat/6.13`) |
| Files touched | ~20 |
| Lines (incl. generated + tests) | ~1,100 |
| Languages | 2 (Go + Python SDK regen + impls) |
| Backends implementing it | 4 (inmemory-go, inmemory-py, sqlite-py, postgres-py) |
| Core consumers wired | 2 (HA-lease bootstrap, arrow-ticket store) |
| Wall-clock | ~5 days of sessions |

That is the *floor* for doing it right. If the amendment doesn't justify that cost,
reconsider: can an existing capability + labels/selection (ADR-045) express it instead?
(Memory aid: **prefer a pure plugin over a contract change; prefer a contract amendment
over a new axis** — a new axis additionally requires core changes: `knownKinds`,
per-kind schema, descriptors, i.e. a core recompile.)

## Known friction (acknowledged, queued)

- **Committed-SDK merge conflicts:** two branches amending *different* axes still collide
  in regenerated files. Resolution: rebase, `make gen-sdks`, commit the regen — never
  hand-merge generated code.
- **Python codegen toolchain** is a pinned custom path (`codegen/Dockerfile.python`:
  standalone protoc + pinned `grpcio-tools`), not `buf generate` — ADR-018 Q01 is still
  open. If `make gen-sdks` fails on Python, suspect the toolchain image, not your proto.
- **No vector schema:** conformance vectors are unvalidated JSON (step 5's caveat).

## Related

- [README.md](README.md) — the contract surface this amends.
- [ADR-009](../docs/architecture/adrs/009-data-plane-contract-freeze-v1.md) · [ADR-017](../docs/architecture/adrs/017-pre-unfreeze-contract-amendment-gate.md) · [ADR-035](../docs/architecture/adrs/035-state-axis-delete.md) · [ADR-049](../docs/architecture/adrs/049-state-v1-create-if-absent.md)
- [conformance/README.md](conformance/README.md) — vector format + harness conventions.
