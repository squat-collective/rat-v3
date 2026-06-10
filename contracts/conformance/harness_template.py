#!/usr/bin/env python3
"""harness_template.py — COPY ME: the canonical conformance-harness skeleton (DX-4).

Previously every new reference copied "the closest sibling's" ~200-line harness, drifting
a little each time. This is the one canonical starting point. To use it:

  1. Copy this file into your plugin dir as `harness_test.py`.
  2. Replace the <PLACEHOLDERS>: your axis, your servicer, your op dispatch, your
     expect handlers.
  3. Run it the way the suite does (scripts/conformance.sh auto-discovers it):
         PYTHONPATH=$REPO/contracts/sdks/python python harness_test.py

Design rules this template enforces (don't undo them):
  - UNMAPPED OP → hard exit. A vector op your harness doesn't dispatch must fail the
    run, not skip.
  - UNKNOWN EXPECT KEY → hard exit (rat.vectors.run_expect). The silent-skip hazard.
  - The vector file is the contract: never edit it to make your plugin pass — extend
    your plugin (and if you must extend the VECTOR, register new keys in
    contracts/schema/conformance-vector.v1.json — `make validate-vectors` gates it).

A worked real example of this exact shape: plugins/state/sqlite-py/harness_test.py.
"""

import sys

import grpc

from rat import vectors

# <PLACEHOLDER 1: your generated stubs + your servicer>
# from rat.<axis>.v1 import <axis>_pb2, <axis>_pb2_grpc
# from server import MyServicer

TEMPLATE = True  # delete this line (and its guard at the bottom) in your copy


def main():
    vec = vectors.load("<axis>-v1")  # <PLACEHOLDER 2: your axis's golden vector>

    # The Rig: your servicer, in-process, on an ephemeral port.
    channel, stop = vectors.serve_inprocess(
        lambda s: None  # <PLACEHOLDER 3: add_<Axis>ServiceServicer_to_server(MyServicer(), s)
    )
    stub = None  # <PLACEHOLDER 4: <axis>_pb2_grpc.<Axis>ServiceStub(channel)>

    failures = 0
    for step in vec.get("lifecycle", []):
        op, label = step.get("op"), step.get("step", "?")
        try:
            # <PLACEHOLDER 5: the op dispatch — one branch per vector op>
            if op == "<some-op>":
                resp = None  # stub.SomeMethod(<axis>_pb2.SomeRequest(field=step["field"]))
                vectors.run_expect(step.get("expect"), {
                    # one handler per expect key your axis's vector uses:
                    # "found":    lambda want: vectors.check(resp.found == want, f"{label}: found"),
                    # "revision": lambda want: vectors.check(resp.revision == want, f"{label}: revision"),
                }, step=label)
            else:
                # An op this harness can't drive MUST fail the run (never skip).
                sys.exit(f"unmapped op {op!r} at step {label!r} — extend the dispatch")
        except AssertionError as e:
            failures += 1
            print(f"  ✗ {label}: {e}")
        else:
            print(f"  ✓ {label}")

    for step in vec.get("errors", []):
        label = step.get("step", "?")
        try:
            # <PLACEHOLDER 6: drive the op; it MUST raise grpc.RpcError>
            # stub.SomeMethod(...)
            failures += 1
            print(f"  ✗ {label}: expected an error, got success")
        except grpc.RpcError as e:
            want = (step.get("expect") or {}).get("code")
            if want and e.code().name != want:
                failures += 1
                print(f"  ✗ {label}: code {e.code().name}, want {want}")
            else:
                print(f"  ✓ {label} ({e.code().name})")

    stop()
    if failures:
        sys.exit(f"{failures} step(s) failed")
    print("PASS")


if __name__ == "__main__":
    if TEMPLATE:
        sys.exit("this is a template — copy it into your plugin dir and fill the placeholders")
    main()
