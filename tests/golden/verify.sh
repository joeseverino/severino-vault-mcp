#!/usr/bin/env bash
# Frozen-contract drift guard for the vault-engine extraction.
#
# Re-emits each public surface and diffs it against the committed golden
# snapshot. While the engine is being extracted from severino-vault-mcp, the
# Labs server's surfaces MUST stay byte-identical — these are what HQ, the
# severino-obsidian plugin, the tools CLIs, and Claude Code all bind to.
#
# Run from the repo root after every refactor step:  bash tests/golden/verify.sh
# Exit 0 = all surfaces unchanged. Exit 1 = drift (printed as a diff).
#
# The installed console script is the default. Override the invocation to gate a
# source tree that isn't installed (e.g. a worktree venv) — pytest-style:
#   SVMC_CMD="python -m severino_vault_mcp" PYTHONPATH=src bash tests/golden/verify.sh
set -uo pipefail

read -r -a CLI <<< "${SVMC_CMD:-severino-vault-mcp}"
GOLDEN="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$GOLDEN/../.." && pwd)"
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

# 3. Registered MCP tool names (what Claude Code calls).
check "mcp tool names" "$GOLDEN/mcp-tools.txt" bash -c \
  "grep -B1 -E '^[[:space:]]*def [a-z_]+\(' '$ROOT/src/severino_vault_mcp/server.py' | grep -A1 '@mcp.tool' | grep -oE 'def [a-z_]+' | sed 's/def //' | sort -u"

exit "$fail"
