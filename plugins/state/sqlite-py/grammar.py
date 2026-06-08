"""Key-grammar validation — identical to the in-memory references' grammar so all
three `state` references stay in lockstep on the shared golden vectors. See
plugins/state/inmemory-go/grammar.go for the canonical version.
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
    for ch in s:
        if ord(ch) < 0x20:  # NUL + any ASCII control char
            raise GrammarError("key contains a control character")
    if "../" in s or "..\\" in s:
        raise GrammarError("key contains a path-traversal sequence")
    for comp in s.split("/"):
        if comp in (".", ".."):
            raise GrammarError("key contains a '.' or '..' path component")
