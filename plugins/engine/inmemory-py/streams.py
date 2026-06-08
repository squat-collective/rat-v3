"""In-process stand-in for the out-of-band Arrow data leg — Python mirror of the
Go reference's stream.go. Query/Preview stash result rows under a single-use
ticket and advertise a producer-hosted ArrowStream; the caller pulls them back by
ticket. The real Arrow Flight wire is deferred to a production reference.
"""

import os
import threading
from typing import Dict, List

from rat.common.v1 import data_pb2

Row = Dict[str, str]


class StreamRegistry:
    def __init__(self) -> None:
        self._batches: Dict[bytes, List[Row]] = {}
        self._lock = threading.Lock()

    def put(self, rows: List[Row]) -> data_pb2.ArrowStream:
        ticket = os.urandom(16)
        with self._lock:
            self._batches[ticket] = list(rows)
        return data_pb2.ArrowStream(
            endpoint="inproc://stream",
            ticket=ticket,
            transport=data_pb2.ArrowTransport.ARROW_TRANSPORT_FLIGHT,
            role=data_pb2.ArrowStreamRole.ARROW_STREAM_ROLE_PRODUCER_HOSTED,
        )

    def pull(self, stream: data_pb2.ArrowStream) -> List[Row]:
        if stream is None:
            return []
        with self._lock:
            return self._batches.pop(stream.ticket, [])
