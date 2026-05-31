"""The work unit — runs in its OWN OS process (spawned by the runtime server).

Reads a work_spec (JSON) from argv[1], emits one JSON event per line to stdout: a
`progress` event per step (with a `fraction` unless indeterminate), then a terminal
`completed` event carrying success + rows + this process's PID. The PID is what lets
the round-2 isolation test prove the work ran in a separate process (the in-thread
runtime always reports the server's own PID).
"""

import json
import os
import sys


def main() -> None:
    spec = json.loads(sys.argv[1])
    out = sys.stdout

    if spec.get("fail"):
        out.write(json.dumps({"t": "completed", "success": False, "error": spec["fail"], "pid": os.getpid()}) + "\n")
        return

    steps = int(spec.get("steps", 0))
    indeterminate = bool(spec.get("indeterminate", False))
    for i in range(steps):
        ev = {"t": "progress", "message": f"step {i + 1}/{steps}"}
        if not indeterminate:
            ev["fraction"] = (i + 1) / steps
        out.write(json.dumps(ev) + "\n")

    out.write(json.dumps({"t": "completed", "success": True, "rows": int(spec.get("rows", 0)), "pid": os.getpid()}) + "\n")


if __name__ == "__main__":
    main()
