"""The education dataset: the governed projection build consumers read.

The vault's publishable education facts, projected once: institutions
(top-level indexed folders whose ``index.md`` carries ``institution`` +
``slug``) and their courses (``doc_type: course`` docs beneath them), each
with row facts from frontmatter and public bullets from the body's
``## Site`` section.

jseverino.com's sync-content and resume-engine's coursework reconciler both
read this projection, through ``severino-vault-mcp export education`` or the
``education_dataset`` tool, so what a course is stays defined next to the
schema profile that governs the vault. Courses are exported whatever their status, sorted
by term then code; each consumer applies its own surface policy (the site
shows active + completed courses that have bullets, the resume coursework
line lists completed courses only).
"""

from __future__ import annotations

import re
from typing import Any

from vault_engine.context import GovernanceContext
from vault_engine.vault import VaultLoader

_TERM_SEASONS = {"spring": 0, "summer": 1, "fall": 2}
_HEADING = re.compile(r"^#{1,6} ")


def _term_key(term: str) -> tuple[int, int]:
    """Chronological sort key for a ``<Season> <Year>`` term; unknown shapes last."""
    season, _, year = term.strip().partition(" ")
    try:
        return (int(year), _TERM_SEASONS[season.lower()])
    except (KeyError, ValueError):
        return (9999, 9)


def _site_section(body: str) -> str:
    """The ``## Site`` section body: a course's public bullets, verbatim."""
    lines = body.split("\n")
    try:
        start = next(index for index, line in enumerate(lines) if line.strip() == "## Site")
    except StopIteration:
        return ""
    rest = lines[start + 1 :]
    end = next((index for index, line in enumerate(rest) if _HEADING.match(line)), len(rest))
    return "\n".join(rest[:end]).strip()


def education_dataset(loader: VaultLoader, statuses: frozenset[str]) -> dict[str, Any]:
    """Institutions with their courses, validated against the education profile.

    Fails closed: a course with a status outside the profile enum, or missing
    ``code`` / ``term``, or an institution note without ``slug``, makes the
    whole export ``ok: false``, so consumers never build from drifted facts.
    """
    docs = loader.index().docs
    errors: list[str] = []
    institutions: list[dict[str, Any]] = []

    for doc in sorted(docs, key=lambda entry: entry.relative_path):
        if doc.doc_type != "resource":
            continue
        folder, _, leaf = doc.relative_path.partition("/")
        if leaf != "index.md":
            continue
        if "institution" not in doc.extra:
            continue
        if not doc.extra.get("slug"):
            errors.append(f"{doc.relative_path}: institution note missing `slug`")
            continue

        courses: list[dict[str, Any]] = []
        for course in docs:
            if course.doc_type != "course" or not course.relative_path.startswith(folder + "/"):
                continue
            missing = [key for key in ("code", "term") if not course.extra.get(key)]
            if missing:
                errors.append(f"{course.relative_path}: course missing `{'`, `'.join(missing)}`")
                continue
            if course.status not in statuses:
                errors.append(
                    f"{course.relative_path}: status `{course.status}` is not in the "
                    "education profile"
                )
                continue
            courses.append(
                {
                    "doc_id": course.doc_id,
                    "code": str(course.extra["code"]),
                    "title": course.title,
                    "short_title": course.extra.get("short_title"),
                    "term": str(course.extra["term"]),
                    "status": course.status,
                    "site_bullets": _site_section(course.body),
                }
            )
        courses.sort(key=lambda course: (_term_key(course["term"]), course["code"]))

        institutions.append(
            {
                "doc_id": doc.doc_id,
                "institution": str(doc.extra["institution"]),
                "slug": str(doc.extra["slug"]),
                "description": doc.extra.get("description"),
                "courses": courses,
            }
        )

    if errors:
        return {"ok": False, "errors": errors}
    return {"ok": True, "institutions": institutions}


def register(mcp, ctx: GovernanceContext) -> None:
    """Register the edu vault's own tool."""
    @mcp.tool(name="education_dataset")
    def education_dataset_tool() -> dict[str, Any]:
        """The edu vault's publishable dataset: institutions and their courses
        (row facts from frontmatter, public bullets from each course's `## Site`
        section). The same JSON `severino-vault-mcp export education` emits for
        the site and the resume."""
        return education_dataset(ctx.loader, ctx.profile.statuses)


__all__ = ["education_dataset", "register"]
