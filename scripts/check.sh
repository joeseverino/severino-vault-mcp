#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/check.sh [--quick] [--release]

Default:
  version alignment, ruff, pytest, sample-vault doctor

Options:
  --quick    only run ruff + pytest
  --release  also sync deps and smoke-test the installed tool
EOF
}

quick=0
release=0

for arg in "$@"; do
  case "$arg" in
    --quick) quick=1 ;;
    --release) release=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ "$release" -eq 1 ]]; then
  echo "==> Syncing environment"
  uv sync --extra dev
fi

if [[ "$quick" -eq 0 ]]; then
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
fi

echo "==> Running lint"
uv run ruff check .

echo "==> Running tests"
uv run pytest -q

if [[ "$quick" -eq 0 ]]; then
  echo "==> Checking sample vault"
  env PYTHONPATH=src SVMC_VAULT_PATH=examples/sample-vault \
    uv run python -m severino_vault_mcp doctor
fi

if [[ "$release" -eq 1 ]]; then
  echo "==> Checking installed tool"
  uv tool install --from . severino-vault-mcp --force
  severino-vault-mcp --help >/dev/null
  SVMC_VAULT_PATH=examples/sample-vault severino-vault-mcp doctor
fi

echo "Checks passed."
