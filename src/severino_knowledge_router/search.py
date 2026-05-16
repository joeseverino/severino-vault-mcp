"""Tiny keyword-search ranker.

No embeddings, no vector store — for ~50–200 well-tagged docs the bag-of-words
score over title + system + tags + doc_id is plenty. Add semantic search later
only if keyword retrieval starts to feel weak.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

from .vault import Doc

_TOKEN_RE = re.compile(r"[A-Za-z0-9]+")


def tokenize(text: str) -> set[str]:
    return {m.group(0).lower() for m in _TOKEN_RE.finditer(text or "")}


def doc_tokens(doc: Doc) -> set[str]:
    bits = " ".join([
        doc.doc_id,
        doc.title,
        doc.system,
        doc.doc_type,
        doc.environment,
        " ".join(doc.tags),
    ])
    return tokenize(bits)


@dataclass(frozen=True)
class Hit:
    doc: Doc
    score: int


def score(doc: Doc, query_tokens: set[str]) -> int:
    """Weighted bag-of-words score.

    Tags / system / title overlaps weigh more than incidental matches in doc_id.
    Active-status bonus nudges current docs above deprecated/archived peers.
    """
    if not query_tokens:
        return 0

    tag_toks = tokenize(" ".join(doc.tags))
    system_toks = tokenize(doc.system)
    title_toks = tokenize(doc.title)
    id_toks = tokenize(doc.doc_id.replace("-", " "))
    env_toks = tokenize(doc.environment)

    s = 0
    s += 5 * len(query_tokens & tag_toks)
    s += 3 * len(query_tokens & system_toks)
    s += 3 * len(query_tokens & title_toks)
    s += 1 * len(query_tokens & id_toks)
    s += 1 * len(query_tokens & env_toks)

    if s and doc.status == "active":
        s += 1
    return s


def rank(docs: list[Doc], query: str, *, limit: int = 5) -> list[Hit]:
    qtoks = tokenize(query)
    hits = [Hit(d, score(d, qtoks)) for d in docs]
    hits = [h for h in hits if h.score > 0]
    # Stable secondary sort: more recently-reviewed first.
    hits.sort(
        key=lambda h: (h.score, h.doc.last_reviewed or "", h.doc.title),
        reverse=True,
    )
    return hits[:limit]
