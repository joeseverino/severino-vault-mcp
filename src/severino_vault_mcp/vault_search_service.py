"""Section-scoped search/read, single-sourced for the MCP *and* the CLI.

This is the "emit-once, render-many" core (see the vault decision record
`report-emit-once-render-many` and `docs/federated-retrieval.md`): the menu
computation lives here, FastMCP-free, so `server.py` renders it to a model and
`__main__.py` renders the *same* payload to the shell. Never restate the menu
shape in either renderer — call :func:`find_sections` / :func:`read_section`.

- :func:`section_menu` — the two-tier menu line for one hit (heading, slug,
  one-line summary; never a body).
- :func:`find_sections` — ranked menu over the vault, the shape `find_runbook`
  returns minus the server-only Quick Index routing hint.
- :func:`read_section` — one section span (or whole body), honoring the
  sensitivity gate. Restricted bodies are withheld here with no interactive
  unlock — that one-shot local unlock is a `read_doc` (MCP) affordance, not a
  shell-pipe one, exactly as `search_body` already withholds restricted.
"""

from __future__ import annotations

from typing import Any

from .search import best_section, rank
from .sections import resolve_section, section_summary
from .sensitivity import Sensitivity, advisory, body_is_releasable
from .vault import Doc, VaultLoader
from .vault_query_service import doc_to_hit

# Match find_runbook's clamp so the CLI and MCP cap the menu identically.
_MAX_LIMIT = 25


def section_menu(doc: Doc, query: str) -> dict[str, Any]:
    """Additive two-tier menu line for a hit: which section best answers `query`.

    Returns the heading, addressable slug, and a one-line summary — never a body.
    Empty dict for a doc with no parsed sections, so callers can ``**``-merge it.
    """
    sec, sec_score = best_section(doc, query)
    if sec is None:
        return {}
    return {
        "heading": sec.heading or doc.title,
        "section": sec.slug,
        "heading_path": sec.heading_path,
        "section_summary": section_summary(sec),
        "section_score": sec_score,
    }


def find_sections(loader: VaultLoader, query: str, *, limit: int = 5) -> dict[str, Any]:
    """Ranked section menu over the indexed vault.

    The canonical payload — ``{query, indexed_doc_count, hits}`` — both the MCP
    `find_runbook` and the CLI `find` render. Each hit is the slim doc
    projection plus its best-matching section line; no bodies are returned.
    """
    idx = loader.index()
    hits = rank(idx.docs, query, limit=max(1, min(int(limit), _MAX_LIMIT)))
    return {
        "query": query,
        "indexed_doc_count": len(idx.docs),
        "hits": [
            {"score": h.score, **doc_to_hit(h.doc), **section_menu(h.doc, query)}
            for h in hits
        ],
    }


def read_section(
    loader: VaultLoader,
    doc_id: str,
    section: str | None = None,
) -> dict[str, Any]:
    """Read one section span (or the whole body) of a doc, honoring the gate.

    The shell-side counterpart to `read_doc`: same releasable bodies, but
    restricted docs are withheld with no interactive unlock (a pipe can't
    answer a hidden prompt). Resolves a doc by stable `doc_id` only — the CLI
    feeds back a `find` hit's `doc_id`, so the alias-fallback machinery in
    `read_doc` isn't needed here.
    """
    idx = loader.index()
    doc = idx.by_doc_id.get(doc_id)
    if doc is None:
        duplicates = idx.duplicate_doc_ids.get(doc_id)
        if duplicates:
            return {
                "ok": False,
                "doc_id": doc_id,
                "found": False,
                "ambiguous": True,
                "error": f"duplicate doc_id {doc_id!r}",
                "paths": duplicates,
            }
        return {
            "ok": False,
            "doc_id": doc_id,
            "found": False,
            "error": f"no indexed doc with doc_id {doc_id!r}",
        }

    base: dict[str, Any] = {
        "ok": True,
        "doc_id": doc.doc_id,
        "found": True,
        **doc_to_hit(doc),
    }

    if not body_is_releasable(doc.sensitivity):
        base["body_released"] = False
        base["advisory"] = advisory(doc.sensitivity)
        return base

    if section:
        sec = resolve_section(doc.sections, section)
        if sec is None:
            base["ok"] = False
            base["body_released"] = False
            base["section_error"] = f"no section {section!r} in {doc.doc_id}"
            base["available_sections"] = [
                {"section": s.slug, "heading_path": s.heading_path}
                for s in doc.sections
            ]
            return base
        base["body"] = sec.body
        base["heading"] = sec.heading or doc.title
        base["section"] = sec.slug
        base["heading_path"] = sec.heading_path
        base["body_scope"] = "section"
    else:
        base["body"] = doc.body
        base["body_scope"] = "doc"

    base["body_released"] = True
    if doc.sensitivity is Sensitivity.SENSITIVE:
        base["advisory"] = advisory(doc.sensitivity)
    return base


__all__ = ["section_menu", "find_sections", "read_section"]
