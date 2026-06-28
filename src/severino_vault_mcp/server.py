"""MCP server registration.

Most tools read the vault from disk. Two are vault writers (add_frontmatter,
update_frontmatter) that mutate vault `.md` files in place -- both validate
against the schema and refuse unsafe operations. doc_id is immutable across
all writes. A small jseverino.com operations group wraps fixed Wrangler/curl
workflows for the operator's own site; it is deliberately not a generic shell.

The Quick Index is also exposed as an MCP resource so clients can discover
the vault's navigation hub without spending a search-tool call.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from mcp.server.fastmcp import FastMCP

from . import (
    daily_notes,
    task_service,
    vault_query_service,
    vault_search_service,
    vault_write_service,
)
from .config import Config
from .context import ServerContext
from .search import best_section, tokenize
from .secret_unlock import (
    SecretUnlockResult,
    audit_secret_unlock,
    load_unlock_hash,
    prompt_unlock_phrase,
    verify_unlock_phrase,
)
from .sections import resolve_section
from .sensitivity import Sensitivity, advisory, body_is_releasable
from .tabular import is_separator as _is_table_separator
from .tabular import split_row as _split_table_row
from .tools import infra_datasets as infra_datasets_tools
from .tools import site_ops as site_ops_tools
from .tools import topology as topology_tools
from .tools import writeups as writeups_tools
from .vault import Doc, _normalize_alias
from .vault_query_service import doc_to_hit as _hit_to_dict

QUICK_INDEX_DOC_ID = "report-playbook-mcp-index"
QUICK_INDEX_RESOURCE_URI = "vault://quick-index"
DOC_RESOURCE_TEMPLATE_URI = "vault://doc/{doc_id}"


_CTX = ServerContext(Config.from_env())
# config + loader back the generic core tools still inlined here (find / read /
# search / tasks / daily). They collapse into _CTX when the core moves to the
# engine; the loader is always needed, so building it now costs nothing.
_CONFIG = _CTX.config
_LOADER = _CTX.loader


_SERVER_INSTRUCTIONS = """\
This MCP routes the calling AI session to the right runbook, infrastructure
doc, or project metadata in the configured Obsidian-style operational vault.
The vault is expected to contain stable frontmatter IDs, a Quick Index, and
runbooks or infrastructure notes for the operator's environment.

OPERATING RULES (read this BEFORE answering any operational question):

1. For broad or cross-cutting questions ("how do I X", "where do I look",
   "what's the process for..."), START with the Quick Index navigation hub:
   read the `vault://quick-index` resource if your MCP host supports
   resources, or call `read_doc('report-playbook-mcp-index')` if it does not.
   Then read the specific target doc before answering. Prefer
   `vault://doc/{doc_id}` for stable doc reads when your MCP host supports
   resource templates; otherwise call `read_doc(doc_id)`.

2. If the user asks "what's the runbook for Y" or anything more specific
   resembling operational knowledge from the configured vault, you MUST call
   `find_runbook` (or `lookup_system`, or `search_body`) BEFORE generating
   any prose. Then call `read_doc` on the top hit and answer in the doc's
   own words — quote commands verbatim.

3. Do NOT generate a generic tutorial from training data when a runbook
   exists in the vault. That is the single failure mode this MCP is built
   to prevent. If the user's answer is "run cert-gen <host>", that is the
   entire answer — no openssl tutorial, no preamble.

4. If `find_runbook` returns no relevant hits, say so explicitly and offer
   to create the missing runbook (Write the file, then `add_frontmatter` to
   register it). Only fall back to general guidance when you've confirmed
   no doc exists, and label it "no doc exists for this — here's a general
   approach."

5. Match the doc's terseness in your reply. If the runbook is four lines,
   your answer is four lines. Long answers when the doc is short are a
   smell that you didn't actually read it.

6. For personal progress/log questions ("what progress did I make on Friday?",
   "what happened yesterday?"), use `daily_progress` first. Daily notes are
   intentionally outside the runbook index and live under `00 Inbox/Daily Note`.

SENSITIVITY GATE (don't be afraid of it):

- public / internal / sensitive — `read_doc` returns the full body. Bodies
  marked `sensitive` come with an `advisory` field; pass it along to the
  user but use the content.
- restricted — `read_doc` withholds the body by default. Pass
  `include_restricted=True` only when the user explicitly needs it.
  The local MCP still requires `SVMC_ALLOW_RESTRICTED_UNLOCK=1` plus a
  successful hidden local unlock prompt before releasing the body. These are
  docs about CA keys, credential rotation, the offline CA, etc.

WRITE TOOLS:

`add_frontmatter` and `update_frontmatter` mutate vault files. Use them
when the user asks to tag a doc, bump `last_reviewed`, deprecate something,
etc. After successful writes, remind the user to run any downstream vault
sync or indexing job their workflow requires.

JSEVERINO.COM WRITEUP WORKFLOW (MANDATORY for portfolio work):

When working on writeups in `05 Writeups/` or the technology catalog at
`06 Pages/_technology-groups.md`, you MUST use the dedicated tools below
instead of grepping or reading the files by hand. The manual workflow is
how publishes ship with wrong `featured_order` values, missing tech-catalog
slugs, or dangling image references; these tools exist to prevent that.

IMPORTANT FOR LOCAL MODELS:
- Do not print fake tool-call text such as `list_writeups("published")`.
- Actually invoke the MCP tool, wait for the JSON result, then answer from it.
- For any "what is the order / currently published / featured / home order"
  question, call `list_featured_writeup_order()` first (fast path).

Each tool's own description is authoritative; pick by intent, not by grep:

- READ state — `list_featured_writeup_order` (compact order), `list_writeups`
  (full inventory; `filter="featured"` is the only correct home-cloud order),
  `get_technology_catalog` (slugs/groups), `find_writeups_using_tag` (is a tag
  earned a featured slot?). Never `cat`/`rg` `05 Writeups/*/index.md` or hand-
  parse `_technology-groups.md` for these.
- VALIDATE — `validate_writeup` / `validate_all_writeups`. "I read the file" is
  not "I validated it." `writeup_dashboard` returns inventory + validation from
  one snapshot; use it instead of separate calls for the same screen.
- MUTATE (never `Edit` writeup YAML) — `update_writeup_frontmatter` for one
  writeup's scalars; `reorder_featured` / `apply_writeup_plan` for the featured
  list, which they keep sequential 1..N transactionally. Hand-shuffling
  `featured_order` across files is the exact failure mode that produced v2.4.2.

VERIFY BEFORE SHIPPING: call `prepare_writeup_publish(slug)` (or at minimum
`validate_writeup(slug)` + `list_writeups("featured")`) *immediately before*
the commit/push of a writeup, not after. It catches wrong `featured_order`,
missing tech-catalog entries, missing images, and unresolved `related_projects`
/ `related_assets` before they reach production. Shipping first and verifying
second is exactly how this MCP came to exist; do not repeat that pattern. Apply
the same discipline as Rule 2 above about runbooks.

The configured vault root and indexed directories are controlled by
`config.example.toml`-style settings or `SVMC_*` environment overrides.
"""

mcp = FastMCP("severino-vault-mcp", instructions=_SERVER_INSTRUCTIONS)


# ----- helpers ----------------------------------------------------------------

# Markdown table parsing is single-sourced in tabular.py (the parse-side mirror
# of its one-renderer rule); _split_table_row / _is_table_separator are the
# imported split_row / is_separator, kept under these local names for the
# Quick Index reader below.


def _wiki_targets(text: str) -> list[str]:
    targets: list[str] = []
    for part in text.split("[[")[1:]:
        target = part.split("]]", 1)[0].split("|", 1)[0].split("#", 1)[0].strip()
        if target:
            targets.append(target)
    return targets


def _doc_for_reference(idx, reference: str):
    clean = reference.strip().strip("`")
    if not clean:
        return None
    if clean in idx.by_doc_id:
        return idx.by_doc_id[clean]

    targets = _wiki_targets(reference) or [clean]
    by_title = {doc.title.lower(): doc for doc in idx.docs}
    by_path_stem = {doc.path.stem.lower(): doc for doc in idx.docs}
    for target in targets:
        lowered = target.lower()
        if lowered in by_title:
            return by_title[lowered]
        if lowered in by_path_stem:
            return by_path_stem[lowered]
    return None


def _quick_index_matches(idx, query: str, *, limit: int = 3) -> list[dict[str, Any]]:
    """Return high-signal Quick Index table rows matching the query.

    This turns the Quick Index from an optional resource into a structured
    routing hint for models that call tools but do not reliably read resources.
    """
    quick_index_doc = idx.by_doc_id.get(QUICK_INDEX_DOC_ID)
    if quick_index_doc is None:
        return []

    query_tokens = tokenize(query)
    if not query_tokens:
        return []

    rows: list[dict[str, Any]] = []
    headers: list[str] | None = None
    for raw in quick_index_doc.body.splitlines():
        line = raw.strip()
        if not line.startswith("|") or not line.endswith("|"):
            headers = None
            continue

        cells = _split_table_row(line)
        if _is_table_separator(cells):
            continue
        if headers is None:
            headers = cells
            continue
        if len(cells) != len(headers):
            continue

        row = dict(zip(headers, cells, strict=True))
        intent = row.get("Intent") or row.get("Symptom") or row.get("Topic") or ""
        command = row.get("Command") or row.get("First step") or row.get("Start Here") or ""
        doc_ref = row.get("Doc") or row.get("Then Read") or ""

        intent_overlap = len(query_tokens & tokenize(intent))
        command_overlap = len(query_tokens & tokenize(command))
        doc_overlap = len(query_tokens & tokenize(doc_ref))
        score = 5 * intent_overlap + 3 * command_overlap + 2 * doc_overlap
        if score == 0:
            continue

        target_doc = _doc_for_reference(idx, doc_ref) or _doc_for_reference(idx, command)
        match: dict[str, Any] = {
            "score": score,
            "intent": intent,
            "command": command,
            "doc": doc_ref,
            "quick_index_doc_id": quick_index_doc.doc_id,
        }
        if target_doc is not None:
            match["target_doc_id"] = target_doc.doc_id
            match["target_title"] = target_doc.title
        rows.append(match)

    rows.sort(key=lambda item: (item["score"], item["intent"]), reverse=True)
    return rows[:limit]


def _add_quick_index_recommendation(
    response: dict[str, Any],
    idx,
    query: str,
    *,
    top_doc_id: str | None = None,
) -> None:
    matches = _quick_index_matches(idx, query)
    if not matches:
        return
    response["quick_index_matches"] = matches
    if top_doc_id and matches[0].get("target_doc_id") != top_doc_id:
        return
    response["recommended"] = {
        "source": QUICK_INDEX_RESOURCE_URI,
        **matches[0],
    }


def _withheld_secret_adjacent_response(base: dict[str, Any], result: str | None = None) -> dict[str, Any]:
    base["body_released"] = False
    base["advisory"] = advisory(Sensitivity.parse(str(base["sensitivity"])))
    if result:
        base["unlock"] = {
            "allowed": False,
            "result": result,
            "message": _SECRET_UNLOCK_MESSAGES[result],
        }
    return base


def _lookup_doc(identifier: str) -> tuple[Doc | None, dict[str, str] | None]:
    """Resolve a stable doc_id or a human-facing title/path alias."""
    idx = _LOADER.index()
    doc = idx.by_doc_id.get(identifier)
    if doc is not None:
        return doc, None

    alias = _normalize_alias(identifier)
    alias_doc_id = idx.aliases.get(alias)
    if alias_doc_id:
        return idx.by_doc_id[alias_doc_id], {
            "input": identifier,
            "matched_alias": alias,
            "target_doc_id": alias_doc_id,
        }

    needle = _normalize_alias(identifier)
    if not needle:
        return None, None

    for candidate in idx.docs:
        aliases = {
            candidate.doc_id,
            candidate.title,
            candidate.relative_path,
            Path(candidate.relative_path).stem,
        }
        normalized_aliases = {_normalize_alias(alias) for alias in aliases}
        if needle in normalized_aliases:
            return candidate, {
                "input": identifier,
                "matched_alias": candidate.title,
                "target_doc_id": candidate.doc_id,
                "source": "title_or_path",
            }
    return None, None


def _not_found_response(identifier: str) -> dict[str, Any]:
    duplicates = _LOADER.index().duplicate_doc_ids.get(identifier)
    if duplicates:
        return {
            "doc_id": identifier,
            "found": False,
            "ambiguous": True,
            "error": f"duplicate doc_id {identifier!r}",
            "paths": duplicates,
            "guidance": (
                "Resolve the duplicate frontmatter IDs before reading this "
                "document. Run `severino-vault-mcp doctor` for the full report."
            ),
        }
    return {
        "doc_id": identifier,
        "found": False,
        "guidance": (
            "`read_doc` works best with a stable `doc_id`. If you only have a "
            "human title or phrase, call `find_runbook`, `lookup_system`, or "
            "`search_body` first, then retry `read_doc` with the returned "
            "`doc_id`."
        ),
        "suggested_tools": ["find_runbook", "lookup_system", "search_body"],
        "alias_hint": (
            "For recurring local phrases, add an entry to the vault aliases "
            "file configured by `SVMC_ALIASES_PATH` or `[aliases].path`."
        ),
    }


_SECRET_UNLOCK_MESSAGES = {
    "not_requested": (
        "Body withheld. To request release, rerun with include_restricted=True; "
        "the local MCP will still require an interactive unlock on the Mac."
    ),
    "disabled": (
        "Interactive unlock is disabled. Set SVMC_ALLOW_RESTRICTED_UNLOCK=1 "
        "in the local MCP environment to allow local unlock prompts."
    ),
    "no_unlock_hash": (
        "No local unlock hash is configured. Store a salted sha256 unlock hash "
        "in Keychain, SVMC_RESTRICTED_UNLOCK_HASH_FILE, or "
        "SVMC_RESTRICTED_UNLOCK_HASH."
    ),
    "prompt_unavailable": "Local hidden-input prompt was unavailable or cancelled.",
    "failed": "Local unlock phrase verification failed.",
}


def _secret_adjacent_unlock(doc_id: str, title: str) -> SecretUnlockResult:
    if not _CONFIG.allow_secret_adjacent_unlock:
        return SecretUnlockResult(False, "disabled", _SECRET_UNLOCK_MESSAGES["disabled"])

    unlock_hash = load_unlock_hash(
        env_hash=_CONFIG.secret_unlock_hash,
        hash_file=_CONFIG.secret_unlock_hash_file,
        keychain_service=_CONFIG.secret_unlock_keychain_service,
        keychain_account=_CONFIG.secret_unlock_keychain_account,
    )
    if not unlock_hash:
        return SecretUnlockResult(False, "no_unlock_hash", _SECRET_UNLOCK_MESSAGES["no_unlock_hash"])

    phrase = prompt_unlock_phrase(doc_id, title)
    if phrase is None:
        return SecretUnlockResult(
            False,
            "prompt_unavailable",
            _SECRET_UNLOCK_MESSAGES["prompt_unavailable"],
        )

    if not verify_unlock_phrase(phrase, unlock_hash):
        return SecretUnlockResult(False, "failed", _SECRET_UNLOCK_MESSAGES["failed"])

    return SecretUnlockResult(True, "released", "Local unlock succeeded for this request only.")


# ----- resources --------------------------------------------------------------

def _render_doc_resource(doc_id: str) -> str:
    """Return a markdown resource view of one vault doc."""
    idx = _LOADER.index()
    doc = idx.by_doc_id.get(doc_id)
    if doc is None:
        duplicates = idx.duplicate_doc_ids.get(doc_id)
        if duplicates:
            paths = "\n".join(f"- `{path}`" for path in duplicates)
            return (
                "# Duplicate Vault Doc ID\n\n"
                f"`{doc_id}` is used by multiple indexed documents:\n\n"
                f"{paths}\n\n"
                "Resolve the duplicate IDs and rerun the vault doctor."
            )
        return (
            "# Vault Doc Not Found\n\n"
            f"No indexed vault doc exists with doc_id `{doc_id}`.\n\n"
            "Use `find_runbook`, `lookup_system`, or `search_body` to locate "
            "the right document."
        )
    if not body_is_releasable(doc.sensitivity, include_secret_adjacent=False):
        return (
            f"# {doc.title}\n\n"
            f"{advisory(doc.sensitivity)}\n\n"
            f"- doc_id: `{doc.doc_id}`\n"
            f"- path: `{doc.relative_path}`\n"
            f"- system: `{doc.system}`\n"
            f"- sensitivity: `{doc.sensitivity.value}`\n"
        )
    return doc.body

@mcp.resource(
    QUICK_INDEX_RESOURCE_URI,
    name="quick-index",
    title="Vault Quick Index",
    description=(
        "Navigation hub for broad operational questions. Read this first for "
        "cross-cutting 'how do I' or 'where do I look' questions, then read "
        "the target runbook or infrastructure doc it points to."
    ),
    mime_type="text/markdown",
)
def quick_index() -> str:
    """Return the Quick Index markdown body as a discoverable MCP resource."""
    return _render_doc_resource(QUICK_INDEX_DOC_ID)


@mcp.resource(
    DOC_RESOURCE_TEMPLATE_URI,
    name="vault-doc",
    title="Vault Doc by doc_id",
    description=(
        "Stable resource template for reading an indexed vault doc by doc_id. "
        "Returns markdown body when releasable, or an advisory plus metadata "
        "when the doc is restricted."
    ),
    mime_type="text/markdown",
)
def vault_doc(doc_id: str) -> str:
    """Return one indexed vault doc as a resource template."""
    return _render_doc_resource(doc_id)


# ----- tools ------------------------------------------------------------------

# The section-menu computation is single-sourced in vault_search_service so the
# CLI renders the identical payload (emit-once, render-many). Aliased here for
# the get_runbook hit list below.
_section_menu = vault_search_service.section_menu


@mcp.tool()
def find_runbook(query: str, limit: int = 5) -> dict[str, Any]:
    """USE THIS FIRST when the user asks an operational question covered by the vault.

    Searches vault docs by title / system / tags / doc_id and returns ranked
    hits. The intended pattern is:

        find_runbook("user's question")
        → pick the top hit
        → read_doc(hit["doc_id"])
        → answer with the doc's actual content (quote commands verbatim)

    Do NOT generate a generic tutorial when a runbook exists in the vault.
    If hits look weak, also try `search_body` for full-text matches inside
    doc bodies.

    Args:
        query: Natural-language query, e.g. "renew the internal TLS cert".
        limit: Maximum hits to return (default 5).
    """
    response = vault_search_service.find_sections(_LOADER, query, limit=limit)
    top_doc_id = response["hits"][0]["doc_id"] if response["hits"] else None
    _add_quick_index_recommendation(response, _LOADER.index(), query, top_doc_id=top_doc_id)
    return response


@mcp.tool()
def get_runbook(query: str, limit: int = 5) -> dict[str, Any]:
    """Search for the best runbook and return its body in one tool call.

    This is the safest path for smaller/local models because it removes the
    multi-step `find_runbook` → copy `doc_id` → `read_doc` failure mode. It
    returns the same ranked hits as `find_runbook`, plus a `selected` document
    and the selected document body when the sensitivity gate allows release.

    Args:
        query: Natural-language operational question, e.g. "ssh into the VPS".
        limit: Maximum ranked hits to include for context (default 5).
    """
    # Reuse the canonical hits payload (emit-once) instead of restating the
    # menu shape; get_runbook only *adds* the resolved `selected` body on top.
    idx = _LOADER.index()
    response = vault_search_service.find_sections(_LOADER, query, limit=limit)
    hits = response["hits"]
    top_doc_id = hits[0]["doc_id"] if hits else None
    _add_quick_index_recommendation(response, idx, query, top_doc_id=top_doc_id)
    if not hits:
        response["found"] = False
        return response

    doc = idx.by_doc_id[top_doc_id]
    selected: dict[str, Any] = {"score": hits[0]["score"], **_hit_to_dict(doc)}
    if doc.sensitivity is Sensitivity.SECRET_ADJACENT:
        response["found"] = True
        response["selected"] = _withheld_secret_adjacent_response(selected, "not_requested")
        return response

    # Token win: return the matched *section* when one actually scored against
    # the query; fall back to the whole body when the match was metadata-only,
    # so a tag-only hit never silently drops the part that holds the answer.
    sec, sec_score = best_section(doc, query)
    if sec is not None and sec_score > 0:
        selected.update(_section_menu(doc, query))
        selected["body"] = sec.body
        selected["body_scope"] = "section"
        selected["full_body_available"] = True
    else:
        selected["body"] = doc.body
        selected["body_scope"] = "doc"
    selected["body_released"] = True
    if doc.sensitivity is Sensitivity.SENSITIVE:
        selected["advisory"] = advisory(doc.sensitivity)
    response["found"] = True
    response["selected"] = selected
    return response


@mcp.tool()
def lookup_system(name: str) -> dict[str, Any]:
    """Return every doc whose `system:` field matches a name (case-insensitive substring).

    Useful for "tell me everything about AdGuard Home" / "show me Tailscale docs".

    Args:
        name: System / service name to look up.
    """
    needle = name.strip().lower()
    if not needle:
        return {"system_query": name, "matches": []}
    idx = _LOADER.index()
    matches = [
        d
        for d in idx.docs
        if needle in d.system.lower()
        or needle in d.title.lower()
        or needle in d.doc_id.lower()
    ]
    matches.sort(key=lambda d: (d.status != "active", d.last_reviewed or "", d.title))
    return {
        "system_query": name,
        "match_count": len(matches),
        "matches": [_hit_to_dict(d) for d in matches],
    }


def _narrow_to_section(base: dict[str, Any], doc: Doc, section: str) -> dict[str, Any]:
    """Replace `base['body']` with one section span (mutates and returns base).

    Only called once the gate has already released the body, so this never
    widens access. An unknown section withholds the body and lists the
    available slugs so the caller can retry — it does not fall back to the
    whole doc.
    """
    sec = resolve_section(doc.sections, section)
    if sec is None:
        base.pop("body", None)
        base["body_released"] = False
        base["section_error"] = f"no section {section!r} in {doc.doc_id}"
        base["available_sections"] = [
            {"section": s.slug, "heading_path": s.heading_path} for s in doc.sections
        ]
        return base
    base["body"] = sec.body
    base["heading"] = sec.heading or doc.title
    base["section"] = sec.slug
    base["heading_path"] = sec.heading_path
    base["body_scope"] = "section"
    return base


@mcp.tool()
def read_doc(
    doc_id: str,
    section: str | None = None,
    include_restricted: bool = False,
    include_secret_adjacent: bool = False,
) -> dict[str, Any]:
    """Read the full markdown body of a vault doc. Call this after find_runbook.

    The body comes back for `public`, `internal`, AND `sensitive` docs — the
    MCP runs locally under the operator's user account. Use the content.
    `sensitive` docs include an `advisory` field you should pass along to
    the user but the body is yours to quote.

    `restricted` docs (Local PKI, Offline CA, age workflow, Wazuh
    credential rotation) withhold the body by default. If the user explicitly
    needs one, pass `include_restricted=True`; the local MCP still
    requires `SVMC_ALLOW_RESTRICTED_UNLOCK=1`, a configured unlock hash,
    and a successful hidden local unlock prompt on the Mac.

    Pass `section` to get back just one H2-scoped span instead of the whole
    body — the token-minimal path. The value is a section slug from a
    `find_runbook` hit (its `section` field) or the `heading_path` string.
    Omit it and the full body comes back exactly as before.

    Args:
        doc_id: Stable identifier from the doc's frontmatter, e.g. "rb-add-nginx-proxy-host".
            Exact doc titles and vault-relative filenames are also accepted as
            a fallback for smaller local models, but stable `doc_id` values are
            preferred.
        section: Optional section slug or heading path. When set, the response
            body is just that section; an unknown value returns the list of
            available section slugs instead of the body.
        include_restricted: If True, request the body of `restricted` docs.
            Default False. This is only a request; local unlock policy must
            still approve the release.
        include_secret_adjacent: Backwards-compatible alias for
            `include_restricted`.
    """
    doc, resolved_from_alias = _lookup_doc(doc_id)
    if doc is None:
        return _not_found_response(doc_id)

    base: dict[str, Any] = {"doc_id": doc.doc_id, "found": True, **_hit_to_dict(doc)}
    if resolved_from_alias:
        base["resolved_from_alias"] = resolved_from_alias

    requested_restricted = include_restricted or include_secret_adjacent
    if doc.sensitivity is Sensitivity.SECRET_ADJACENT:
        if not requested_restricted:
            return _withheld_secret_adjacent_response(base, "not_requested")

        unlock = _secret_adjacent_unlock(doc.doc_id, doc.title)
        audit_secret_unlock(
            _CONFIG.secret_unlock_audit_log,
            doc_id=doc.doc_id,
            result=unlock.result,
        )
        base["unlock"] = unlock.to_dict()
        if unlock.allowed:
            base["body"] = doc.body
            base["body_released"] = True
            base["override_used"] = True
            base["advisory"] = advisory(doc.sensitivity, override_used=True)
            return _narrow_to_section(base, doc, section) if section else base
        return _withheld_secret_adjacent_response(base, unlock.result)

    if body_is_releasable(doc.sensitivity, include_secret_adjacent=requested_restricted):
        base["body"] = doc.body
        base["body_released"] = True
        if doc.sensitivity is Sensitivity.SENSITIVE:
            base["advisory"] = advisory(doc.sensitivity)
        if section:
            _narrow_to_section(base, doc, section)
    else:
        base["body_released"] = False
        base["advisory"] = advisory(doc.sensitivity)
    return base


@mcp.tool()
def inventory_for_project(project_slug: str) -> dict[str, Any]:
    """List vault docs that name `project_slug` in their `related_projects` frontmatter.

    Returns the docs grouped by `doc_type`. This is the "what do I have for
    project X" question based on vault frontmatter.

    Args:
        project_slug: Project slug (e.g. "client-edge-dns") — must match the
            entry in a doc's `related_projects:` list.
    """
    slug = project_slug.strip()
    if not slug:
        return {"project_slug": project_slug, "by_doc_type": {}}
    idx = _LOADER.index()
    matches = [d for d in idx.docs if slug in d.related_projects]

    by_type: dict[str, list[dict]] = {}
    for d in matches:
        by_type.setdefault(d.doc_type, []).append(_hit_to_dict(d))
    return {
        "project_slug": project_slug,
        "match_count": len(matches),
        "by_doc_type": by_type,
    }


@mcp.tool()
def recent_changes(days: int = 7, limit: int = 50) -> dict[str, Any]:
    """Recent vault commits within the indexed folders.

    Reads `git log` in the vault working tree. Returns commit metadata only —
    no diffs, no doc bodies. Useful for "what did I change in the last week?"

    Args:
        days: Look-back window in days (default 7).
        limit: Max commits to return (default 50).
    """
    return vault_query_service.recent_changes(_LOADER, days, limit)


@mcp.tool()
def daily_progress(query: str, today: str | None = None) -> dict[str, Any]:
    """Read a daily note for progress/log questions like "what did I do Friday?".

    Daily notes live outside the durable runbook index under the configured
    daily-notes folder, defaulting to `00 Inbox/Daily Note`. This tool resolves
    small natural-language date references (`today`, `yesterday`, `Friday`,
    `last Friday`, `YYYY-MM-DD`, `MM/DD/YYYY`) to a concrete daily note and
    returns the body plus extracted progress lines for summarization.

    Args:
        query: The user's natural-language progress question.
        today: Optional ISO date used as the anchor for relative terms. Omit in
            normal use; tests and deterministic clients can set it.
    """
    return daily_notes.daily_progress(_LOADER, query, today=today)


@mcp.tool()
def search_body(
    query: str,
    limit: int = 10,
    context_lines: int = 1,
    case_sensitive: bool = False,
) -> dict[str, Any]:
    """Full-text search across vault doc bodies using ripgrep.

    Searches the content of every indexed `.md`, skipping matches that fall
    inside frontmatter blocks. Results are grouped by `doc_id`. Honors the
    sensitivity gate the same way `read_doc` does: `sensitive` docs are
    included with an advisory; `restricted` docs are always excluded. Restricted
    bodies are never searched here — per-doc local unlock is only available
    through `read_doc(..., include_restricted=True)`.

    Args:
        query: Literal string or regex (ripgrep regex syntax).
        limit: Max number of distinct documents to return (default 10).
        context_lines: Lines of context above + below each match (default 1).
        case_sensitive: If True, match case. Default False.
    """
    return vault_query_service.search_body(
        _LOADER,
        query,
        limit=limit,
        context_lines=context_lines,
        case_sensitive=case_sensitive,
    )


@mcp.tool()
def describe_commands() -> dict[str, Any]:
    """Return this server's own CLI command surface as structured data.

    The "command leg" of emit-once, render-many: the same JSON the `describe`
    console subcommand prints, generated by walking the argparse parser so it
    can't drift from `--help`. Use it to learn what the `severino-vault-mcp`
    shell CLI exposes (find / read / schema / hq-manifest / the writeup and
    drift-guard subcommands) before reaching for Bash — one structured call
    instead of reading the scripts or prose.
    """
    from .cli import build_parser
    from .cli_introspect import describe_parser

    # cordon's emitter returns the full {ok, schema_version, ...} document.
    return describe_parser(build_parser())


# ----- composed Labs tool groups --------------------------------------------
site_ops_tools.register(mcp, _CTX)
writeups_tools.register(mcp, _CTX)
topology_tools.register(mcp, _CTX)
infra_datasets_tools.register(mcp, _CTX)


# ----- generic vault write tools ---------------------------------------------


@mcp.tool()
def add_frontmatter(
    relative_path: str,
    doc_id: str,
    title: str,
    doc_type: str,
    system: str,
    environment: str = "other",
    status: str = "active",
    sensitivity: str = "internal",
    tags: list[str] | None = None,
    related_projects: list[str] | None = None,
    related_assets: list[str] | None = None,
    last_reviewed: str | None = None,
) -> dict[str, Any]:
    """Prepend YAML frontmatter to a vault doc that doesn't have any.

    Refuses if the file already starts with `---` — frontmatter edits go
    through Obsidian, not this tool. Validates every enum field against the
    schema before writing.

    After success, the file is reloaded into the vault cache. If the operator
    syncs vault metadata into another system, remind them to run that sync.

    Args:
        relative_path: Path relative to the vault root, e.g.
            "01 Projects/tools/index.md".
        doc_id: Stable identifier. Must start with one of:
            rb-* (runbook), infra-* (infrastructure), report-* (report),
            project-* (project index), note-* (dated note within a project).
        title: Free text, usually matches the H1.
        doc_type: One of runbook | architecture_note | deployment_guide |
            troubleshooting_guide | recovery_procedure | public_article_draft |
            decision_record.
        system: Free text — the system / service the doc is about.
        environment: One of homelab | vps | wordpress | cloudflare |
            tailscale | adguard | unifi | local_mac | other. Default "other".
        status: One of draft | active | deprecated | archived. Default "active".
        sensitivity: One of public | internal | sensitive | restricted.
            Default "internal" — pick conservatively for credential/key-adjacent docs.
        tags: Optional kebab-case tag list.
        related_projects: Optional project slugs.
        related_assets: Optional asset slugs.
        last_reviewed: Optional ISO date (YYYY-MM-DD). Defaults to today.
    """
    return vault_write_service.add_frontmatter(
        _LOADER,
        relative_path,
        doc_id,
        title,
        doc_type,
        system,
        environment=environment,
        status=status,
        sensitivity=sensitivity,
        tags=tags,
        related_projects=related_projects,
        related_assets=related_assets,
        last_reviewed=last_reviewed,
        profile=_CTX.profile,
    )


@mcp.tool()
def update_frontmatter(
    relative_path: str,
    touch_last_reviewed: bool = False,
    last_reviewed: str | None = None,
    title: str | None = None,
    doc_type: str | None = None,
    system: str | None = None,
    environment: str | None = None,
    status: str | None = None,
    sensitivity: str | None = None,
    set_tags: list[str] | None = None,
    add_tags: list[str] | None = None,
    remove_tags: list[str] | None = None,
    set_related_projects: list[str] | None = None,
    add_related_projects: list[str] | None = None,
    remove_related_projects: list[str] | None = None,
    set_related_assets: list[str] | None = None,
    add_related_assets: list[str] | None = None,
    remove_related_assets: list[str] | None = None,
) -> dict[str, Any]:
    """Update fields in an existing frontmatter block. doc_id is immutable.

    Every parameter is optional — None means "leave that field alone." For
    list fields, you may either replace wholesale (`set_*`), append (`add_*`),
    or strip (`remove_*`); `set_*` wins over add/remove if both are given.

    Refuses if the file has no frontmatter — use `add_frontmatter` for those.

    Args:
        relative_path: Path relative to the vault root, e.g.
            "03 Runbooks/Add Nginx Proxy Host.md".
        touch_last_reviewed: If True, sets `last_reviewed` to today's date
            (overrides the `last_reviewed` argument). Use this when you've
            just re-read or revised the doc.
        last_reviewed: Explicit ISO date (YYYY-MM-DD). Ignored if
            `touch_last_reviewed` is True.
        title, doc_type, system, environment, status, sensitivity: scalar
            updates. Pass None to leave unchanged. Enum-valued fields are
            validated against the schema.
        set_tags / add_tags / remove_tags: list ops for `tags`.
        set_related_projects / add_related_projects / remove_related_projects:
            list ops for `related_projects` (project slugs).
        set_related_assets / add_related_assets / remove_related_assets:
            list ops for `related_assets` (asset slugs).
    """
    return vault_write_service.update_frontmatter(
        _LOADER,
        relative_path,
        touch_last_reviewed=touch_last_reviewed,
        last_reviewed=last_reviewed,
        title=title,
        doc_type=doc_type,
        system=system,
        environment=environment,
        status=status,
        sensitivity=sensitivity,
        set_tags=set_tags,
        add_tags=add_tags,
        remove_tags=remove_tags,
        set_related_projects=set_related_projects,
        add_related_projects=add_related_projects,
        remove_related_projects=remove_related_projects,
        set_related_assets=set_related_assets,
        add_related_assets=add_related_assets,
        remove_related_assets=remove_related_assets,
        profile=_CTX.profile,
    )


@mcp.tool()
def task_board(
    status: str | None = None,
    project: str | None = None,
    stale_only: bool = False,
    include_all: bool = False,
    stale_days: int = 14,
) -> dict[str, Any]:
    """The backlog: every open task across the whole fleet, in one view.

    Tasks are `doc_type: task` vault docs — project tasks under
    `01 Projects/<project>/tasks/`, cross-cutting ones in `07 Backlog/`. This
    derives the board from the index (the one task brain); call it at the start
    of a session to see what's open, and `set_task_status` to move work.

    Args:
        status: Only this status (open | active | parked | done | wontfix).
        project: Only this project (its folder name, or a related_projects link).
        stale_only: Only stale tasks — open/active, untouched past the window.
        include_all: Include parked/done/wontfix (default shows open + active).
        stale_days: Stale window in days (default 14).
    """
    return task_service.list_tasks(
        _LOADER,
        status=status,
        project=project,
        stale_only=stale_only,
        include_all=include_all,
        stale_days=stale_days,
    )


@mcp.tool()
def task_projects() -> dict[str, Any]:
    """List the projects a task can be filed in, with each one's open-task count.

    The colocation universe: every `01 Projects/<project>/` folder. Use it to pick
    a `project` for `add_task`, or to see which projects carry the most open work.
    """
    return task_service.list_projects(_LOADER)


@mcp.tool()
def reconcile_tasks() -> dict[str, Any]:
    """Re-home tasks into tasks/ (live) or tasks/done/ (closed) per their status.

    Idempotent tidy: catches statuses edited by hand (a Base/Properties edit) that
    didn't move through set_task_status. Returns how many were re-filed.
    """
    return task_service.reconcile_tasks(_LOADER)


@mcp.tool()
def add_task(
    title: str,
    project: str | None = None,
    related_projects: list[str] | None = None,
    effort: str = "S",
    priority: str = "med",
    tags: list[str] | None = None,
) -> dict[str, Any]:
    """Capture a new task as a vault file.

    With `project`, the task is colocated at `01 Projects/<project>/tasks/` and
    linked to that project; without it, the task is filed in the cross-cutting
    `07 Backlog/` bucket (pass `related_projects` to record what it touches).

    Args:
        title: Imperative task title, e.g. "Fix the bats PATH trap".
        project: Owning project — must be an existing `01 Projects/<project>/`.
        related_projects: Projects a cross-cutting task touches (slugs).
        effort: S | M | L. Default S.
        priority: high | med | low. Default med.
        tags: Optional kebab-case tags. Default ["backlog"].
    """
    return task_service.add_task(
        _LOADER,
        title=title,
        project=project,
        related_projects=related_projects,
        effort=effort,
        priority=priority,
        tags=tags,
    )


@mcp.tool()
def promote_note(
    source: str,
    title: str,
    project: str | None = None,
    effort: str = "S",
    priority: str = "med",
) -> dict[str, Any]:
    """Promote a captured note (an inbox capture) into a task, preserving its body.

    Reads the note's body, creates a task (colocated in `project` or the bucket),
    and deletes the source — the capture → task half of the inbox loop.

    Args:
        source: Vault-relative path to the note.
        title: Task title.
        project: Owning project (an `01 Projects/<project>/` folder).
        effort: S | M | L.
        priority: high | med | low.
    """
    return task_service.promote_note(
        _LOADER, source, title=title, project=project, effort=effort, priority=priority
    )


@mcp.tool()
def set_task_status(doc_id: str, status: str) -> dict[str, Any]:
    """Move a task to a new status; stamp `closed:` on done, clear it on reopen.

    Done tasks are kept (not deleted) so "what shipped this month" stays a query.
    Resolves either a bare slug (`backlog-cli`) or the full id (`task-backlog-cli`).

    Args:
        doc_id: Task id or slug.
        status: Target status (open | active | parked | done | wontfix).
    """
    return task_service.set_task_status(_LOADER, doc_id, status)


@mcp.tool()
def delete_task(doc_id: str) -> dict[str, Any]:
    """Permanently delete a task file — for mistakes and junk only.

    Finished or abandoned *real* work should be `set_task_status` to
    `done`/`wontfix` instead (kept and queryable). This removes the file; it is
    git-recoverable only if it was committed. Resolves a bare slug or the full id.

    Args:
        doc_id: Task id or slug.
    """
    return task_service.delete_task(_LOADER, doc_id)


# ----- entry point ------------------------------------------------------------

def run() -> None:
    """Start the MCP server over stdio. Invoked by `__main__.py` and the console script."""
    mcp.run()


__all__ = ["mcp", "run"]
