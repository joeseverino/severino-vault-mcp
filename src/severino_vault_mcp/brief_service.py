"""Vault brief: the deterministic "state of the vault" aggregate.

One emit-once source for the doc-side vault facts an agent otherwise re-derives
every session — recent changes, docs overdue for review, and inbox backlog —
so the `brief` shell tool can compose them with repo and writeup state in a
single cheap read instead of inferring from raw files.

FastMCP-free (the service spine): the same code backs both the MCP and the CLI.
Writeup state keeps its own owner (`writeup_service` / `list-writeups`); this
stays doc-focused so each fact has exactly one owner.
"""

from __future__ import annotations

from datetime import date
from typing import Any

from .vault import VaultLoader
from .vault_query_service import recent_changes


def _age_days(iso: str | None) -> int | None:
    """Days since an ISO `last_reviewed` date, or None if absent/unparseable."""
    if not iso:
        return None
    try:
        year, month, day = (int(part) for part in iso[:10].split("-"))
        return (date.today() - date(year, month, day)).days
    except (ValueError, IndexError):
        return None


def vault_brief(
    loader: VaultLoader,
    *,
    days: int = 7,
    review_after_days: int = 180,
    recent_limit: int = 15,
) -> dict[str, Any]:
    """Doc-side vault state in one structured payload.

    - `recent_changes`: vault commits in the indexed dirs over the last `days`.
    - `docs_to_review`: indexed docs whose `last_reviewed` is older than
      `review_after_days`, newest-stale first.
    - `inbox`: count of top-level `00 Inbox/*.md` captures.
    """
    review_after_days = max(0, int(review_after_days))
    idx = loader.index()

    changes = recent_changes(loader, days, recent_limit)
    commits = changes.get("commits", []) if isinstance(changes, dict) else []
    changes_error = changes.get("error") if isinstance(changes, dict) else None

    stale: list[dict[str, Any]] = []
    for doc in idx.docs:
        age = _age_days(doc.last_reviewed)
        if age is not None and age > review_after_days:
            stale.append(
                {
                    "doc_id": doc.doc_id,
                    "title": doc.title,
                    "obsidian_path": doc.relative_path,
                    "last_reviewed": doc.last_reviewed,
                    "age_days": age,
                }
            )
    stale.sort(key=lambda entry: entry["age_days"], reverse=True)

    inbox_dir = loader.config.vault_path / "00 Inbox"
    inbox_count = sum(1 for _ in inbox_dir.glob("*.md")) if inbox_dir.is_dir() else 0

    recent: dict[str, Any] = {"days": days, "count": len(commits), "commits": commits}
    if changes_error:
        recent["error"] = changes_error

    return {
        "ok": True,
        "vault_doc_count": len(idx.docs),
        "recent_changes": recent,
        "docs_to_review": {
            "after_days": review_after_days,
            "count": len(stale),
            "docs": stale,
        },
        "inbox": {"count": inbox_count},
    }
