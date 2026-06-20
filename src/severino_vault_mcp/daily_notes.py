"""Daily-note retrieval helpers.

Daily notes are intentionally outside the operational runbook index: they live
under ``00 Inbox/Daily Note`` and use the lightweight ``daily-*`` frontmatter
contract. This module gives the MCP a narrow way to answer "what happened on
Friday?" without making daily notes compete with durable docs.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import date, datetime, timedelta
from pathlib import Path
from typing import Any

from .frontmatter import split_frontmatter
from .vault import VaultLoader

_ISO_DATE_RE = re.compile(r"\b(20\d{2}-\d{2}-\d{2})\b")
_US_DATE_RE = re.compile(r"\b(\d{1,2})/(\d{1,2})/(20\d{2})\b")
_WEEKDAYS = {
    "monday": 0,
    "mon": 0,
    "tuesday": 1,
    "tue": 1,
    "tues": 1,
    "wednesday": 2,
    "wed": 2,
    "thursday": 3,
    "thu": 3,
    "thur": 3,
    "thurs": 3,
    "friday": 4,
    "fri": 4,
    "saturday": 5,
    "sat": 5,
    "sunday": 6,
    "sun": 6,
}


@dataclass(frozen=True)
class DailyNote:
    date: date
    doc_id: str
    path: Path
    relative_path: str
    created: str | None
    body: str


def _today() -> date:
    return datetime.now().date()


def _parse_today(value: str | None) -> date:
    if not value:
        return _today()
    return date.fromisoformat(value)


def _resolve_weekday(anchor: date, target_weekday: int, *, previous: bool) -> date:
    delta = (anchor.weekday() - target_weekday) % 7
    if previous and delta == 0:
        delta = 7
    return anchor - timedelta(days=delta)


def resolve_daily_note_date(query: str, *, today: str | None = None) -> tuple[date, str]:
    """Resolve a natural-language day reference to a concrete date.

    Supported forms are deliberately small and predictable: ISO dates,
    US-style dates, today/yesterday, and weekday names. A bare weekday resolves
    to the most recent occurrence on or before ``today``; ``last Friday`` means
    the previous occurrence, even if today is Friday.
    """
    anchor = _parse_today(today)
    q = query.strip().lower()

    match = _ISO_DATE_RE.search(q)
    if match:
        return date.fromisoformat(match.group(1)), "iso_date"

    match = _US_DATE_RE.search(q)
    if match:
        month, day, year = (int(match.group(i)) for i in (1, 2, 3))
        return date(year, month, day), "us_date"

    if "yesterday" in q:
        return anchor - timedelta(days=1), "yesterday"
    if "today" in q:
        return anchor, "today"

    for token, weekday in _WEEKDAYS.items():
        if re.search(rf"\b{re.escape(token)}\b", q):
            previous = bool(re.search(rf"\b(last|previous)\s+{re.escape(token)}\b", q))
            return _resolve_weekday(anchor, weekday, previous=previous), "weekday"

    return anchor, "default_today"


def _progress_lines(body: str) -> list[str]:
    lines: list[str] = []
    for raw in body.splitlines():
        line = raw.strip()
        if not line:
            continue
        if line.startswith(("#", "---")):
            continue
        if line.startswith(("- [x]", "- [X]", "-", "*")) or len(line) > 3:
            lines.append(line)
    return lines


def _read_note(loader: VaultLoader, note_date: date) -> DailyNote | None:
    relative = f"{loader.config.daily_notes_dir}/{note_date.isoformat()}.md"
    path = loader.config.vault_path / relative
    if not path.is_file():
        return None
    text = path.read_text(encoding="utf-8", errors="replace")
    fm, body, _body_start_line = split_frontmatter(text)
    fm = fm or {}
    return DailyNote(
        date=note_date,
        doc_id=str(fm.get("doc_id") or f"daily-{note_date:%Y%m%d}"),
        path=path,
        relative_path=relative,
        created=str(fm["created"]) if fm.get("created") else None,
        body=body,
    )


def daily_progress(loader: VaultLoader, query: str, *, today: str | None = None) -> dict[str, Any]:
    note_date, resolution = resolve_daily_note_date(query, today=today)
    note = _read_note(loader, note_date)
    response: dict[str, Any] = {
        "query": query,
        "resolved_date": note_date.isoformat(),
        "date_resolution": resolution,
        "daily_notes_dir": loader.config.daily_notes_dir,
    }
    if note is None:
        response.update({
            "found": False,
            "expected_path": f"{loader.config.daily_notes_dir}/{note_date.isoformat()}.md",
            "progress_items": [],
        })
        return response

    response.update({
        "found": True,
        "doc_id": note.doc_id,
        "obsidian_path": note.relative_path,
        "created": note.created,
        "body": note.body,
        "body_released": True,
        "progress_items": _progress_lines(note.body),
        "answer_guidance": (
            "Summarize progress from progress_items/body. If the body is empty, "
            "say no progress was recorded in the daily note for this date."
        ),
    })
    return response


__all__ = ["daily_progress", "resolve_daily_note_date"]
