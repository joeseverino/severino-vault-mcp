#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/release.sh X.Y.Z "short headline"

Example:
  scripts/release.sh 2.2.0 "retrieval reliability"

This verifies the release, creates a signed annotated tag, pushes main + tag,
creates the GitHub release from the matching CHANGELOG.md section, and verifies
that the release exists before exiting successfully.
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

tag="v$version"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "release must run from main" >&2
  exit 1
fi

py_version="$(
  python3 - <<'PY'
import tomllib
with open("pyproject.toml", "rb") as handle:
    print(tomllib.load(handle)["project"]["version"])
PY
)"

init_version="$(
  python3 - <<'PY'
from pathlib import Path
import re
text = Path("src/severino_vault_mcp/__init__.py").read_text()
match = re.search(r'__version__ = "([^"]+)"', text)
print(match.group(1) if match else "")
PY
)"

if [[ "$py_version" != "$version" || "$init_version" != "$version" ]]; then
  echo "version mismatch: requested=$version pyproject=$py_version __init__=$init_version" >&2
  exit 1
fi

notes="$(
  VERSION="$version" python3 - <<'PY'
from pathlib import Path
import os
text = Path("CHANGELOG.md").read_text()
version = os.environ["VERSION"]
start_marker = f"## [{version}]"
start = text.find(start_marker)
if start == -1:
    raise SystemExit(f"missing changelog section: {start_marker}")
next_start = text.find("\n## [", start + len(start_marker))
section = text[start:] if next_start == -1 else text[start:next_start]
print(section.strip())
PY
)"

echo "==> Running release checks"
scripts/check.sh --release

if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is dirty; commit release changes before tagging" >&2
  git status --short >&2
  exit 1
fi

if git rev-parse "$tag" >/dev/null 2>&1; then
  echo "tag already exists locally: $tag" >&2
  exit 1
fi

echo "==> Creating signed annotated tag $tag"
git tag -s "$tag" -m "$tag - $headline"

echo "==> Verifying tag signature"
git tag -v "$tag" >/dev/null

echo "==> Pushing main"
git push origin main

echo "==> Pushing tag $tag"
git push origin "$tag"

if gh release view "$tag" >/dev/null 2>&1; then
  echo "GitHub release already exists: $tag"
else
  echo "==> Creating GitHub release"
  gh release create "$tag" --title "$tag - $headline" --latest --notes "$notes"
fi

echo "==> Verifying GitHub release"
gh release view "$tag" >/dev/null

echo "Release complete: $tag"
