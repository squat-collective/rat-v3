#!/usr/bin/env python3
# Context-carriage conformance — Python reference (PU-2, ADR-017).
#
# The technologically-divergent SECOND implementation (cf. ../go/main.go) of the KEYSTONE
# context-carriage contract (common/v1/context.proto + ADR-007 gateway stamping). Clean-room
# from context.proto's prose — it shares no code with the Go impl or core/gateway. Both
# cross-run the SHARED ../context-carriage-v1.json: agreement on every vector is the
# ADR-003 two-reference conformance signal for the most-irreversible frozen surface
# (architect F1, reviews/11-q02-architect.md). Only stdlib + `cryptography` (ed25519).
import json
import sys

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey


def canonical_subject(principal, tenant, bound, expires, key_id):
    # The deterministic bytes the core signs and a hop reconstructs FROM THE BARE MIRRORS to
    # verify (SubjectAssertion VERIFICATION CONTRACT). Rebuilding from the bare mirrors is the
    # M4 cross-check: a bare value the signature does not cover fails verification.
    return (principal + "\x00" + tenant + "\x00" + bound + "\x00" + str(expires) + "\x00" + key_id).encode()


def well_formed_traceparent(tp):
    p = tp.split("-")
    return len(p) == 4 and len(p[0]) == 2 and len(p[1]) == 32 and len(p[2]) == 16 and len(p[3]) == 2


def stamp(inb, channel_caller, pubkeys, now_ms):
    """Apply the per-hop contract: reject (with a reason) or re-stamp downstream."""
    tr = inb["trace"]
    if not well_formed_traceparent(tr["traceparent"]):
        return None, "", "traceparent"
    if tr["correlation_id"] == "":
        return None, "", "correlation"
    principal = ""
    s = inb["identity"].get("subject")
    if s is not None:
        if s["bound_correlation_id"] != tr["correlation_id"]:  # anti-stockpile
            return None, "", "subject-bound"
        if now_ms > s["expires_unix_ms"]:  # short-TTL
            return None, "", "subject-expired"
        pub = pubkeys.get(s["key_id"])
        if pub is None:
            return None, "", "subject-signature"
        msg = canonical_subject(s["principal"], inb["identity"]["tenant"], s["bound_correlation_id"], s["expires_unix_ms"], s["key_id"])
        try:
            pub.verify(s["signature"], msg)  # M4: reconstructed from the bare mirrors
        except InvalidSignature:
            return None, "", "subject-signature"
        principal = s["principal"]
    down = {
        "trace": tr,  # verbatim
        "identity": {"caller_plugin": channel_caller, "tenant": inb["identity"]["tenant"], "subject": s},
        "deadline_unix_ms": inb["deadline_unix_ms"],
    }
    return down, principal, ""


def check_expect(exp, down, principal, reject):
    if exp["outcome"] == "reject":
        if reject == "":
            return False, "want reject, got accept"
        if exp.get("reason") and reject != exp["reason"]:
            return False, f"reject reason {reject!r}, want {exp['reason']!r}"
        return True, "reject:" + reject
    if reject != "":
        return False, "want accept, got reject:" + reject
    d = exp["downstream"]
    if down["trace"]["traceparent"] != d["traceparent"]:
        return False, "traceparent not propagated verbatim"
    if down["trace"]["tracestate"] != d["tracestate"]:
        return False, "tracestate not propagated verbatim"
    if down["trace"]["correlation_id"] != d["correlation_id"]:
        return False, "correlation_id changed"
    if down["identity"]["caller_plugin"] != d["caller_plugin"]:
        return False, f"caller_plugin={down['identity']['caller_plugin']!r}, want re-derived {d['caller_plugin']!r}"
    if down["identity"]["tenant"] != d["tenant"]:
        return False, "tenant not propagated"
    if principal != d["subject_principal"]:
        return False, f"subject principal={principal!r}, want {d['subject_principal']!r}"
    return True, "accept"


def main():
    if len(sys.argv) != 2:
        print("usage: check.py <vectors.json>", file=sys.stderr)
        sys.exit(2)
    with open(sys.argv[1]) as f:
        v = json.load(f)
    priv = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(v["signing"]["seed_hex"]))
    pubkeys = {v["signing"]["key_id"]: priv.public_key()}

    fails = 0
    for c in v["cases"]:
        inb = {
            "trace": {
                "traceparent": c["inbound"]["traceparent"],
                "tracestate": c["inbound"]["tracestate"],
                "correlation_id": c["inbound"]["correlation_id"],
            },
            "identity": {"caller_plugin": c["inbound"]["caller_plugin"], "tenant": c["inbound"]["tenant"]},
            "deadline_unix_ms": c["inbound"]["deadline_unix_ms"],
        }
        s = c["inbound"].get("subject")
        if s is not None:
            sg = s["signed"]
            sig = priv.sign(canonical_subject(sg["principal"], sg["tenant"], sg["bound_correlation_id"], sg["expires_unix_ms"], sg["key_id"]))
            if s["signature"] == "tampered":
                sig = bytes([sig[0] ^ 0xFF]) + sig[1:]
            inb["identity"]["subject"] = {
                "principal": s["bare_principal"],
                "signature": sig,
                "bound_correlation_id": sg["bound_correlation_id"],
                "expires_unix_ms": sg["expires_unix_ms"],
                "key_id": sg["key_id"],
            }
        down, principal, reject = stamp(inb, c["channel_caller"], pubkeys, c["now_unix_ms"])
        ok, detail = check_expect(c["expect"], down, principal, reject)
        status = "PASS" if ok else "FAIL"
        if not ok:
            fails += 1
        print(f"  [{status}] {c['name']:<40} {detail}")
    print(f"context-carriage (py): {len(v['cases']) - fails}/{len(v['cases'])} vectors pass")
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
