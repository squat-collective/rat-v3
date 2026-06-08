"""In-memory scheduler backend for rat-scheduler-inmemory-py — the `scheduler-backend`
reference. A clock, not an orchestrator: it owns "fire this at this time", the
reconciler decides what to do when it fires (scheduler.proto).

This reference implements one-shot triggers (`at_unix_ms`); a real backend would also
parse `cron`. WatchDue yields every currently-due, not-cancelled, not-yet-fired trigger
and marks it fired — at-least-once by contract (a reconnecting consumer re-derives
due-ness; redelivery is allowed).
"""

import threading


class Scheduler:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._triggers = {}  # trigger_id -> {"at": int, "cancelled": bool, "fired": bool}

    def schedule(self, trigger_id: str, cron: str, at_unix_ms: int) -> str:
        with self._lock:
            self._triggers[trigger_id] = {"at": at_unix_ms, "cancelled": False, "fired": False}
        return trigger_id

    def cancel(self, trigger_id: str) -> bool:
        with self._lock:
            t = self._triggers.get(trigger_id)
            if t is None:
                return False
            t["cancelled"] = True
            return True

    def due(self, now_ms: int):
        """Return [(trigger_id, fired_at_ms)] for every one-shot whose time has passed
        and that is neither cancelled nor already fired; mark them fired."""
        out = []
        with self._lock:
            for tid, t in self._triggers.items():
                if not t["cancelled"] and not t["fired"] and 0 < t["at"] <= now_ms:
                    t["fired"] = True
                    out.append((tid, now_ms))
        return out
