"""The RuntimeService gRPC implementation (Python) — second `runtime` reference.

Execute is a server-streaming RPC: the servicer method is a generator that yields
`steps` ExecuteProgress updates then a terminal ExecuteCompleted. Fraction is set
to (i+1)/steps unless the work is indeterminate (then left absent, exercising the
proto3 optional double). Empty work_spec → INVALID_ARGUMENT (aborts the stream).

RequestContext is NOT a field (ADR-007); this reference ignores the
rat-callmeta-bin envelope. The same tiny work_spec format as the Go reference.
"""

import json

import grpc

from rat.common.v1 import data_pb2
from rat.runtime.v1 import runtime_pb2, runtime_pb2_grpc


class RuntimeServicer(runtime_pb2_grpc.RuntimeServiceServicer):
    def Execute(self, request, context):
        if not request.work_spec:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "work_spec is required")
        spec = json.loads(request.work_spec)

        if spec.get("fail"):
            yield runtime_pb2.ExecuteResponse(
                completed=runtime_pb2.ExecuteCompleted(success=False, error=spec["fail"])
            )
            return

        steps = int(spec.get("steps", 0))
        indeterminate = bool(spec.get("indeterminate", False))
        for i in range(steps):
            prog = runtime_pb2.ExecuteProgress(message=f"step {i + 1}/{steps}")
            if not indeterminate:
                prog.fraction = (i + 1) / steps  # present == determinate progress
            yield runtime_pb2.ExecuteResponse(progress=prog)

        rows = int(spec.get("rows", 0))
        yield runtime_pb2.ExecuteResponse(
            completed=runtime_pb2.ExecuteCompleted(
                success=True, result=data_pb2.WriteResult(rows_affected=rows)
            )
        )
