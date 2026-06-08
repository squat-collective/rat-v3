"""In-memory telemetry sink for rat-observability-inmemory-py — the `observability`
reference. An EXPORT sink: the core's own observability is native (its /metrics, OTel
spans) and does NOT depend on this axis (observability.proto). This sink receives
telemetry the core + plugins emit and (here) just counts it.

A point is accepted if it has a non-empty `name`; an unnamed point is rejected. Counts
are cumulative for the lifetime of one Ingest bidi stream, so the emitter sees running
accepted/rejected totals and can apply backpressure (observability.proto API-4).
"""

import threading


class TelemetrySink:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.points = []  # all accepted points (for inspection)

    def ingest(self, points):
        """Record a batch; return (accepted_in_batch, rejected_in_batch)."""
        acc = rej = 0
        with self._lock:
            for p in points:
                if p.name:
                    self.points.append(p)
                    acc += 1
                else:
                    rej += 1
        return acc, rej
