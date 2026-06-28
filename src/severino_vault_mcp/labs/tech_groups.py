"""Parser for the jseverino.com technology-groups catalog markdown.

The catalog at `<vault>/06 Pages/_technology-groups.md` is the single
source of truth for site technology slugs. Each `##` section is a group;
each section contains a markdown table with `Slug | Label | Featured`
columns. The `Featured` cell is `yes` or empty.

The file is prefixed with `_` so the main vault loader skips it. This
module reads it directly.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from ..tabular import is_separator, split_row


@dataclass
class TechSlug:
    slug: str
    label: str
    group: str
    featured: bool


def load_technology_catalog(path: Path) -> list[TechSlug]:
    """Parse the technology-groups markdown into a flat list of slugs."""
    if not path.is_file():
        return []
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return []

    slugs: list[TechSlug] = []
    current_group: str | None = None
    saw_header = False

    for raw in text.splitlines():
        stripped = raw.strip()

        if stripped.startswith("## "):
            current_group = stripped[3:].strip()
            saw_header = False
            continue

        if not current_group:
            continue

        if not (stripped.startswith("|") and stripped.endswith("|")):
            saw_header = False
            continue

        cells = split_row(stripped)

        if is_separator(cells):
            continue

        if not saw_header:
            saw_header = True
            continue

        if len(cells) < 3:
            continue

        slug = cells[0]
        label = cells[1]
        featured_cell = cells[2].lower()
        if not slug:
            continue

        slugs.append(
            TechSlug(
                slug=slug,
                label=label,
                group=current_group,
                featured=featured_cell == "yes",
            )
        )

    return slugs
