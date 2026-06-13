"""Section chunking — the unit of retrieval for federated, token-minimal reads.

P1 of `docs/federated-retrieval.md`: parse a doc body into a list of `Section`
spans so search can score and `read_doc` can return *one section* instead of a
whole body. Decisions locked in that doc:

- **Granularity**: split at H2 only; H3+ stay inside their parent H2. A section
  over `DEFAULT_TOKEN_CAP` tokens is sub-split at its H3 boundaries, and any
  still-oversized piece is hard-wrapped by lines so one giant section can't blow
  the token budget.
- **Addressing**: each section gets a heading `slug`, unique within the doc
  (collisions disambiguated `-2`, `-3`, … in document order). The full
  `heading_path` is accepted as an alias by :func:`resolve_section`.

FastMCP-free, like every other service module — `vault.py` calls
:func:`parse_sections` while building the index; `search.py` scores the spans.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

# ~4 chars/token is a good-enough estimate at this scale; we never tokenize for
# a model here, only decide when a section is big enough to sub-split.
DEFAULT_TOKEN_CAP = 400

_FENCE_RE = re.compile(r"^\s*(```|~~~)")
# ATX heading: 1-6 leading '#', a space, text, optional trailing '#'s.
_ATX_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")


@dataclass(frozen=True)
class Section:
    heading: str        # H2 text, or "" for the pre-H2 preamble
    slug: str           # addressable, unique within the doc
    heading_path: str   # e.g. "Routine operations > Backing commands"
    level: int          # heading level that opened it (2; 3 when sub-split; 0 preamble)
    body: str           # section text, including its heading line
    start_line: int     # 1-indexed line in the source file


@dataclass
class _Block:
    heading: str
    level: int
    heading_path: str
    lines: list[str]
    start_line: int


def _estimate_tokens(text: str) -> int:
    return max(1, len(text) // 4)


def _slugify(text: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")


class _Slugger:
    """Hands out doc-unique slugs; the 2nd 'overview' becomes 'overview-2'."""

    def __init__(self) -> None:
        self._counts: dict[str, int] = {}

    def unique(self, base: str) -> str:
        base = base or "overview"
        n = self._counts.get(base, 0) + 1
        self._counts[base] = n
        return base if n == 1 else f"{base}-{n}"


def _heading_lines(lines: list[str]) -> dict[int, tuple[int, str]]:
    """Map line index -> (level, text) for real ATX headings (fences skipped)."""
    out: dict[int, tuple[int, str]] = {}
    in_fence = False
    fence = ""
    for i, line in enumerate(lines):
        m = _FENCE_RE.match(line)
        if m:
            tok = m.group(1)
            if not in_fence:
                in_fence, fence = True, tok
            elif tok == fence:
                in_fence, fence = False, ""
            continue
        if in_fence:
            continue
        hm = _ATX_RE.match(line)
        if hm:
            out[i] = (len(hm.group(1)), hm.group(2).strip())
    return out


def _hardwrap(block: _Block, token_cap: int) -> list[_Block]:
    """Last-resort split of an oversized, H3-less block into line chunks."""
    chunks: list[list[str]] = []
    cur: list[str] = []
    cur_tok = 0
    for line in block.lines:
        lt = _estimate_tokens(line) + 1
        if cur and cur_tok + lt > token_cap:
            chunks.append(cur)
            cur, cur_tok = [], 0
        cur.append(line)
        cur_tok += lt
    if cur:
        chunks.append(cur)
    if len(chunks) <= 1:
        return [block]

    out: list[_Block] = []
    cursor = 0
    for n, chunk in enumerate(chunks, 1):
        path = f"{block.heading_path} (part {n})" if block.heading_path else f"(part {n})"
        out.append(_Block(block.heading, block.level, path, chunk, block.start_line + cursor))
        cursor += len(chunk)
    return out


def _subsplit(block: _Block, token_cap: int) -> list[_Block]:
    """Split an over-cap H2 block at its H3 boundaries, then hard-wrap remnants."""
    if block.level == 0 or _estimate_tokens("\n".join(block.lines)) <= token_cap:
        return [block]

    headings = _heading_lines(block.lines)
    h3 = [i for i, (lvl, _) in sorted(headings.items()) if lvl == 3]
    if not h3:
        return _hardwrap(block, token_cap)

    parts: list[_Block] = []
    lead = block.lines[: h3[0]]
    if any(line.strip() for line in lead):
        parts.append(_Block(block.heading, 2, block.heading_path, lead, block.start_line))
    for n, start in enumerate(h3):
        end = h3[n + 1] if n + 1 < len(h3) else len(block.lines)
        text = headings[start][1]
        parts.append(
            _Block(text, 3, f"{block.heading} > {text}", block.lines[start:end],
                   block.start_line + start)
        )

    out: list[_Block] = []
    for part in parts:
        if _estimate_tokens("\n".join(part.lines)) > token_cap:
            out.extend(_hardwrap(part, token_cap))
        else:
            out.append(part)
    return out


def parse_sections(
    body: str, body_start_line: int = 1, *, token_cap: int = DEFAULT_TOKEN_CAP
) -> list[Section]:
    """Chunk a markdown body into addressable :class:`Section` spans."""
    lines = body.split("\n")
    headings = _heading_lines(lines)
    h2 = [i for i, (lvl, _) in sorted(headings.items()) if lvl == 2]

    blocks: list[_Block] = []
    first = h2[0] if h2 else len(lines)
    if any(line.strip() for line in lines[:first]):
        blocks.append(_Block("", 0, "", lines[:first], body_start_line))
    for n, start in enumerate(h2):
        end = h2[n + 1] if n + 1 < len(h2) else len(lines)
        heading = headings[start][1]
        blocks.append(
            _Block(heading, 2, heading, lines[start:end], body_start_line + start)
        )

    slugger = _Slugger()
    sections: list[Section] = []
    for block in blocks:
        for sub in _subsplit(block, token_cap):
            sections.append(
                Section(
                    heading=sub.heading,
                    slug=slugger.unique(_slugify(sub.heading_path)),
                    heading_path=sub.heading_path,
                    level=sub.level,
                    body="\n".join(sub.lines),
                    start_line=sub.start_line,
                )
            )
    return sections


def resolve_section(sections: list[Section], ref: str):
    """Find a section by slug, then by heading_path / heading (case-insensitive)."""
    needle = (ref or "").strip().lower()
    if not needle:
        return None
    for sec in sections:
        if needle == sec.slug.lower():
            return sec
    nslug = _slugify(needle)
    for sec in sections:
        if needle in (sec.heading_path.lower(), sec.heading.lower()) or nslug == sec.slug:
            return sec
    return None


def section_summary(section: Section, *, limit: int = 120) -> str:
    """One-line menu summary: the section's first prose sentence, capped."""
    lines = section.body.split("\n")
    start = 1 if section.heading else 0
    buf: list[str] = []
    for line in lines[start:]:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or _FENCE_RE.match(line):
            if buf:
                break
            continue
        buf.append(stripped)
        if any(p in stripped for p in ".!?"):
            break
    text = " ".join(buf)
    match = re.search(r"(.+?[.!?])(\s|$)", text)
    sentence = match.group(1) if match else text
    if len(sentence) > limit:
        sentence = sentence[: limit - 1].rstrip() + "…"
    return sentence
