#!/usr/bin/env bash
# check.sh — the one gate, runnable independently. You run it locally, the
# pre-push hook runs it, and CI runs it (the reusable cordon gate calls
# `scripts/check.sh --ci`). Same script everywhere, so local and CI can't drift.
#
# The standardized checks (ruff, pytest, pip-audit + repo invariants) come from
# cordon's checks engine over cordon.checks.json — referenced, never vendored.
# This script adds the repo-specific extras (version alignment, sample-vault
# doctor) and the release smoke test.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/check.sh [--ci] [--release]

Default / --ci:
  version alignment, cordon checks (ruff + pytest + pip-audit + invariants),
  sample-vault doctor

--release:
  also sync deps and smoke-test the installed tool
EOF
}

ci=0
release=0
for arg in "$@"; do
  case "$arg" in
    --ci) ci=1 ;;
    --release) release=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

# cordon's engine is the source of the standardized gate. CI's reusable gate
# exports CORDON_HOME; locally fall back to the sibling checkout.
CORDON_HOME="${CORDON_HOME:-${ASSETS_HOME:-$HOME/Documents/Code/Assets}/cordon}"
if [[ ! -f "$CORDON_HOME/checks/run.mjs" ]]; then
  echo "cordon not found at \$CORDON_HOME ($CORDON_HOME). Set CORDON_HOME or check out cordon as a sibling." >&2
  exit 1
fi

if [[ "$release" -eq 1 ]]; then
  echo "==> Syncing environment"
  uv sync --extra dev
fi

echo "==> Checking version alignment"
python3 - <<'PY'
from pathlib import Path
import re
import tomllib

pyproject = tomllib.loads(Path("pyproject.toml").read_text())["project"]["version"]
init_text = Path("src/severino_vault_mcp/__init__.py").read_text()
match = re.search(r'__version__ = "([^"]+)"', init_text)
init_version = match.group(1) if match else ""

if pyproject != init_version:
    raise SystemExit(f"version mismatch: pyproject={pyproject} __init__={init_version}")

print(pyproject)
PY

echo "==> cordon checks (ruff + pytest + pip-audit + repo invariants)"
node "$CORDON_HOME/checks/run.mjs" --root "$PWD"

echo "==> Checking sample vault"
env PYTHONPATH=src SVMC_VAULT_PATH=examples/sample-vault \
  uv run python -m severino_vault_mcp doctor

if [[ "$release" -eq 1 ]]; then
  echo "==> Checking installed tool"
  uv tool install --from . severino-vault-mcp --force
  severino-vault-mcp --help >/dev/null
  SVMC_VAULT_PATH=examples/sample-vault severino-vault-mcp doctor
fi

echo "Checks passed."
