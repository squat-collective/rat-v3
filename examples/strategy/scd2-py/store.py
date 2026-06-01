"""The SCD2 strategy backend — the SECOND `kind: strategy` reference (ADR-003).

Where [`fullrefresh-py`](../fullrefresh-py) is a stateless transform-and-replace, SCD2
(Slowly Changing Dimension, Type 2) is **stateful and temporal**: it reads the incoming
source snapshot AND the existing target history, computes which natural keys are new /
changed / unchanged / deleted, and incrementally writes a versioned history — closing
the old version of a changed row (`is_current=false`, `effective_to=<run ts>`) and
inserting a new current version. It is the deliberately-divergent second implementation
ADR-003 calls for: a genuinely different code path with different semantics over the
SAME `strategy/v1` contract.

It exercises a DIFFERENT capability mix than full-refresh — which is the whole point,
the contract must serve both:

  full-refresh : …register-table → engine.query    → format.overwrite → commit-table
  SCD2         : …register-table → format.scan (×2) → format.merge     → commit-table

Three contract observations this reference surfaced (the ADR-003 payoff):
  1. A strategy that READS existing target state composes by calling `format.scan` on
     the target ref — the contract already supports it; no new RPC needed.
  2. A strategy that SYNTHESIZES rows (the SCD2 delta) is a data PRODUCER: it hosts
     those rows on an Arrow stream for `format.merge` to pull (`host_rows`).
  3. A strategy that READS bulk data is a data CONSUMER: it pulls the scan ArrowStream
     itself (`pull`). Full-refresh is neither — it just routes the engine's stream to
     the format. So a strategy can sit anywhere on the data plane, and the contract's
     two seams (the control-plane `invoke` + the out-of-band Arrow legs) cover it.

The run timestamp (`effective_from`) and the key/tracked columns ride in `options`
(strategy.proto's metadata-schema'd bytes) — the contract's per-run parameter bag.
"""

import json

from rat.catalog.v1 import catalog_pb2
from rat.common.v1 import data_pb2
from rat.format.v1 import format_pb2

CAP_GET_TABLE = "rat://catalog/v1/get-table"
CAP_REGISTER = "rat://catalog/v1/register-table"
CAP_COMMIT = "rat://catalog/v1/commit-table"
CAP_SCAN = "rat://format/v1/scan"
CAP_MERGE = "rat://format/v1/merge"

REQUIRES = (CAP_GET_TABLE, CAP_REGISTER, CAP_COMMIT, CAP_SCAN, CAP_MERGE)

# Standard SCD2 metadata columns the strategy maintains on the target.
COL_FROM, COL_TO, COL_CURRENT = "effective_from", "effective_to", "is_current"


class SCD2Strategy:
    def __init__(self, invoke, host_rows, pull) -> None:
        self._invoke = invoke          # invoke(capability, request) -> response (gateway)
        self._host_rows = host_rows    # host_rows(list[dict]) -> ArrowStream (producer)
        self._pull = pull              # pull(ArrowStream) -> list[dict] (consumer)

    def _get_table(self, identifier):
        return self._invoke(CAP_GET_TABLE, catalog_pb2.GetTableRequest(identifier=identifier)).table

    def _scan_rows(self, ref):
        resp = self._invoke(CAP_SCAN, format_pb2.ResolveRequest(table=ref))
        return self._pull(resp.stream)

    def apply(self, source_id, target_id, options):
        spec = json.loads(options.decode("utf-8")) if options else {}
        nk = list(spec["natural_key"])      # e.g. ["id"]
        tracked = list(spec["tracked"])     # e.g. ["name", "region"]
        ts = spec["effective_from"]         # this run's effective timestamp

        source_ref = self._get_table(source_id)
        # Register the SCD2 history table the pipeline owns (idempotent — run 2 of a
        # temporal load re-registers the same target and gets the existing ref back).
        target_ref = self._invoke(
            CAP_REGISTER, catalog_pb2.RegisterTableRequest(identifier=target_id)).table
        source_rows = self._scan_rows(source_ref)
        target_rows = self._scan_rows(target_ref)

        def key(r):
            return tuple(r[k] for k in nk)

        current = {key(r): r for r in target_rows if r.get(COL_CURRENT)}
        src_keys = set()
        delta = []  # closures + new versions, merged on (natural key…, effective_from)

        for sr in source_rows:
            k = key(sr)
            src_keys.add(k)
            cur = current.get(k)
            if cur is None:                                       # NEW natural key
                delta.append(_version(sr, nk, tracked, ts))
            elif any(cur.get(c) != sr.get(c) for c in tracked):   # CHANGED → close + new
                delta.append(_close(cur, nk, tracked, ts))
                delta.append(_version(sr, nk, tracked, ts))
            # else UNCHANGED → no-op

        for k, cur in current.items():                            # DELETED → close
            if k not in src_keys:
                delta.append(_close(cur, nk, tracked, ts))

        if not delta:
            return data_pb2.WriteResult(rows_affected=0)

        # Merge on the VERSION identity (natural key + effective_from): closures match
        # the existing open version and replace it; new versions are inserts.
        stream = self._host_rows(delta)
        resp = self._invoke(CAP_MERGE, format_pb2.MergeRequest(
            table=target_ref, source=stream, merge_keys=nk + [COL_FROM]))

        # Commit-linkage (ADR-010): record the snapshot this temporal load produced.
        # The run timestamp keys the idempotent commit (one logical commit per run).
        self._invoke(CAP_COMMIT, catalog_pb2.CommitTableRequest(
            identifier=target_id, branch=target_ref.branch,
            snapshot_id=resp.result.snapshot_id, idempotency_key=f"{target_id}@{ts}"))
        return resp.result


def _version(src, nk, tracked, ts):
    row = {c: src[c] for c in nk + tracked}
    row[COL_FROM], row[COL_TO], row[COL_CURRENT] = ts, "", True
    return row


def _close(cur, nk, tracked, ts):
    row = {c: cur[c] for c in nk + tracked}
    row[COL_FROM], row[COL_TO], row[COL_CURRENT] = cur[COL_FROM], ts, False
    return row
