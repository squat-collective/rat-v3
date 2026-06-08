"""In-memory delivery sink for the notifications/v1 axis — a test/webhook
stand-in that CAPTURES notifications instead of delivering them anywhere.

A `kind: notifications` plugin is a delivery sink: WHAT to notify on is the
operator's subscription config (overview.md reconciliation: subscriptions =
[event, action]), not this plugin. This reference owns only the delivery side:
take a (severity, title, body, target, attributes) and "deliver" it — here, by
appending it to an in-memory list the harness can inspect.
"""

import grpc


class NotificationError(Exception):
    """Carries a gRPC status code + message for the servicer to abort with."""

    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class NotificationSink:
    def __init__(self) -> None:
        self.captured = []  # list of dicts, one per delivered notification
        self._n = 0  # monotonic message counter

    def send(self, severity, title, body, target, attributes):
        """Deliver one notification. Returns (delivered, message_id).

        Empty title → INVALID_ARGUMENT (a notification needs a subject)."""
        if not title:
            raise NotificationError(grpc.StatusCode.INVALID_ARGUMENT, "title is required")
        self._n += 1
        message_id = f"msg-{self._n}"
        self.captured.append(
            {
                "severity": severity,
                "title": title,
                "body": body,
                "target": target,
                "attributes": dict(attributes),
            }
        )
        return True, message_id
