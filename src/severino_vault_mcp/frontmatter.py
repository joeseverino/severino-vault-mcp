"""Shared constrained-YAML frontmatter parsing and serialization.

Durable file replacement lives in :mod:`atomic_write`; this module is only the
YAML subset (parse + serialize) so the escaping rules have a single home.
"""

from __future__ import annotations

import re
from pathlib import Path
from typing import Any

_BLOCK_SCALARS = {">", ">-", ">+", "|", "|-", "|+"}


def scalar(token: str) -> str | bool | None:
    token = token.strip()
    if len(token) >= 2 and token[0] == token[-1] and token[0] in ('"', "'"):
        token = token[1:-1]
    if token in ("null", "~", ""):
        return None
    if token == "true":
        return True
    if token == "false":
        return False
    return token


def split_inline_list(inner: str) -> list[str]:
    if not inner:
        return []
    output: list[str] = []
    depth = 0
    buffer: list[str] = []
    for character in inner:
        if character == "," and depth == 0:
            output.append("".join(buffer).strip())
            buffer = []
            continue
        buffer.append(character)
        if character in "[{":
            depth += 1
        elif character in "]}":
            depth -= 1
    output.append("".join(buffer).strip())
    return [value for value in output if value]


def parse_yaml_block(block: str) -> dict[str, Any]:
    """Parse the vault's supported YAML subset.

    Supports scalar values, inline lists, block lists, and folded/literal
    multiline scalars. Dates remain strings.
    """
    data: dict[str, Any] = {}
    current_list_key: str | None = None
    lines = block.splitlines()
    index = 0
    while index < len(lines):
        raw = lines[index]
        index += 1
        if not raw.strip() or raw.lstrip().startswith("#"):
            current_list_key = None
            continue
        if current_list_key and re.match(r"^\s+-\s+", raw):
            item = raw.strip()[2:].strip()
            data.setdefault(current_list_key, []).append(scalar(item))
            continue
        match = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.*)$", raw)
        if not match:
            current_list_key = None
            continue
        key, value = match.group(1), match.group(2).strip()
        if value in _BLOCK_SCALARS:
            block_lines: list[str] = []
            while index < len(lines):
                candidate = lines[index]
                if re.match(r"^[A-Za-z_][A-Za-z0-9_]*\s*:", candidate):
                    break
                block_lines.append(candidate.strip())
                index += 1
            if value.startswith("|"):
                data[key] = "\n".join(block_lines).strip()
            else:
                data[key] = " ".join(
                    line for line in block_lines if line
                ).strip()
            current_list_key = None
            continue
        if value == "":
            current_list_key = key
            data[key] = []
            continue
        current_list_key = None
        if value.startswith("[") and value.endswith("]"):
            data[key] = [
                scalar(item)
                for item in split_inline_list(value[1:-1].strip())
            ]
            continue
        data[key] = scalar(value)
    return data


def split_frontmatter(
    text: str,
) -> tuple[dict[str, Any] | None, str, int]:
    """Return frontmatter, markdown body, and 1-indexed body start line."""
    if not text.lstrip().startswith("---"):
        return None, text, 1
    lines = text.splitlines()
    start = next(
        (index for index, line in enumerate(lines) if line.strip() == "---"),
        None,
    )
    if start is None:
        return None, text, 1
    end = next(
        (
            index
            for index in range(start + 1, len(lines))
            if lines[index].strip() == "---"
        ),
        None,
    )
    if end is None:
        return None, text, 1
    frontmatter = parse_yaml_block("\n".join(lines[start + 1 : end]))
    body = "\n".join(lines[end + 1 :]).lstrip("\n")
    return frontmatter, body, end + 2


def read_frontmatter(path: Path) -> dict[str, Any] | None:
    text = path.read_text(encoding="utf-8", errors="replace")
    frontmatter, _body, _body_start = split_frontmatter(text)
    return frontmatter


_KNOWN_KEY_ORDER = (
    "doc_id",
    "title",
    "doc_type",
    "system",
    "environment",
    "status",
    "sensitivity",
    "last_reviewed",
    "related_projects",
    "related_assets",
    "tags",
    "notes",
    # Task-profile fields. Harmless for other doc types (which never carry them);
    # ordering them here keeps the one serializer's task output tidy and stable.
    "effort",
    "priority",
    "created",
    "closed",
)

_YAML_SPECIAL_CHARS = (
    ":", "#", "@", "|", ">", "{", "}", "[", "]", ",", "&", "*", "!", "%", "`",
)


def yaml_escape(value: str) -> str:
    """Quote a scalar only when the vault's constrained YAML subset requires it."""
    if value == "":
        return '""'
    if any(char in value for char in _YAML_SPECIAL_CHARS):
        return '"' + value.replace('"', '\\"') + '"'
    if value.strip() != value:
        return '"' + value + '"'
    return value


def serialize_frontmatter(data: dict[str, Any]) -> str:
    """Render a frontmatter dict back to the vault's constrained YAML block.

    Keys in :data:`_KNOWN_KEY_ORDER` lead in canonical order; any extra keys
    follow in insertion order. Empty lists render as ``[]``; booleans and
    ``None`` use YAML literals. This is the single serializer behind every
    vault-doc write so escaping rules can never fork between tools.
    """
    lines = ["---"]
    seen: set[str] = set()
    for key in (*_KNOWN_KEY_ORDER, *data.keys()):
        if key in seen or key not in data:
            continue
        seen.add(key)
        value = data[key]
        if isinstance(value, list):
            if not value:
                lines.append(f"{key}: []")
            else:
                lines.append(f"{key}:")
                lines.extend(
                    f"  - {yaml_escape(str(item))}" for item in value
                )
        elif value is None:
            lines.append(f"{key}: null")
        elif isinstance(value, bool):
            lines.append(f"{key}: {'true' if value else 'false'}")
        else:
            lines.append(f"{key}: {yaml_escape(str(value))}")
    lines.extend(["---", ""])
    return "\n".join(lines) + "\n"


__all__ = [
    "parse_yaml_block",
    "read_frontmatter",
    "scalar",
    "serialize_frontmatter",
    "split_frontmatter",
    "split_inline_list",
    "yaml_escape",
]
