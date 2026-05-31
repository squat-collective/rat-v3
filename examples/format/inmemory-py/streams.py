"""In-process stand-in for the out-of-band Arrow data leg — Python mirror of the
Go reference's stream.go.

The format/v1 contract says bulk rows move out-of-band as Arrow IPC, described by a
common.v1.ArrowStream {endpoint, ticket, transport=FLIGHT, role}. Standing up a real
Arrow Flight server is out of scope for a contract-validation reference; instead the
plugin runs an in-process ticket registry. A producer stashes rows under a ticket and
advertises a PRODUCER_HOSTED ArrowStream; a consumer pulls them back by ticket. Single
use: a pull removes the batch (the SEC-14 ticket guidance the contract documents).
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
        """Stash rows; return a producer-hosted descriptor pointing at them."""
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
        """Retrieve (and remove) the rows for a stream's ticket. Single-use."""
        if stream is None:
            return []
        with self._lock:
            return self._batches.pop(stream.ticket, [])
