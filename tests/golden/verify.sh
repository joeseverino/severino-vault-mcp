#!/usr/bin/env bash
# Contract drift guard: re-emits each public surface and diffs it against the
# committed snapshot. HQ, the severino-obsidian plugin, the tools CLIs and
# Claude Code bind to these, so a change here is a deliberate contract change.
#
# Run from the repo root after every refactor step:  bash tests/golden/verify.sh
# Exit 0 = all surfaces unchanged. Exit 1 = drift (printed as a diff).
#
# The installed console script is the default. Override the invocation to gate a
# source tree that isn't installed (e.g. a worktree venv) — pytest-style:
#   SVMC_CMD="python -m severino_vault_mcp" PYTHONPATH=src bash tests/golden/verify.sh
set -uo pipefail

read -r -a CLI <<< "${SVMC_CMD:-severino-vault-mcp}"
PYBIN="${SVMC_PY:-python3}"   # interpreter that can import severino_vault_mcp
GOLDEN="$(cd "$(dirname "$0")" && pwd)"
fail=0

check() {
  local name="$1" golden="$2"; shift 2
  if ! diff -u "$golden" <("$@") >/tmp/golden-drift.diff 2>&1; then
    echo "DRIFT: $name"
    cat /tmp/golden-drift.diff
    fail=1
  else
    echo "ok:    $name"
  fi
}

# 1. Canonical frontmatter schema (HQ commits + validates this).
check "schema --json" "$GOLDEN/schema.json" "${CLI[@]}" schema --json

# 2. Cordon CLI command surface (help/completions/effect ladder).
check "describe" "$GOLDEN/cli-describe.json" "${CLI[@]}" describe

# 3. Registered MCP tool names (what Claude Code calls), from the assembled server.
check "mcp tool names" "$GOLDEN/mcp-tools.txt" "$PYBIN" "$GOLDEN/list_tools.py"

exit "$fail"
