# Conformance vectors — the ADR-003 shared golden data

> *"No data-plane contract may be tagged `v1-frozen` until two independent reference
> implementations exist, both pass the axis's conformance suite, and have been run
> against each other on golden data."* — [ADR-003](../../docs/architecture/adrs/003-two-references-before-contract-freeze.md)

This directory holds the **language-neutral golden vectors** for each data-plane
axis. They are the *single source of truth* for "what does a conformant plugin of
this axis do." Every independent reference implementation's harness loads the JSON
from here and drives its service over real gRPC against these expectations.

This is how the "run against each other on golden data" clause is satisfied
**literally**: both impls execute the *same file*, not two hand-copied vector sets
that happen to agree. If the file changes, every impl re-tests against the new
truth on its next run.

## Running the suite (sub-phase 0f)

```bash
make conformance        # or: scripts/conformance.sh
```

This **auto-discovers every reference** under `plugins/<axis>/<impl>/`, runs each
one's harness against its golden vectors (Go refs via `go test`, Python refs via
`python harness_test.py`, all containerized — no host installs), and prints a single
pass/fail matrix:

```
  axis      impl             lang vectors              result
  --------- ---------------- ---- -------------------- ------
  engine    duckdb-py        py  engine-real-v1.json  PASS
  format    parquet-py       py  format-v1.json       PASS
  state     sqlite-py        py  state-v1.json        PASS
  …
  20/20 references conform
```

Exit 0 iff every reference conforms — so CI / the freeze gate can hang on it. A new
reference joins the suite automatically the moment it lands. This is the operational
form of ADR-003's "both pass the axis's conformance suite": one command, one matrix.

## Per-axis contract docs (sub-phase 0g — ✅ complete, all 18 axes)

The author-facing guide for each axis — what the axis is, its capabilities + RPCs, the
conformance obligations, how to write a plugin, and the reference impls — lives as a
`CONTRACT.md` **next to that axis's proto** (so authors read the wire contract + the
guide together): `proto/rat/<axis>/v1/CONTRACT.md`, for every one of the 18 axes.
Examples: [`state`](../proto/rat/state/v1/CONTRACT.md) ·
[`engine`](../proto/rat/engine/v1/CONTRACT.md) ·
[`format`](../proto/rat/format/v1/CONTRACT.md) ·
[`ui`](../proto/rat/ui/v1/CONTRACT.md). The walkthrough that ties them together:
[`docs/guides/authoring-a-plugin.md`](../../docs/guides/authoring-a-plugin.md).

## Files

| File | Axis | Consumed by |
|---|---|---|
| `format-v1.json` | `format/v1` | `plugins/format/inmemory-go` (Go harness), `plugins/format/inmemory-py` (Python harness) |
| `engine-v1.json` | `engine/v1` | `plugins/engine/inmemory-go` (Go harness), `plugins/engine/inmemory-py` (Python harness) — round-1 toy mini-SQL (wire contract) |
| `engine-real-v1.json` | `engine/v1` | `plugins/engine/duckdb-py`, `plugins/engine/datafusion-py` — round-2 REAL SQL + typed Arrow (the duckdb+datafusion cross-run) |
| `storage-v1.json` | `storage/v1` | `plugins/storage/inmemory-go` (Go harness), `plugins/storage/inmemory-py` (Python harness) |

### `engine-v1.json` mini-SQL grammar

The engine references implement a deliberately tiny, fully-specified mini-SQL so
two independent parsers stay in lockstep (the SAME three regexes in Go + Python).
Case-sensitive keywords, single-space separated; identifiers/values `[A-Za-z0-9_]+`:

```
CREATE TABLE <t> (<col>, <col>, ...)        -- Execute; rows_affected = 0
INSERT INTO <t> VALUES (<v>, <v>, ...)       -- Execute; rows_affected = 1 (values bind positionally to columns)
SELECT <* | col, col, ...> FROM <t> [WHERE <col> = <val>] [LIMIT <n>]  -- Query / Preview
```

`Query` returns an ArrowStream of all matching rows; `Preview` bounds it by the
request `limit`. The vectors add `rows_exclude_keys` to assert projected-out
columns are absent. SQL fidelity is NOT the point — the engine WIRE contract is.

## Vector shape (`format-v1.json`)

```jsonc
{
  "axis":  "format/v1",
  "table": "warehouse.sales.orders",   // the TableRef.identifier all steps target
  "lifecycle": [                        // ordered; state carries across steps
    { "step": "...", "op": "append|merge|overwrite|scan|maintain",
      "source": [ {col: val, ...} ],    // rows staged onto the source ArrowStream (write ops)
      "merge_keys": ["id"],             // merge only
      "expect": {
        "rows_affected": 2,             // assert WriteResult.rows_affected
        "rows_affected_absent": true,   // assert rows_affected is unset (proto3 optional)
        "snapshot_id_set": true,        // assert WriteResult.snapshot_id != ""
        "row_count": 3,                 // scan: assert N rows come back
        "rows_contain": [ {col: val} ]  // scan: assert each partial row matches some scanned row
      }
    }
  ],
  "errors": [                           // independent (not part of the lifecycle state)
    { "step": "...", "op": "...", "table_override": "",
      "expect": { "code": "INVALID_ARGUMENT" } }   // assert the gRPC status code
  ]
}
```

Values are strings throughout: the reference impls carry the "bulk" leg as a
trivial in-process row registry rather than a typed Arrow stream (the control
contract is what's under test in 0d — the real Arrow Flight wire is deferred to a
production reference). A typed-Arrow conformance pass is future work.

## Adding an axis

When a second data-plane axis reaches 0d (two impls), add `<axis>-v1.json` here
and have both impls' harnesses load it. Keep the executor in each harness generic
(a switch over `op`) so new steps need only a JSON edit, not harness code.
