"""rat.vectors — conformance-harness helpers (backlog DX-4; hand-written, like rat.plugin).

Every reference plugin's harness repeats the same three chores: find + load the golden
vector, boot its servicer in-process, and assert expectations. The third one hides the
worst failure mode of hand-rolled harnesses: an expect key the harness doesn't know is
SILENTLY SKIPPED — the vector "passes" while testing nothing. `run_expect` makes that
impossible: every key must have a registered handler, or the harness fails loudly.

    from rat import vectors

    vec = vectors.load("state-v1")
    channel, stop = vectors.serve_inprocess(
        lambda s: state_pb2_grpc.add_StateServiceServicer_to_server(MyServicer(), s))
    ...
    vectors.run_expect(step.get("expect"), {
        "found":    lambda want: check(resp.found == want),
        "revision": lambda want: check(resp.revision == want),
    }, step=step["step"])

The static half of the same defense is `make validate-vectors` (the key registry in
contracts/schema/conformance-vector.v1.json); this is the runtime half. The canonical
skeleton using both: contracts/conformance/harness_template.py.
"""

import json
import os


def load(name):
    """Load a golden vector by name ("state-v1" or "state-v1.json").

    Resolution: $RAT_CONFORMANCE_DIR if set (plugins outside this repo / baked images),
    else contracts/conformance/ relative to this SDK's in-repo location.
    """
    base = os.environ.get("RAT_CONFORMANCE_DIR")
    if base is None:
        base = os.path.normpath(
            os.path.join(os.path.dirname(__file__), "..", "..", "..", "conformance"))
    path = os.path.join(base, name if name.endswith(".json") else name + ".json")
    with open(path) as f:
        return json.load(f)


def serve_inprocess(register, max_workers=8):
    """Boot an in-process gRPC server on 127.0.0.1:0 (the harness Rig pattern).

    `register(server)` adds your servicer(s). Returns (channel, stop): an insecure
    channel dialed at the bound port, and a stop() that tears both down.
    """
    from concurrent import futures

    import grpc

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=max_workers))
    register(server)
    port = server.add_insecure_port("127.0.0.1:0")
    server.start()
    channel = grpc.insecure_channel(f"127.0.0.1:{port}")

    def stop():
        channel.close()
        server.stop(grace=None)

    return channel, stop


def run_expect(expect, handlers, step="?"):
    """Assert every expectation in `expect`, refusing to skip unknown keys.

    `handlers` maps expect-key → callable(want) that raises (e.g. AssertionError) when
    the expectation does not hold. A key with NO handler is a hard failure — the
    silent-skip hazard this module exists to kill.
    """
    for key, want in (expect or {}).items():
        fn = handlers.get(key)
        if fn is None:
            raise AssertionError(
                f"step {step!r}: expect key {key!r} has no handler in this harness — "
                f"refusing to silently skip (add a handler, or fix the vector; "
                f"`make validate-vectors` checks the key registry)")
        fn(want)


def check(cond, msg="expectation failed"):
    """Tiny assert helper for handler lambdas: check(resp.found == want, "found")."""
    if not cond:
        raise AssertionError(msg)
