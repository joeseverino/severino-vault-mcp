"""Generated marked-region writes inside a vault note (the "mirror" mechanic).

A generated block is delimited by HTML-comment markers, so a re-run rewrites
only that span and never touches the human-authored text around it — the
anti-clobber guarantee the drift-guard doc tables already rely on. This module
owns only the *mechanic* (find/replace/insert a span); it never owns a path.
Schema-specific writers (``daily_write``) name their own region and file shape
and call in here, so the idempotent-region logic lives in one place rather than
being re-copied per feature.

``replace_region`` is shared with ``infra_datasets`` (one implementation, two
marker namespaces: ``INFRA-DATA`` for generated tables, ``MIRROR`` here).
"""

from __future__ import annotations


def replace_region(text: str, begin: str, end: str, content: str) -> str | None:
    """Rewrite the span between the ``begin`` and ``end`` markers.

    Returns the new text, or ``None`` when the markers are absent (so callers
    can fall back to inserting the region). Everything before ``begin`` and from
    ``end`` onward is preserved byte-for-byte.
    """
    bi, ei = text.find(begin), text.find(end)
    if bi == -1 or ei == -1 or ei < bi:
        return None
    return f"{text[:bi + len(begin)]}\n{content}\n{text[ei:]}"


def markers(region_id: str) -> tuple[str, str]:
    """The begin/end comment markers for a named mirror region."""
    return (
        f"<!-- MIRROR:BEGIN {region_id} (generated — do not edit) -->",
        f"<!-- MIRROR:END {region_id} -->",
    )


def _frontmatter_end(text: str) -> int:
    """Char index just past a leading frontmatter block's closing fence, else 0.

    Used to place a first-run region right after the frontmatter without
    re-serializing it — the frontmatter is preserved byte-for-byte.
    """
    if not text.lstrip().startswith("---"):
        return 0
    lines = text.splitlines(keepends=True)
    seen_open = False
    for index, line in enumerate(lines):
        if line.strip() == "---":
            if seen_open:
                return sum(len(piece) for piece in lines[: index + 1])
            seen_open = True
    return 0


def upsert_region(text: str, region_id: str, content: str) -> tuple[str, bool]:
    """Replace ``region_id``'s region with ``content``; insert it (after the
    frontmatter, else at the top) on first run.

    Returns ``(new_text, inserted)`` — ``inserted`` is True only when the region
    did not exist yet. The free-authored text outside the region is preserved.
    """
    content = content.rstrip("\n")
    begin, end = markers(region_id)
    replaced = replace_region(text, begin, end, content)
    if replaced is not None:
        return replaced, False
    block = f"{begin}\n{content}\n{end}\n"
    cut = _frontmatter_end(text)
    if cut == 0:
        return f"{block}\n{text.lstrip(chr(10))}" if text else block, True
    head, rest = text[:cut], text[cut:].lstrip("\n")
    return f"{head}\n{block}\n{rest}" if rest else f"{head}\n{block}", True
