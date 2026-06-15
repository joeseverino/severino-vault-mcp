"""Release guard: fail if pyproject version != package __version__."""

import re
import sys
import tomllib
from pathlib import Path

pv = tomllib.loads(Path("pyproject.toml").read_text())["project"]["version"]
m = re.search(
    r'__version__ = "([^"]+)"',
    Path("src/severino_vault_mcp/__init__.py").read_text(),
)
iv = m.group(1) if m else None
sys.exit(0 if pv == iv else f"version mismatch: pyproject={pv} __init__={iv}")
