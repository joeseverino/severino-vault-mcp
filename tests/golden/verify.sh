#!/usr/bin/env bash
# Contract drift guard: re-emits each public surface and diffs it against the
# committed snapshot. HQ, the severino-obsidian plugin, the tools CLIs and
# Claude Code bind to these, so a change here is a deliberate contract change.
#
# Run from the repo root:  bash tests/golden/verify.sh
# Exit 0 = all surfaces unchanged. Exit 1 = drift (printed as a diff).
#
# Gates the source tree by default. Override to gate an installed binary:
#   SVMC_CMD=severino-vault-mcp bash tests/golden/verify.sh
set -uo pipefail

read -r -a CLI <<< "${SVMC_CMD:-go run ./cmd/severino-vault-mcp}"
GOLDEN="$(cd "$(dirname "$0")" && pwd)"
DIFF="$(mktemp)"
trap 'rm -f "$DIFF"' EXIT
fail=0

check() {
  local name="$1" golden="$2"; shift 2
  if ! diff -u "$golden" <("$@") >"$DIFF" 2>&1; then
    echo "DRIFT: $name"
    cat "$DIFF"
    fail=1
  else
    echo "ok:    $name"
  fi
}

# 1. Canonical frontmatter schema (HQ commits + validates this).
check "schema --json" "$GOLDEN/schema.json" "${CLI[@]}" schema --json

# 2. Cordon CLI command surface (help/completions/effect ladder).
check "describe" "$GOLDEN/cli-describe.json" "${CLI[@]}" describe

# 3. Registered MCP tool names, from the assembled server.
if go test -count=1 -run '^TestToolNamesMatchTheGolden$' ./internal/mcpserver/ >"$DIFF" 2>&1; then
  echo "ok:    mcp tool names"
else
  echo "DRIFT: mcp tool names"
  cat "$DIFF"
  fail=1
fi

exit "$fail"
