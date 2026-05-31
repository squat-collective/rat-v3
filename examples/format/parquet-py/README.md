# rat-format-parquet-py — ROUND-2 `format` reference (real Parquet files)

Half of the **round-2** `format` pair (with [`delta-py`](../delta-py)): writes table
rows as **real Parquet files** on disk (one directory per table identifier) and reads
them back with pyarrow — the real data leg, not the toy string-row registry.

It passes the **SAME shared vectors** as the in-memory format refs
(`contracts/conformance/format-v1.json`) — format's data is just rows, so the vectors
are provider-neutral. Source rows for Append/Merge/Overwrite are staged as **real
Arrow** (Arrow IPC), and Resolve results are pulled back as **real Arrow** — the
typed-Arrow data leg, **both directions** (the `streams.py` shared with the engine
pair). Plus a backend test that real `.parquet` files land on disk and are readable.

The full Append → scan → Merge(upsert) → Overwrite → Maintain lifecycle runs against
real files; `Merge` is a read-modify-write upsert + rewrite, `Maintain` compacts.

See [`../delta-py`](../delta-py) for the second real format backend (Delta Lake, with
time-travel). Together they're ADR-003's two-real-format cross-run (option b).

## Run it (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/format/parquet-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```
