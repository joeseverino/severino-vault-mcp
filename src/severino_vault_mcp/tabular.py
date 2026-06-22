"""Generic records → markdown-table renderer for derived dataset views.

A reflected dataset's doc shows a human table generated from its JSON cache.
The column spec lives in the registry (declarative, per dataset), so one
renderer serves every tabular dataset — no per-dataset rendering code.

A column is either:
    {"label": "Domain", "key": "domain"}            field lookup (lists joined)
    {"label": "Upstream", "template": "{a}://{b}"}   format string over the record
"""

from __future__ import annotations

from typing import Any


class _Blank(dict):
    """Missing template fields render empty rather than raising."""

    def __missing__(self, key: str) -> str:  # noqa: D105
        return ""


def _cell(value: Any, *, join: str = ", ") -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "yes" if value else "no"
    if isinstance(value, list):
        return join.join(_cell(v, join=join) for v in value)
    return str(value)


def _escape(text: str) -> str:
    return text.replace("|", "\\|").replace("\n", " ")


def render_cell(column: dict, record: dict) -> str:
    if "template" in column:
        raw = str(column["template"]).format_map(_Blank(record))
    else:
        raw = _cell(record.get(column.get("key", "")), join=column.get("join", ", "))
    return _escape(raw)


def render_table(records: list[dict], columns: list[dict]) -> str:
    """Render a markdown table for ``records`` using the column spec."""
    labels = [str(c.get("label", c.get("key", ""))) for c in columns]
    lines = [
        "| " + " | ".join(labels) + " |",
        "|" + "|".join(["---"] * len(columns)) + "|",
    ]
    for record in records:
        cells = [render_cell(c, record) if isinstance(record, dict) else "" for c in columns]
        lines.append("| " + " | ".join(cells) + " |")
    return "\n".join(lines)
