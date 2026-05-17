#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/prepare-release.sh X.Y.Z "short headline"

This bumps pyproject.toml, __init__.py, uv.lock, README status, and creates a
CHANGELOG.md section from the current git diff. Edit the generated changelog
section before committing if you want tighter release notes.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

version="${1:-}"
headline="${2:-}"

if [[ -z "$version" || -z "$headline" ]]; then
  usage >&2
  exit 2
fi

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must look like X.Y.Z, got: $version" >&2
  exit 2
fi

previous="$(
  python3 - <<'PY'
import tomllib
with open("pyproject.toml", "rb") as handle:
    print(tomllib.load(handle)["project"]["version"])
PY
)"

today="$(date +%F)"

python3 - "$version" "$previous" "$headline" "$today" <<'PY'
from pathlib import Path
import re
import subprocess
import sys

version, previous, headline, today = sys.argv[1:5]

def replace(path: str, pattern: str, repl: str) -> None:
    p = Path(path)
    text = p.read_text()
    new = re.sub(pattern, repl, text, count=1)
    if new == text:
        raise SystemExit(f"no replacement made in {path}")
    p.write_text(new)

replace("pyproject.toml", r'version = "[^"]+"', f'version = "{version}"')
replace(
    "src/severino_vault_mcp/__init__.py",
    r'__version__ = "[^"]+"',
    f'__version__ = "{version}"',
)
replace("README.md", rf"v{re.escape(previous)}\.", f"v{version}.")

changed = subprocess.run(
    ["git", "diff", "--name-only", "HEAD"],
    check=True,
    text=True,
    capture_output=True,
).stdout.splitlines()

bullets: list[str] = []
for path in changed:
    if path in {"CHANGELOG.md", "uv.lock"}:
        continue
    if path == "pyproject.toml" or path == "src/severino_vault_mcp/__init__.py":
        continue
    bullets.append(f"- Updated `{path}`.")

if not bullets:
    bullets.append("- Release maintenance changes.")

section = f"""## [{version}] - {today}

{headline}.

### Changed

- Bumped package version to {version} in `pyproject.toml` and
  `src/severino_vault_mcp/__init__.py`.
{chr(10).join(bullets)}

### Verification

- `scripts/check.sh --release` passes.

"""

changelog = Path("CHANGELOG.md")
text = changelog.read_text()
if f"## [{version}]" in text:
    raise SystemExit(f"CHANGELOG.md already has a {version} section")
text = text.replace("## [Unreleased]\n\n", "## [Unreleased]\n\n" + section, 1)
text = text.replace(
    f"[Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/v{previous}...HEAD",
    f"[Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/v{version}...HEAD\n"
    f"[{version}]: https://github.com/joeseverino/severino-vault-mcp/compare/v{previous}...v{version}",
)
changelog.write_text(text)
PY

uv sync --extra dev

echo "Prepared v$version."
echo "Next:"
echo "  1. Edit CHANGELOG.md if needed."
echo "  2. Run scripts/check.sh --release."
echo "  3. Commit with a descriptive message."
echo "  4. Run scripts/release.sh $version \"$headline\"."
