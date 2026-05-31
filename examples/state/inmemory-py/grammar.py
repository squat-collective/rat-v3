"""Key-grammar validation for rat-state-inmemory-py — mirrors the Go reference's
grammar.go exactly so the two stay in lockstep on the shared golden vectors.

Enforces the KEY GRAMMAR (state.proto header, freeze-blocker #3 / SEC-2): a
key/prefix is rejected if empty (key; prefix may be empty), > 512 bytes, contains a
NUL / ASCII control char (< 0x20), or contains a path-traversal sequence. Rejection
surfaces as INVALID_ARGUMENT at the RPC boundary.
"""

MAX_KEY_BYTES = 512


class GrammarError(Exception):
    """A key-grammar violation → INVALID_ARGUMENT."""


def validate_key(s: str, allow_empty: bool) -> None:
    if s == "":
        if allow_empty:
            return
        raise GrammarError("key is required")
    if len(s.encode("utf-8")) > MAX_KEY_BYTES:
        raise GrammarError(f"key exceeds {MAX_KEY_BYTES} bytes")
    # a Python str is always valid Unicode; the UTF-8 check is moot here.
    for ch in s:
        if ord(ch) < 0x20:  # NUL + any ASCII control char
            raise GrammarError("key contains a control character")
    if "../" in s or "..\\" in s:
        raise GrammarError("key contains a path-traversal sequence")
    for comp in s.split("/"):
        if comp in (".", ".."):
            raise GrammarError("key contains a '.' or '..' path component")
