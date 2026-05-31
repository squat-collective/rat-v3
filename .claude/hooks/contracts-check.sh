#!/usr/bin/env bash
# contracts-check.sh — PreToolUse hook for git commit
#
# Runs `make check` (buf lint, fast — seconds) only when contracts/proto files
# are staged. Costs nothing when no proto files are in the commit.
#
# Exit codes (per Claude Code hook spec):
#   0  — no objection; proceed normally
#   2  — block the commit; stderr becomes Claude's feedback
#
# Invoked by: .claude/settings.json PreToolUse / Bash / if: "Bash(git commit *)"
# Doc: https://code.claude.com/docs/en/hooks-guide.md

set -euo pipefail

# ── guard: only act when contracts/proto files are staged ─────────────────────
# Pure shell, no containers. If nothing in contracts/proto is staged, exit 0
# immediately so this hook costs effectively zero on non-contract commits.
if ! git diff --cached --name-only | grep -q 'contracts/proto'; then
  exit 0
fi

# ── run the fast lint gate ────────────────────────────────────────────────────
echo "contracts/proto files staged — running make check (buf lint)..." >&2

if make -C "$CLAUDE_PROJECT_DIR" check; then
  echo "make check passed." >&2
  exit 0
else
  echo "" >&2
  echo "ERROR: buf lint failed. Fix the proto errors above before committing." >&2
  echo "  Fast fix: make check" >&2
  echo "  Full verification: make verify" >&2
  exit 2
fi
