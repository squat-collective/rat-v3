# rat-format-delta-py — ROUND-2 `format` reference (real Delta Lake table)

The **second** real format of the round-2 pair (with [`parquet-py`](../parquet-py)):
backs the table with a real **Delta Lake** table — a transaction log over Parquet,
genuinely different storage technology from plain Parquet files.

It passes the **SAME shared vectors** (`contracts/conformance/format-v1.json`) as the
in-memory + Parquet refs — the typed-Arrow data leg both directions — and earns a
property neither the in-memory dict nor plain Parquet files can show:

| Round-2 property | Test | Why the others can't |
|---|---|---|
| **Time travel** | `test_delta_time_travel` | two appends → versions 0 and 1; read version 0 back and get the *prior* table state | a dict / a pile of Parquet files have no version log |

Delta/Iceberg-style versioned snapshots are exactly what the `catalog` axis's branches
sit on top of — so this is the storage substrate for branch-isolated pipeline runs,
made real. Only `store.py` differs from the Parquet reference; `server.py`/`streams.py`
are identical.

> Note: deltalake's Rust runtime aborts at interpreter teardown (a known quirk) AFTER
> all logic completes; the standalone harness calls `os._exit(0)` after PASS to exit
> cleanly past it. The conformance + time-travel logic all runs and passes first.

## Run it (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/plugins/format/delta-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — Delta: real format backend conformed to format/v1 golden vectors + time travel`.
