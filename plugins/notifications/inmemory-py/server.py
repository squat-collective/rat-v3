"""The NotificationsService gRPC implementation (Python) — `notifications`
reference.

Implements Send: validate, hand off to an in-memory NotificationSink, and
return (delivered, message_id). The notifications axis is NOT tenant-scoped, so
unlike the storage reference there is no rat-callmeta-bin tenant handling here.

The servicer takes an optional shared NotificationSink so the conformance
harness can inspect what was `captured`.
"""

import grpc

from rat.notifications.v1 import notifications_pb2, notifications_pb2_grpc

from store import NotificationError, NotificationSink


class NotificationsServicer(notifications_pb2_grpc.NotificationsServiceServicer):
    def __init__(self, sink: NotificationSink = None) -> None:
        self.sink = sink or NotificationSink()

    def Send(self, request, context):
        try:
            delivered, message_id = self.sink.send(
                severity=request.severity,
                title=request.title,
                body=request.body,
                target=request.target,
                attributes=request.attributes,
            )
        except NotificationError as e:
            context.abort(e.code, e.message)
        return notifications_pb2.SendResponse(delivered=delivered, message_id=message_id)
