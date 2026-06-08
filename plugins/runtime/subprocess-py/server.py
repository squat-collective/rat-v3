"""The RuntimeService gRPC implementation (Python, subprocess-backed) — round-2
reference. Each Execute spawns a real child OS PROCESS (worker.py) to run the work
unit, then streams that process's emitted progress + completion. Where the
in-memory references run the work in-thread, this gives REAL process isolation — the
"where does the code run" axis actually running it somewhere else.
"""

import json
import os
import subprocess
import sys

import grpc

from rat.runtime.v1 import runtime_pb2, runtime_pb2_grpc

WORKER = os.path.join(os.path.dirname(__file__), "worker.py")


def run_worker(spec_json: str):
    """Spawn the work unit in its own process; return its parsed JSON events. Used
    by Execute (to stream) and by the round-2 isolation tests (to inspect the PID)."""
    proc = subprocess.run([sys.executable, WORKER, spec_json], capture_output=True, text=True)
    events = [json.loads(line) for line in proc.stdout.splitlines() if line.strip()]
    return events, proc.returncode


class RuntimeServicer(runtime_pb2_grpc.RuntimeServiceServicer):
    def Execute(self, request, context):
        if not request.work_spec:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "work_spec is required")
        events, _ = run_worker(request.work_spec.decode("utf-8"))
        for ev in events:
            if ev["t"] == "progress":
                p = runtime_pb2.ExecuteProgress(message=ev.get("message", ""))
                if "fraction" in ev:
                    p.fraction = ev["fraction"]
                yield runtime_pb2.ExecuteResponse(progress=p)
            elif ev["t"] == "completed":
                c = runtime_pb2.ExecuteCompleted(success=ev["success"])
                if ev.get("error"):
                    c.error = ev["error"]
                if ev["success"]:
                    c.result.rows_affected = ev.get("rows", 0)
                yield runtime_pb2.ExecuteResponse(completed=c)
