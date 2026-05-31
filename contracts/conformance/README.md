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

## Files

| File | Axis | Consumed by |
|---|---|---|
| `format-v1.json` | `format/v1` | `examples/format/inmemory-go` (Go harness), `examples/format/inmemory-py` (Python harness) |

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
