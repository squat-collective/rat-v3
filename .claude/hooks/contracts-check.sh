#!/usr/bin/env bash
# contracts-check.sh — PreToolUse hook for git commit
#
# Two guards, both cheap:
#   1. Block direct commits to `main` (the sealed-state line; see git-branching.md).
#   2. Run `make check` (buf lint, fast) only when contracts/proto files are staged.
# Costs effectively nothing on a normal commit on a working branch.
#
# Exit codes (per Claude Code hook spec):
#   0  — no objection; proceed normally
#   2  — block the commit; stderr becomes Claude's feedback
#
# Invoked by: .claude/settings.json PreToolUse / matcher: "Bash"
# The hook reads tool_input.command from stdin and exits 0 immediately unless
# the command starts with "git commit". This is the correct gating pattern per
# https://code.claude.com/docs/en/hooks-guide.md#read-input-and-return-output —
# the "if" field in settings.json fails open on unparseable shell constructs
# (compound commands, subshells, loops) so content-based filtering must live
# inside the script, not in the "if" field.
# Doc: https://code.claude.com/docs/en/hooks-guide.md

set -euo pipefail

# ── gate: only act on `git commit` commands ───────────────────────────────────
# Read the JSON hook input from stdin; extract tool_input.command.
# Exit 0 immediately for anything that is not a `git commit` invocation so
# every other Bash command (reads, make targets, loops, etc.) passes through
# without any check.
INPUT=$(cat)
COMMAND=$(printf '%s' "$INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('tool_input',{}).get('command',''))" 2>/dev/null || true)

# Substring match, not prefix: real commits often arrive as compound commands
# ("git add … && git commit …", "cd … && git commit"). False positives are fine —
# the guards below exit 0 cheaply when nothing relevant is staged.
if [[ "$COMMAND" != *"git commit"* ]]; then
  exit 0
fi

# ── guard: never commit directly to main ─────────────────────────────────────
# main is the sealed-state line (see .claude/rules/git-branching.md). Active work
# lives on phase-N / phase-N/<slug> branches. Block direct commits to main.
CURRENT_BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null || echo "")
if [[ "$CURRENT_BRANCH" == "main" ]]; then
  echo "ERROR: You are on 'main'. Direct commits to main are not allowed." >&2
  echo "  main only receives phase-seal merges. For active work, use a branch:" >&2
  echo "    git checkout phase-1                 # the integration branch, or" >&2
  echo "    git checkout -b phase-1-<slug>       # a new topic branch (hyphen, not slash)" >&2
  echo "  See .claude/rules/git-branching.md" >&2
  exit 2
fi

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
