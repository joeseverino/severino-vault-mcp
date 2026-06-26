"""Write the generated brief region of a daily note.

A *schema-specific* write (per the write model: no arbitrary path + text). The
file shape is the daily note — ``<daily_notes_dir>/<ISO date>.md`` with the
``daily-*`` frontmatter contract — so this validates the date, resolves the one
known path, and replaces a single named ``MIRROR`` region atomically. Everything
outside that region, including the free-capture area below it, is preserved; the
region is inserted just after the frontmatter on first run.

Daily notes are a capture surface, not indexed docs, so nothing is stamped and
the note is not re-indexed. The rendered content is owned by the caller
(``tools``' ``vault daily``); this module only places it safely.
"""

from __future__ import annotations

from datetime import date as date_cls
from datetime import datetime
from typing import Any

from .atomic_write import atomic_write_text
from .config import Config
from .frontmatter import serialize_frontmatter
from .mirror import upsert_region
from .paths import path_within_root

REGION_ID = "daily-brief"


def _stub_text(note_date: date_cls) -> str:
    """A fresh daily note's frontmatter, per the vault's daily-note contract."""
    now = datetime.now()
    return serialize_frontmatter(
        {
            "doc_id": f"daily-{note_date:%Y%m%d}",
            "created": now.strftime("%Y-%m-%d %H:%M:%S"),
            "date": note_date.isoformat(),
        }
    )


def write_daily_block(
    config: Config, content: str, *, note_date: str | None = None
) -> dict[str, Any]:
    """Replace the generated brief region of a daily note with ``content``.

    ``note_date`` is an ISO date (default today). Creates the note from the
    daily-note frontmatter contract if it does not exist yet, so the terminal
    path works without Obsidian. Returns the standard ``{"ok": ...}`` envelope.
    """
    try:
        day = date_cls.fromisoformat(note_date) if note_date else datetime.now().date()
    except ValueError:
        return {"ok": False, "error": f"invalid date {note_date!r} (want YYYY-MM-DD)"}

    relative = f"{config.daily_notes_dir}/{day.isoformat()}.md"
    path = config.vault_path / relative
    if err := path_within_root(config.vault_path, path, "daily note"):
        return err

    created_file = not path.is_file()
    text = _stub_text(day) if created_file else path.read_text(encoding="utf-8")

    new_text, inserted = upsert_region(text, REGION_ID, content)
    if new_text == text:
        return {"ok": True, "wrote": relative, "changed": False, "created": False, "inserted": False}

    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        atomic_write_text(path, new_text)
    except OSError as exc:
        return {"ok": False, "error": f"daily note write failed: {exc}"}

    return {
        "ok": True,
        "wrote": relative,
        "changed": True,
        "created": created_file,
        "inserted": inserted,
    }


__all__ = ["write_daily_block", "REGION_ID"]
