"""Writeup loader for the jseverino.com portfolio surface.

Writeups live under `<vault>/05 Writeups/<slug>/index.md` and use a different
frontmatter shape than the doc_id-keyed runbooks/infrastructure notes the
main vault loader indexes. This module handles their schema separately so
the existing `Doc` loader stays focused on operational docs.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

from .frontmatter import split_frontmatter

IMAGE_REF_PATTERN = re.compile(r"!\[[^\]]*\]\(([^)]+)\)")


@dataclass
class Writeup:
    slug: str
    title: str
    description: str
    published: bool
    published_at: str | None
    last_reviewed: str | None
    cover_image: str | None
    cover_alt: str | None
    technologies: list[str]
    featured: bool
    featured_order: int | None
    related_projects: list[str]
    related_assets: list[str]
    path: Path
    body: str

    def to_summary(self) -> dict:
        return {
            "slug": self.slug,
            "title": self.title,
            "description": self.description,
            "published": self.published,
            "published_at": self.published_at,
            "last_reviewed": self.last_reviewed,
            "cover_image": self.cover_image,
            "cover_alt": self.cover_alt,
            "featured": self.featured,
            "featured_order": self.featured_order,
            "technologies": list(self.technologies),
            "related_projects": list(self.related_projects),
            "related_assets": list(self.related_assets),
        }


def _coerce_bool(value, default: bool = False) -> bool:
    if isinstance(value, bool):
        return value
    if value is None:
        return default
    text = str(value).strip().lower()
    if text in {"true", "yes", "1", "on"}:
        return True
    if text in {"false", "no", "0", "off"}:
        return False
    return default


def _coerce_int_or_none(value) -> int | None:
    if value is None or value == "" or value == []:
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _coerce_str_list(value) -> list[str]:
    if value is None or value == "":
        return []
    if isinstance(value, list):
        return [str(v) for v in value if v not in (None, "")]
    return [str(value)]


def _coerce_optional_str(value) -> str | None:
    if value is None or value == "" or value == []:
        return None
    return str(value)


def load_writeups(writeups_root: Path) -> list[Writeup]:
    """Walk `<vault>/05 Writeups/` and load every writeup with frontmatter."""
    out: list[Writeup] = []
    if not writeups_root.is_dir():
        return out
    for slug_dir in sorted(writeups_root.iterdir()):
        if not slug_dir.is_dir():
            continue
        if slug_dir.name.startswith("."):
            continue
        index = slug_dir / "index.md"
        if not index.is_file():
            continue
        try:
            text = index.read_text(encoding="utf-8")
        except OSError:
            continue
        fm, body, _ = split_frontmatter(text)
        if not fm:
            continue
        out.append(
            Writeup(
                slug=slug_dir.name,
                title=str(fm.get("title") or slug_dir.name),
                description=str(fm.get("description") or ""),
                published=_coerce_bool(fm.get("published"), default=False),
                published_at=_coerce_optional_str(fm.get("published_at")),
                last_reviewed=_coerce_optional_str(fm.get("last_reviewed")),
                cover_image=_coerce_optional_str(fm.get("cover_image")),
                cover_alt=_coerce_optional_str(fm.get("cover_alt")),
                technologies=_coerce_str_list(fm.get("technologies")),
                featured=_coerce_bool(fm.get("featured"), default=False),
                featured_order=_coerce_int_or_none(fm.get("featured_order")),
                related_projects=_coerce_str_list(fm.get("related_projects")),
                related_assets=_coerce_str_list(fm.get("related_assets")),
                path=index,
                body=body,
            )
        )
    return out


def extract_body_image_refs(body: str) -> list[str]:
    """Return relative image paths referenced from a writeup body."""
    refs: list[str] = []
    for match in IMAGE_REF_PATTERN.finditer(body):
        ref = match.group(1).strip()
        if not ref:
            continue
        if ref.startswith(("http://", "https://", "data:")):
            continue
        if ref.startswith("./"):
            ref = ref[2:]
        refs.append(ref)
    return refs
