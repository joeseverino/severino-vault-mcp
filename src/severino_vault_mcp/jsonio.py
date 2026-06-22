"""The package's single home for JSON serialization and parsing choices.

Three distinct contracts, each spelled exactly once so call sites can't drift:

* :func:`dumps` — the CLI compact-vs-``--pretty`` output contract (every
  subcommand that prints JSON goes through it).
* :func:`canonical` — the deterministic, sorted form for *committed* artifacts
  another system diffs and validates against (the frontmatter schema HQ
  consumes; the topology contract).
* :func:`loads` / :func:`load_file` — parsing with one consistent, source-
  labelled error (the topology inventory and the drift guards' mirror payload
  both parse through these, so a malformed-JSON failure reads the same
  everywhere).

Serializing is centralised; *parsing into typed objects* still lives with each
owner (a TOML config, constrained-YAML frontmatter, the markdown tech catalog,
the JSON topology inventory) — those are genuinely different sources and are
deliberately not collapsed here. This module owns only the JSON mechanics they
share.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any


class JsonError(ValueError):
    """A JSON parse failure carrying a consistent, source-labelled message."""


def dumps(obj: Any, *, pretty: bool = False) -> str:
    """Serialize for CLI output: compact by default, indented under ``--pretty``."""
    if pretty:
        return json.dumps(obj, indent=2)
    return json.dumps(obj, separators=(",", ":"))


def canonical(obj: Any) -> str:
    """Deterministic, sorted, indented form for committed/validated artifacts.

    Use where the JSON is a stable diff target another system commits and
    checks against (e.g. ``schema --json`` consumed by HQ, the topology
    contract). Sorting keys keeps the output byte-stable across runs.
    """
    return json.dumps(obj, indent=2, sort_keys=True)


def loads(text: str, *, source: str = "JSON") -> Any:
    """Parse JSON text, raising :class:`JsonError` with a labelled message."""
    try:
        return json.loads(text)
    except json.JSONDecodeError as exc:
        raise JsonError(f"{source} is not valid JSON: {exc}") from exc


def load_file(path: Path, *, source: str | None = None) -> Any:
    """Read and parse a JSON file, raising :class:`JsonError` on any failure."""
    label = source or str(path)
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise JsonError(f"could not read {label}: {exc}") from exc
    return loads(text, source=label)
