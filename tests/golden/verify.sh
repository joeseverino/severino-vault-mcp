#!/usr/bin/env bash
# Contract drift guard: re-emits each public surface and diffs it against the
# committed snapshot. HQ, the severino-obsidian plugin, the tools CLIs and
# Claude Code bind to these, so a change here is a deliberate contract change.
#
# Run from the repo root:  mise run golden
# Exit 0 = all surfaces unchanged. Exit 1 = drift (printed as a diff).
# Exit 2 = go is not on PATH.
#
# Gates the source tree by default. Override to gate an installed binary:
#   SVMC_CMD=severino-vault-mcp bash tests/golden/verify.sh
set -uo pipefail

# Every surface below is built with go; without it each diff would read as
# drift. `mise run golden` puts the pinned go on PATH.
if ! command -v go >/dev/null 2>&1; then
  echo "verify.sh: go is not on PATH; run it as \`mise run golden\`" >&2
  exit 2
fi

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

# 1b. The labs frontmatter as JSON Schema (editors and off-the-shelf validators).
check "schema --json-schema" "$GOLDEN/labs.schema.json" "${CLI[@]}" schema --json-schema

# 2. Cordon CLI command surface (help/completions/effect ladder). The snapshot
# is indented so a change to one command is a one-hunk diff; consumers read
# the compact form, which --pretty only re-indents.
check "describe" "$GOLDEN/cli-describe.json" "${CLI[@]}" describe --pretty

# 3. Registered MCP tool names, from the assembled server.
if go test -count=1 -run '^TestToolNamesMatchTheGolden$' ./internal/mcpserver/ >"$DIFF" 2>&1; then
  echo "ok:    mcp tool names"
else
  echo "DRIFT: mcp tool names"
  cat "$DIFF"
  fail=1
fi

# 4. README's tool table, generated from the registered tools.
if go test -count=1 -run '^TestReadmeToolsTable$' ./internal/mcpserver/ >"$DIFF" 2>&1; then
  echo "ok:    README tool table"
else
  echo "DRIFT: README tool table"
  cat "$DIFF"
  fail=1
fi

exit "$fail"
