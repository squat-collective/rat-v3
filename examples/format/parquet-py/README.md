# rat-format-parquet-py — ROUND-2 `format` reference (real Parquet + REAL Arrow Flight)

The **round-2** `format` reference (with [`delta-py`](../delta-py)): writes table rows
as **real Parquet files** on disk, AND carries the data leg over a **real
`pyarrow.flight` transport** — retiring the in-process Arrow-IPC registry stand-in
every other reference uses. This is the only reference where the bulk-data leg is
*fully* real: real file format + real Flight wire.

## The real Arrow Flight data leg (`flight.py`)

The RAT contract says bulk rows move out-of-band as Arrow, described by a
`common.v1.ArrowStream {endpoint, ticket, transport=FLIGHT, role}`. This makes that
literal — **both directions**, over real TCP sockets:

| Leg | Who hosts | How |
|---|---|---|
| `Resolve` result | the **plugin** runs a Flight server | descriptor → `grpc://host:port` + ticket; the harness dials it and **DoGet**s the result |
| `Append`/`Merge`/`Overwrite` source | the **caller** (harness) runs a Flight server | descriptor → the caller's endpoint; the plugin **DoGet**s the source rows |

Tickets are single-use (a DoGet consumes the ticket — SEC-14). Both legs use
`PRODUCER_HOSTED` (the data-holder hosts; the data-needer DoGets), matching the
contract's "Resolve → producer-hosted; the format pulls from a caller-hosted source".

It passes the **SAME shared vectors** (`contracts/conformance/format-v1.json`) as the
in-memory + Delta refs, runs the full Append→scan→Merge(upsert)→Overwrite→Maintain
lifecycle on real Parquet files, and asserts real `.parquet` files land on disk.

> The other references (incl. `delta-py`) keep the in-process Arrow-IPC registry for
> simplicity — that was always a transport *choice*, not a contract limitation. This
> reference proves the transport can be real Arrow Flight with zero contract change.

## Run it (containerized — no host installs)

```bash
podman run --rm \
  -v "$PWD":/work:Z \
  -e PYTHONPATH=/work/contracts/sdks/python \
  -w /work/examples/format/parquet-py \
  python:3.12 bash -c 'pip install -q -r requirements.txt && python harness_test.py'
```

Expected: `PASS — Parquet: conformed to format/v1 over REAL Arrow Flight + real files on disk`.
