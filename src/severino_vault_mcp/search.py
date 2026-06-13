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

# Query-only stopwords. Stripped from the *query* before scoring so filler words
# ("a", "the", "into") in a natural-language intent ("Edit a `.age` file in
# place") don't manufacture matches against unrelated docs. Never applied to doc
# tokens — a doc keeps every word it has.
_QUERY_STOPWORDS = frozenset(
    {
        "a", "an", "the", "of", "to", "in", "on", "for", "and", "or", "with",
        "my", "is", "it", "do", "i", "into", "as", "at", "by", "from", "this",
        "that",
    }
)

# How many distinct body-only query terms can contribute, and at what weight.
# Capped so a long doc that happens to contain several query words can't drown
# out a short doc whose title/tags are a direct hit.
_BODY_MATCH_WEIGHT = 1
_BODY_MATCH_CAP = 3


def tokenize(text: str) -> set[str]:
    return {m.group(0).lower() for m in _TOKEN_RE.finditer(text or "")}


def query_tokens(query: str) -> set[str]:
    """Tokenize a search query and drop filler stopwords."""
    return tokenize(query) - _QUERY_STOPWORDS


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
    """Weighted bag-of-words score over metadata, with a capped body signal.

    Tags / system / title overlaps weigh most; doc_id / environment less.
    A query term found only in the body (not in any metadata field) adds a
    small, capped amount — enough to surface a runbook that documents a
    command in its prose ("encrypt a file", "test resolver latency") without
    letting body length dominate a direct metadata hit. Active-status bonus
    nudges current docs above deprecated/archived peers.
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

    metadata_toks = tag_toks | system_toks | title_toks | id_toks | env_toks
    body_only = (query_tokens & tokenize(doc.body)) - metadata_toks
    s += _BODY_MATCH_WEIGHT * min(len(body_only), _BODY_MATCH_CAP)

    if s and doc.status == "active":
        s += 1
    return s


def rank(docs: list[Doc], query: str, *, limit: int = 5) -> list[Hit]:
    qtoks = query_tokens(query)
    hits = [Hit(d, score(d, qtoks)) for d in docs]
    hits = [h for h in hits if h.score > 0]
    # Stable secondary sort: more recently-reviewed first.
    hits.sort(
        key=lambda h: (h.score, h.doc.last_reviewed or "", h.doc.title),
        reverse=True,
    )
    return hits[:limit]
