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
#
# ── Target-repo false-positive fix (2026-06-10) ──────────────────────────────
# OBSERVED BUG: when a command targets ANOTHER repo via a leading `cd <path> &&`
# or similar, the hook was checking the RAT project's branch (via bare `git
# symbolic-ref`) and firing "You are on 'main'" even though the commit landed
# in a sibling repo that had nothing to do with this project. The session had
# to park rat on a scratch branch just to commit in /tmp/otherrepo.
#
# FIX: after the git-commit gate, determine the TARGET repo of the command and
# exit 0 early if it is not this project's repo. This hook's mandate is RAT v3
# only; other repos' commits are not our concern.
#
# TARGET RESOLUTION (in order of precedence):
#   1. A leading `cd <path> && ...` or `cd <path>; ...` in the command.
#      Strips optional surrounding quotes; expands ~ and $HOME.
#   2. A `git -C <path>` immediately before the commit subcommand.
#   3. The hook-input `cwd` field (session working directory, per the official
#      hook input schema: https://code.claude.com/docs/en/hooks — top-level
#      `cwd` string field, confirmed present in PreToolUse events).
#   4. $CLAUDE_PROJECT_DIR as last-resort fallback when cwd is absent.
#
# FAIL-TOWARD-PROTECTION: when parsing is ambiguous (mid-command cd, subshell,
# complex compound) we cannot reliably determine the target repo, so we fall
# through to run the guards (protect the project). We only skip the guards when
# we can POSITIVELY determine the target is a different repo.
#
# Doc citation for `cwd` field: https://code.claude.com/docs/en/hooks
# Schema excerpt (PreToolUse input):
#   { "session_id": "...", "cwd": "/absolute/path", "tool_name": "Bash",
#     "tool_input": { "command": "..." }, ... }
# The `cwd` field is the session's working directory at the moment the hook fires.

set -euo pipefail

# ── gate: only act on `git commit` commands ───────────────────────────────────
# Read the JSON hook input from stdin; extract tool_input.command and cwd.
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

# ── target-repo determination ─────────────────────────────────────────────────
# Identify which git repo this commit will land in, then bail out early if it
# is not the RAT project repo. Only this project's commits are our mandate.

# Extract the hook-input cwd field (may be absent in older Claude versions).
HOOK_CWD=$(printf '%s' "$INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('cwd',''))" 2>/dev/null || true)

# Determine the effective cwd for the command: hook cwd > $CLAUDE_PROJECT_DIR fallback.
EFFECTIVE_CWD="${HOOK_CWD:-${CLAUDE_PROJECT_DIR:-}}"

# Attempt to extract a leading `cd <path>` from the command.
# Matches: cd /some/path && ...  or  cd /some/path; ...  (with optional quotes)
# Does NOT match a cd that is not at the very start of the command string
# (mid-command cd is ambiguous; we fall through to project-level check there).
#
# NOTE: the regex is assigned to a variable first. bash parses `[[ =~ PATTERN ]]`
# after variable expansion, which avoids the "unexpected token '&'" error that
# occurs when & appears literally inside a [[ ... ]] conditional (bash special-
# cases & in that context). Pattern variable bypasses the issue cleanly.
LEADING_CD_PATH=""
_cd_pattern='^[[:space:]]*cd[[:space:]]+(['"'"'"]?)([^[:space:]'"'"'";&|]+)\1[[:space:]]*(&&|;)'
if [[ "$COMMAND" =~ $_cd_pattern ]]; then
  raw="${BASH_REMATCH[2]}"
  # Expand ~ and $HOME
  raw="${raw/#\~/$HOME}"
  raw="${raw/\$HOME/$HOME}"
  LEADING_CD_PATH="$raw"
fi

# Attempt to extract a `git -C <path>` prefix immediately preceding `git commit`.
GIT_DASH_C_PATH=""
_gitc_pattern='git[[:space:]]+-C[[:space:]]+(['"'"'"]?)([^[:space:]'"'"'";&|]+)\1[[:space:]]+commit'
if [[ "$COMMAND" =~ $_gitc_pattern ]]; then
  raw="${BASH_REMATCH[2]}"
  raw="${raw/#\~/$HOME}"
  raw="${raw/\$HOME/$HOME}"
  GIT_DASH_C_PATH="$raw"
fi

# Pick the target directory in precedence order:
#   1. leading cd (most explicit — the user navigated there on purpose)
#   2. git -C (explicit repo override)
#   3. effective cwd (session working directory)
if [[ -n "$LEADING_CD_PATH" ]]; then
  TARGET_DIR="$LEADING_CD_PATH"
elif [[ -n "$GIT_DASH_C_PATH" ]]; then
  TARGET_DIR="$GIT_DASH_C_PATH"
elif [[ -n "$EFFECTIVE_CWD" ]]; then
  TARGET_DIR="$EFFECTIVE_CWD"
else
  # Cannot determine target at all — fail toward protection, run guards.
  TARGET_DIR="${CLAUDE_PROJECT_DIR:-.}"
fi

# Canonicalise the project root once.
PROJECT_TOP=$(git -C "${CLAUDE_PROJECT_DIR:-.}" rev-parse --show-toplevel 2>/dev/null || true)

# Resolve the target repo's top-level. If the directory does not exist or is
# not inside a git repo, target_top will be empty.
TARGET_TOP=$(git -C "$TARGET_DIR" rev-parse --show-toplevel 2>/dev/null || true)

# Early exit: if the target is a different repo (or not a repo at all), this
# hook has no mandate there. Fail toward protection: only skip when we can
# POSITIVELY confirm they differ.
if [[ -n "$TARGET_TOP" && -n "$PROJECT_TOP" && "$TARGET_TOP" != "$PROJECT_TOP" ]]; then
  exit 0
fi

# ── guard: never commit directly to main ─────────────────────────────────────
# main is the sealed-state line (see .claude/rules/git-branching.md). Active work
# lives on phase-N / phase-N-<slug> branches. Block direct commits to main.
# Use explicit -C so the check is always against the project repo, not whatever
# the shell's cwd happens to be.
CURRENT_BRANCH=$(git -C "${PROJECT_TOP:-.}" symbolic-ref --short HEAD 2>/dev/null || echo "")
if [[ "$CURRENT_BRANCH" == "main" ]]; then
  echo "ERROR: You are on 'main' in the RAT project. Direct commits to main are not allowed." >&2
  echo "  main only receives phase-seal merges. For active work, use a branch:" >&2
  echo "    git checkout phase-1                 # the integration branch, or" >&2
  echo "    git checkout -b phase-1-<slug>       # a new topic branch (hyphen, not slash)" >&2
  echo "  See .claude/rules/git-branching.md" >&2
  exit 2
fi

# ── guard: only act when contracts/proto files are staged ─────────────────────
# Pure shell, no containers. If nothing in contracts/proto is staged, exit 0
# immediately so this hook costs effectively zero on non-contract commits.
if ! git -C "${PROJECT_TOP:-.}" diff --cached --name-only | grep -q 'contracts/proto'; then
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
