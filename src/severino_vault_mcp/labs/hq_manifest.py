"""Build the Severino HQ manifest from the configured vault."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from vault_engine.frontmatter import read_frontmatter

HQ_KEYS = {
    "doc_id",
    "title",
    "doc_type",
    "system",
    "environment",
    "status",
    "sensitivity",
    "obsidian_path",
    "github_path",
    "external_url",
    "last_reviewed",
    "notes",
    "related_projects",
    "related_assets",
    "published_at",
    "content_type",
    "tags",
    "slug",
    "description",
    "excerpt",
    "published",
    "technologies",
    "topic",
}
SKIP_DIR_NAMES = {
    ".git",
    ".obsidian",
    "00 Templates",
    "Templates",
    "source",
}
SLIM_WRITEUP_DIR = "05 Writeups"
SLIM_PAGE_DIR = "06 Pages"


def _synthesize_slim_entry(
    frontmatter: dict[str, Any],
    relative_path: Path,
    *,
    kind: str,
) -> dict[str, Any]:
    slug = relative_path.parts[1]
    published = bool(frontmatter.get("published"))
    if kind == "writeup":
        doc_id = frontmatter.get("doc_id") or f"writeup-{slug}"
        content_type = "portfolio_article"
        external_url = f"https://jseverino.com/portfolio/{slug}/"
    else:
        doc_id = frontmatter.get("doc_id") or f"page-{slug}"
        content_type = "page"
        page_path = frontmatter.get("path") or f"/{slug}/"
        external_url = f"https://jseverino.com{page_path}"

    entry: dict[str, Any] = {
        "doc_id": doc_id,
        "title": frontmatter.get("title") or slug,
        "doc_type": "public_article_draft",
        "system": "jseverino.com",
        "environment": "cloudflare",
        "status": "active" if published else "draft",
        "sensitivity": "public" if published else "internal",
        "content_type": content_type,
        "slug": slug,
        "published": published,
    }
    if frontmatter.get("description") or frontmatter.get("excerpt"):
        entry["topic"] = (
            frontmatter.get("description") or frontmatter.get("excerpt")
        )
    if frontmatter.get("tags") or frontmatter.get("technologies"):
        entry["tags"] = (
            frontmatter.get("tags") or frontmatter.get("technologies")
        )
    if published:
        entry["external_url"] = external_url
    for key in (
        "published_at",
        "last_reviewed",
        "related_projects",
        "related_assets",
    ):
        if frontmatter.get(key):
            entry[key] = frontmatter[key]
    return entry


def _should_skip(path: Path) -> bool:
    return any(part in SKIP_DIR_NAMES for part in path.parts)


def build_hq_manifest(
    vault: Path,
    subdirs: tuple[str, ...] | list[str],
) -> dict[str, Any]:
    """Return a manifest plus warnings, failing on duplicate IDs."""
    if not vault.is_dir():
        return {"ok": False, "error": f"vault root not found: {vault}"}

    entries: list[dict[str, Any]] = []
    seen_ids: dict[str, Path] = {}
    missing_frontmatter: list[str] = []
    missing_dirs: list[str] = []
    duplicates: list[dict[str, str]] = []

    for subdir in subdirs:
        root = vault / subdir
        if not root.is_dir():
            missing_dirs.append(subdir)
            continue
        for path in sorted(root.rglob("*.md")):
            if _should_skip(path) or path.name.startswith("_"):
                continue
            frontmatter = read_frontmatter(path)
            relative_path = path.relative_to(vault)
            if frontmatter is None:
                missing_frontmatter.append(str(relative_path))
                continue
            top = relative_path.parts[0] if relative_path.parts else ""
            if top == SLIM_WRITEUP_DIR and path.name == "index.md":
                entry = _synthesize_slim_entry(
                    frontmatter,
                    relative_path,
                    kind="writeup",
                )
            elif top == SLIM_PAGE_DIR and path.name == "index.md":
                entry = _synthesize_slim_entry(
                    frontmatter,
                    relative_path,
                    kind="page",
                )
            elif not frontmatter.get("doc_id"):
                missing_frontmatter.append(str(relative_path))
                continue
            else:
                entry = {
                    key: frontmatter[key]
                    for key in frontmatter
                    if key in HQ_KEYS
                }
            entry["path"] = str(relative_path)
            doc_id = str(entry["doc_id"])
            previous = seen_ids.get(doc_id)
            if previous is not None:
                duplicates.append(
                    {
                        "doc_id": doc_id,
                        "first": str(previous.relative_to(vault)),
                        "second": str(relative_path),
                    }
                )
            else:
                seen_ids[doc_id] = path
            entries.append(entry)

    if duplicates:
        return {
            "ok": False,
            "error": "duplicate doc_id values prevent manifest generation",
            "duplicates": duplicates,
            "missing_frontmatter": missing_frontmatter,
            "missing_dirs": missing_dirs,
        }
    return {
        "ok": True,
        "entries": entries,
        "count": len(entries),
        "missing_frontmatter": missing_frontmatter,
        "missing_dirs": missing_dirs,
    }


__all__ = ["build_hq_manifest"]
