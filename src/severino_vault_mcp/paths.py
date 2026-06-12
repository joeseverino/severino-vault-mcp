"""Shared vault path validation for every write/operator tool.

Both the generic frontmatter writers and the jseverino.com writeup tools must
keep their blast radius inside the configured vault root. Centralizing the
checks here means an env override can never silently redirect a mutation
elsewhere, and every tool returns the same ``{"ok": False, "error": ...}``
failure envelope.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from .config import Config


def path_within_root(
    root: Path,
    path: Path,
    label: str,
    kind: str = "any",
) -> dict[str, Any] | None:
    """Return ``None`` when ``path`` is inside ``root`` (and exists per ``kind``).

    Otherwise return a failure dict. ``kind`` is "dir", "file", or "any".
    """
    resolved_root = root.resolve()
    try:
        path.resolve().relative_to(resolved_root)
    except (OSError, ValueError):
        return {
            "ok": False,
            "error": (
                f"{label} must stay inside configured vault root "
                f"{resolved_root}: {path}"
            ),
        }
    if kind == "dir" and not path.is_dir():
        return {"ok": False, "error": f"{label} not found: {path}"}
    if kind == "file" and not path.is_file():
        return {"ok": False, "error": f"{label} not found: {path}"}
    return None


def validate_indexed_path(
    config: Config,
    relative_path: str,
) -> tuple[Path | None, dict[str, Any] | None]:
    """Resolve a vault-relative path that must be a file under an indexed dir.

    Returns ``(full_path, None)`` on success or ``(None, error_dict)``.
    """
    vault_root = config.vault_path.resolve()
    full_path = (vault_root / relative_path).resolve()
    try:
        full_path.relative_to(vault_root)
    except ValueError:
        return None, {
            "ok": False,
            "error": f"path escapes vault root: {relative_path}",
        }
    if not full_path.is_file():
        return None, {
            "ok": False,
            "error": f"file not found: {relative_path}",
        }
    if not any(
        full_path.is_relative_to(vault_root / sub)
        for sub in config.indexed_dirs
    ):
        return None, {
            "ok": False,
            "error": (
                f"path is outside the indexed dirs: "
                f"{list(config.indexed_dirs)}"
            ),
        }
    return full_path, None


__all__ = ["path_within_root", "validate_indexed_path"]
