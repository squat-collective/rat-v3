"""In-memory usage-metering ledger — the data layer behind the billing reference.

A `kind: billing` plugin is a metering SINK: the core emits usage events at
well-defined points (pipeline run, credential vend, storage bytes) and this plugin
records + aggregates them. This ledger is per-tenant BY CONSTRUCTION (C7): every
write is keyed by the tenant the caller carried in the rat-callmeta-bin metadata
envelope (ADR-007), so multi-tenant deployments meter per tenant without the
billing plugin re-deriving the boundary.
"""

from collections import defaultdict


class BillingLedger:
    """Records usage events per tenant and keeps a running per-(tenant, meter) sum."""

    def __init__(self) -> None:
        # tenant -> list of recorded UsageEvent (append-only, for audit/replay)
        self._ledger = defaultdict(list)
        # (tenant, meter) -> summed quantity
        self._aggregate = defaultdict(float)

    def record(self, tenant: str, events) -> int:
        """Append each event under `tenant` and fold its quantity into the
        (tenant, meter) aggregate. Returns the number of events recorded."""
        for ev in events:
            self._ledger[tenant].append(ev)
            self._aggregate[(tenant, ev.meter)] += ev.quantity
        return len(events)

    def aggregate(self, tenant: str, meter: str) -> float:
        """The summed quantity for (tenant, meter); 0.0 if none recorded."""
        return self._aggregate.get((tenant, meter), 0.0)
