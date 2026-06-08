"""The SchedulerService gRPC implementation (Python) — scheduler-backend reference.

Schedule/Cancel are unary; WatchDue is server-streaming (ADR-008 mediates it via the
core's InvokeServerStream). RequestContext is NOT a field (ADR-007). WatchDue yields
the currently-due triggers then completes — at-least-once delivery (scheduler.proto):
the reconciler keys actions by (trigger_id, fired_at) and ignores duplicates.
"""

import time

from rat.scheduler.v1 import scheduler_pb2, scheduler_pb2_grpc

from store import Scheduler


class SchedulerServicer(scheduler_pb2_grpc.SchedulerServiceServicer):
    def __init__(self, scheduler: Scheduler = None, now_ms=None) -> None:
        self.sched = scheduler or Scheduler()
        self._now_ms = now_ms or (lambda: int(time.time() * 1000))

    def Schedule(self, request, context):
        tid = self.sched.schedule(request.trigger_id, request.cron, request.at_unix_ms)
        return scheduler_pb2.ScheduleResponse(trigger_id=tid)

    def Cancel(self, request, context):
        return scheduler_pb2.CancelResponse(cancelled=self.sched.cancel(request.trigger_id))

    def WatchDue(self, request, context):
        for tid, fired_at in self.sched.due(self._now_ms()):
            yield scheduler_pb2.WatchDueResponse(trigger_id=tid, fired_at_unix_ms=fired_at)
