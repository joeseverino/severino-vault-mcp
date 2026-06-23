"""Tasks — the federated backlog, owned by the vault's one brain.

A task is just another vault doc (``doc_type: task``): the lenient index already
discovers every one under ``01 Projects/<project>/tasks/`` and ``07 Backlog/``,
so this module does not re-scan — it *derives* the board from the index and
*enriches* each task with the profile fields the general ``Doc`` does not carry
(effort/priority/created/closed), read on demand.

Writes go through the shared :func:`serialize_frontmatter` + atomic writer, the
single path every vault-doc write already uses, validated against the task
profile in :mod:`schema`. Everything that renders a backlog — the ``backlog``
CLI, the Obsidian cockpit, ``brief`` — derives from these three functions and
re-authors nothing. Emit once, derive everywhere.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
from typing import Any

from . import schema
from .atomic_write import atomic_write_text
from .frontmatter import (
    read_frontmatter,
    serialize_frontmatter,
    split_frontmatter,
)
from .vault import VaultLoader

PROJECTS_DIR = "01 Projects"
BACKLOG_DIR = "07 Backlog"
CROSS = "cross"

_EFFORTS = {"S", "M", "L"}
_PRIORITIES = {"high", "med", "low"}
# Lifecycle + priority order for a stable board.
_STATUS_ORDER = {"active": 0, "open": 1, "parked": 2, "done": 3, "wontfix": 4}
_PRIORITY_ORDER = {"high": 0, "med": 1, "low": 2, "": 3}
_LIVE = {"open", "active"}


def _project_of(relative_path: str) -> str:
    """The owning project — derived from location, the source of truth.

    A task under ``01 Projects/<project>/`` belongs to ``<project>``; anything
    else (the ``07 Backlog/`` bucket) is cross-cutting.
    """
    parts = Path(relative_path).parts
    if len(parts) >= 2 and parts[0] == PROJECTS_DIR:
        return parts[1]
    return CROSS


def _task_record(loader: VaultLoader, doc, stale_days: int) -> dict[str, Any]:
    """One board row: index facts + the profile fields read on demand."""
    fm = read_frontmatter(doc.path) or {}
    age_days = _age_days(doc.path)
    status = doc.status or "open"
    return {
        "doc_id": doc.doc_id,
        "slug": doc.doc_id.removeprefix("task-"),
        "title": doc.title,
        "status": status,
        "project": _project_of(doc.relative_path),
        "related_projects": list(doc.related_projects),
        "effort": str(fm.get("effort") or ""),
        "priority": str(fm.get("priority") or ""),
        "created": str(fm.get("created") or ""),
        "closed": str(fm.get("closed") or ""),
        "tags": list(doc.tags),
        "relative_path": doc.relative_path,
        "age_days": age_days,
        "stale": status in _LIVE and age_days > stale_days,
    }


def _age_days(path: Path) -> int:
    try:
        import time
        return int((time.time() - path.stat().st_mtime) // 86400)
    except OSError:
        return 0


def _sort_key(task: dict[str, Any]):
    return (
        _STATUS_ORDER.get(task["status"], 99),
        _PRIORITY_ORDER.get(task["priority"], 99),
        task["created"] or "9999",
        task["doc_id"],
    )


def list_tasks(
    loader: VaultLoader,
    *,
    status: str | None = None,
    project: str | None = None,
    stale_only: bool = False,
    include_all: bool = False,
    stale_days: int = 14,
) -> dict[str, Any]:
    """The board: every task, derived from the index, filtered + ranked.

    ``counts`` is always over the *whole* ledger (the badge: how much is open /
    stale), while ``tasks`` honors the filters. Default visibility is live work
    only (open + active); ``include_all`` reveals parked/done/wontfix, an explicit
    ``status`` overrides, and ``stale_only`` narrows to the review nudge.
    """
    index = loader.index()
    all_tasks = [
        _task_record(loader, doc, stale_days)
        for doc in index.by_doc_id.values()
        if doc.doc_type == "task"
    ]

    counts: dict[str, Any] = {"status": {}, "project": {}, "stale": 0}
    for task in all_tasks:
        counts["status"][task["status"]] = counts["status"].get(task["status"], 0) + 1
        counts["project"][task["project"]] = counts["project"].get(task["project"], 0) + 1
        if task["stale"]:
            counts["stale"] += 1

    def visible(task: dict[str, Any]) -> bool:
        if status is not None:
            if task["status"] != status:
                return False
        elif not include_all and task["status"] not in _LIVE:
            return False
        if stale_only and not task["stale"]:
            return False
        if project is not None and task["project"] != project \
                and project not in task["related_projects"]:
            return False
        return True

    tasks = sorted((t for t in all_tasks if visible(t)), key=_sort_key)
    return {
        "ok": True,
        "stale_days": stale_days,
        "count": len(tasks),
        "total": len(all_tasks),
        "counts": counts,
        "tasks": tasks,
    }


def _slugify(title: str) -> str:
    out = []
    for ch in title.lower():
        out.append(ch if ch.isalnum() else "-")
    slug = "".join(out)
    while "--" in slug:
        slug = slug.replace("--", "-")
    return slug.strip("-")[:60].strip("-")


def add_task(
    loader: VaultLoader,
    *,
    title: str,
    project: str | None = None,
    related_projects: list[str] | None = None,
    effort: str = "S",
    priority: str = "med",
    tags: list[str] | None = None,
) -> dict[str, Any]:
    """Author a new task file — in its project's tasks/ folder, or the bucket.

    A ``project`` colocates the task at ``01 Projects/<project>/tasks/`` and
    becomes its sole ``related_projects`` link; omitting it files a cross-cutting
    task in ``07 Backlog/`` (pass ``related_projects`` to record what it touches).
    """
    title = title.strip()
    if not title:
        return {"ok": False, "error": "title is required"}
    if effort not in _EFFORTS:
        return {"ok": False, "error": f"effort {effort!r} not in {sorted(_EFFORTS)}"}
    if priority not in _PRIORITIES:
        return {"ok": False, "error": f"priority {priority!r} not in {sorted(_PRIORITIES)}"}

    slug = _slugify(title)
    if not slug:
        return {"ok": False, "error": "title produced an empty slug"}
    doc_id = f"task-{slug}"

    vault = loader.config.vault_path
    if project:
        project_dir = vault / PROJECTS_DIR / project
        if not project_dir.is_dir():
            return {
                "ok": False,
                "error": f"no such project: {project!r} (expected {PROJECTS_DIR}/{project}/)",
            }
        target_dir = project_dir / "tasks"
        related = related_projects or [project]
        if project not in related:
            related = [project, *related]
    else:
        target_dir = vault / BACKLOG_DIR
        related = related_projects or []

    index = loader.index()
    if doc_id in index.by_doc_id:
        return {
            "ok": False,
            "error": f"doc_id {doc_id!r} already exists at "
            f"{index.by_doc_id[doc_id].relative_path}",
        }
    file_path = target_dir / f"{doc_id}.md"
    if file_path.exists():
        return {"ok": False, "error": f"file already exists: {file_path.name}"}

    frontmatter = {
        "doc_id": doc_id,
        "title": title,
        "doc_type": "task",
        "status": "open",
        "related_projects": list(related),
        "effort": effort,
        "priority": priority,
        "created": date.today().isoformat(),
        "tags": tags or ["backlog"],
    }
    body = (
        f"# {title}\n\n"
        "**Problem.** \n\n"
        "**Fix.** \n\n"
        "**Principle.** \n\n"
        "**Source.** \n"
    )
    try:
        target_dir.mkdir(parents=True, exist_ok=True)
        atomic_write_text(file_path, serialize_frontmatter(frontmatter) + body)
    except OSError as exc:
        return {"ok": False, "error": f"write failed: {exc}"}
    loader.index(force=True)
    return {
        "ok": True,
        "doc_id": doc_id,
        "relative_path": str(file_path.relative_to(vault)),
        "project": project or CROSS,
        "status": "open",
    }


def set_task_status(
    loader: VaultLoader, doc_id: str, status: str
) -> dict[str, Any]:
    """Move a task to a new status; stamp ``closed:`` on done, clear it on reopen.

    Done tasks are kept (not deleted) so "what shipped" stays a query. Resolves a
    bare slug or the full ``task-`` id, surgically through the one serializer.
    """
    if status not in schema.TASK_STATUSES:
        return {
            "ok": False,
            "error": f"status {status!r} not in {sorted(schema.TASK_STATUSES)}",
        }
    index = loader.index()
    doc = index.by_doc_id.get(doc_id) or index.by_doc_id.get(f"task-{doc_id}")
    if doc is None:
        return {"ok": False, "error": f"no task matches: {doc_id!r}"}
    if doc.doc_type != "task":
        return {"ok": False, "error": f"{doc.doc_id!r} is not a task (doc_type {doc.doc_type})"}

    text = doc.path.read_text(encoding="utf-8")
    frontmatter, body, _ = split_frontmatter(text)
    if frontmatter is None:
        return {"ok": False, "error": "task file has no frontmatter"}

    previous = str(frontmatter.get("status") or "")
    frontmatter["status"] = status
    if status == "done":
        frontmatter["closed"] = date.today().isoformat()
    else:
        frontmatter.pop("closed", None)

    try:
        atomic_write_text(doc.path, serialize_frontmatter(frontmatter) + body)
    except OSError as exc:
        return {"ok": False, "error": f"write failed: {exc}"}
    loader.index(force=True)
    return {
        "ok": True,
        "doc_id": doc.doc_id,
        "relative_path": doc.relative_path,
        "status": status,
        "previous": previous,
    }
