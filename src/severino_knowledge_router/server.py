"""
MCP server registration.

Seven tools. Five are read-only (find_runbook, lookup_system, read_doc,
inventory_for_project, recent_changes) and read the vault from disk; no
network calls. Two are vault writers (add_frontmatter, update_frontmatter)
that mutate vault `.md` files in place -- both validate against the schema
and refuse unsafe operations. doc_id is immutable across all writes.

The Quick Index is also exposed as an MCP resource so clients can discover
the vault's navigation hub without spending a search-tool call.
"""

from __future__ import annotations

import json
import shutil
import subprocess
from datetime import date
from typing import Any

from mcp.server.fastmcp import FastMCP

from .config import Config
from .search import rank
from .sensitivity import Sensitivity, advisory, body_is_releasable
from .vault import VaultLoader, _split_frontmatter

# Enum vocabularies — must match the Frontmatter Schema doc in the vault.
DOC_TYPES = {
    "runbook", "architecture_note", "deployment_guide",
    "troubleshooting_guide", "recovery_procedure",
    "public_article_draft", "decision_record",
}
ENVIRONMENTS = {
    "homelab", "vps", "wordpress", "cloudflare", "tailscale",
    "adguard", "unifi", "local_mac", "other",
}
STATUSES = {"draft", "active", "deprecated", "archived"}
SENSITIVITIES = {"public", "internal", "sensitive", "secret_adjacent"}
DOC_ID_PREFIXES = ("rb-", "infra-", "report-", "project-", "note-")
QUICK_INDEX_DOC_ID = "report-playbook-mcp-index"
QUICK_INDEX_RESOURCE_URI = "vault://quick-index"
DOC_RESOURCE_TEMPLATE_URI = "vault://doc/{doc_id}"


_CONFIG = Config.from_env()
_LOADER = VaultLoader(_CONFIG)

_SERVER_INSTRUCTIONS = """\
This MCP routes the calling AI session to the right runbook, infrastructure
doc, or project metadata in Joe Severino's private "Severino Labs" Obsidian
vault. Every operational topic on this homelab — TLS certificates, AdGuard
DNS, Nginx Proxy Manager, Docker, Tailscale, Cloudflare, WordPress, Severino
HQ, vault tooling — has its own runbook there.

OPERATING RULES (read this BEFORE answering any operational question):

1. For broad or cross-cutting questions ("how do I X", "where do I look",
   "what's the process for..."), START with the Quick Index navigation hub:
   read the `vault://quick-index` resource if your MCP host supports
   resources, or call `read_doc('report-playbook-mcp-index')` if it does not.
   Then read the specific target doc before answering. Prefer
   `vault://doc/{doc_id}` for stable doc reads when your MCP host supports
   resource templates; otherwise call `read_doc(doc_id)`.

2. If the user asks "what's the runbook for Y" or anything more specific
   resembling operational knowledge about Joe's stack, you MUST call
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

SENSITIVITY GATE (don't be afraid of it):

- public / internal / sensitive — `read_doc` returns the full body. Bodies
  marked `sensitive` come with an `advisory` field; pass it along to the
  user but use the content.
- secret_adjacent — `read_doc` withholds the body by default. Pass
  `include_secret_adjacent=True` to override if the user needs it. These
  are docs about CA keys, credential rotation, the offline CA, etc.

WRITE TOOLS:

`add_frontmatter` and `update_frontmatter` mutate vault files. Use them
when the user asks to tag a doc, bump `last_reviewed`, deprecate something,
etc. After successful writes, remind the user to run `hq sync` so Severino
HQ picks up the change.

The vault root is `/Users/josephseverino/Documents/Code/Severino Labs/`.
Indexed dirs are `01 Projects/`, `02 Infrastructure/`, `03 Runbooks/`.
"""

mcp = FastMCP("severino-knowledge-router", instructions=_SERVER_INSTRUCTIONS)


# ----- helpers ----------------------------------------------------------------

def _hit_to_dict(doc) -> dict[str, Any]:
    return {
        "doc_id": doc.doc_id,
        "title": doc.title,
        "doc_type": doc.doc_type,
        "system": doc.system,
        "environment": doc.environment,
        "status": doc.status,
        "sensitivity": doc.sensitivity.value,
        "obsidian_path": doc.relative_path,
        "tags": list(doc.tags),
        "last_reviewed": doc.last_reviewed,
    }


# ----- resources --------------------------------------------------------------

def _render_doc_resource(doc_id: str) -> str:
    """Return a markdown resource view of one vault doc."""
    idx = _LOADER.index()
    doc = idx.by_doc_id.get(doc_id)
    if doc is None:
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
    title="Severino Labs Quick Index",
    description=(
        "Navigation hub for broad homelab questions. Read this first for "
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
        "when the doc is secret-adjacent."
    ),
    mime_type="text/markdown",
)
def vault_doc(doc_id: str) -> str:
    """Return one indexed vault doc as a resource template."""
    return _render_doc_resource(doc_id)


# ----- tools ------------------------------------------------------------------

@mcp.tool()
def find_runbook(query: str, limit: int = 5) -> dict[str, Any]:
    """USE THIS FIRST when the user asks any operational question about Joe's homelab.

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
        query: Natural-language query, e.g. "renew the homelab tls cert".
        limit: Maximum hits to return (default 5).
    """
    idx = _LOADER.index()
    hits = rank(idx.docs, query, limit=max(1, min(limit, 25)))
    return {
        "query": query,
        "indexed_doc_count": len(idx.docs),
        "hits": [{"score": h.score, **_hit_to_dict(h.doc)} for h in hits],
    }


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
    matches = [d for d in idx.docs if needle in d.system.lower()]
    matches.sort(key=lambda d: (d.status != "active", d.last_reviewed or "", d.title))
    return {
        "system_query": name,
        "match_count": len(matches),
        "matches": [_hit_to_dict(d) for d in matches],
    }


@mcp.tool()
def read_doc(doc_id: str, include_secret_adjacent: bool = False) -> dict[str, Any]:
    """Read the full markdown body of a vault doc. Call this after find_runbook.

    The body comes back for `public`, `internal`, AND `sensitive` docs — the
    MCP runs locally on Joe's Mac, so there is no shared context to protect
    against. Use the content. `sensitive` docs include an `advisory` field
    you should pass along to the user but the body is yours to quote.

    `secret_adjacent` docs (Local PKI, Offline CA, age workflow, Wazuh
    credential rotation) withhold the body by default. If the user explicitly
    needs one, pass `include_secret_adjacent=True`.

    Args:
        doc_id: Stable identifier from the doc's frontmatter, e.g. "rb-add-nginx-proxy-host".
        include_secret_adjacent: If True, also return the body of `secret_adjacent`
            docs. Default False. Use only when the user asks for a doc
            adjacent to actual credentials/keys.
    """
    idx = _LOADER.index()
    doc = idx.by_doc_id.get(doc_id)
    if doc is None:
        return {"doc_id": doc_id, "found": False}

    base: dict[str, Any] = {"doc_id": doc.doc_id, "found": True, **_hit_to_dict(doc)}

    if body_is_releasable(doc.sensitivity, include_secret_adjacent=include_secret_adjacent):
        base["body"] = doc.body
        base["body_released"] = True
        if doc.sensitivity is Sensitivity.SECRET_ADJACENT:
            base["override_used"] = True
            base["advisory"] = advisory(doc.sensitivity, override_used=True)
        elif doc.sensitivity is Sensitivity.SENSITIVE:
            base["advisory"] = advisory(doc.sensitivity)
    else:
        base["body_released"] = False
        base["advisory"] = advisory(doc.sensitivity)
    return base


@mcp.tool()
def inventory_for_project(project_slug: str) -> dict[str, Any]:
    """List vault docs that name `project_slug` in their `related_projects` frontmatter.

    Returns the docs grouped by `doc_type`. This is the "what do I have for
    project X" question — extend with HQ asset/expense data in a future
    version when an authenticated API is in place.

    Args:
        project_slug: HQ Project slug (e.g. "homelab-dns") — must match the
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
    days = max(1, min(int(days), 365))
    limit = max(1, min(int(limit), 500))

    cwd = str(_LOADER.config.vault_path)
    try:
        proc = subprocess.run(
            [
                "git", "log",
                f"--since={days}.days.ago",
                f"-n{limit}",
                "--pretty=format:%H|%cI|%s",
                "--",
                *_LOADER.config.indexed_dirs,
            ],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"error": f"git log failed: {exc}"}

    if proc.returncode != 0:
        return {"error": proc.stderr.strip() or "git log failed"}

    commits = []
    for line in proc.stdout.splitlines():
        parts = line.split("|", 2)
        if len(parts) == 3:
            commits.append({"sha": parts[0], "committed_at": parts[1], "subject": parts[2]})

    return {
        "days": days,
        "commit_count": len(commits),
        "commits": commits,
    }


@mcp.tool()
def search_body(
    query: str,
    limit: int = 10,
    context_lines: int = 1,
    case_sensitive: bool = False,
    include_secret_adjacent: bool = False,
) -> dict[str, Any]:
    """Full-text search across vault doc bodies using ripgrep.

    Searches the content of every indexed `.md`, skipping matches that fall
    inside frontmatter blocks. Results are grouped by `doc_id`. Honors the
    sensitivity gate the same way `read_doc` does: `sensitive` docs are
    included with an advisory; `secret_adjacent` docs are excluded by
    default and require `include_secret_adjacent=True`.

    Args:
        query: Literal string or regex (ripgrep regex syntax).
        limit: Max number of distinct documents to return (default 10).
        context_lines: Lines of context above + below each match (default 1).
        case_sensitive: If True, match case. Default False.
        include_secret_adjacent: If True, also search inside `secret_adjacent`
            docs. Default False.
    """
    query = (query or "").strip()
    if not query:
        return {"query": query, "hits_by_doc": [], "match_count": 0}

    rg = shutil.which("rg")
    if not rg:
        return {
            "error": (
                "ripgrep (`rg`) not found on PATH. Install via `brew install ripgrep` "
                "or skip this tool."
            ),
        }

    idx = _LOADER.index()
    vault_root = _LOADER.config.vault_path.resolve()
    indexed_roots = [vault_root / sub for sub in _LOADER.config.indexed_dirs]
    indexed_roots = [r for r in indexed_roots if r.is_dir()]
    if not indexed_roots:
        return {"query": query, "hits_by_doc": [], "match_count": 0}

    cmd = [
        rg,
        "--json",
        "--type", "md",
        "--max-count", "10",   # per-file cap
        f"--context={max(0, min(int(context_lines), 5))}",
        "--no-ignore-vcs",
    ]
    if not case_sensitive:
        cmd.append("-i")
    cmd.append(query)
    cmd.extend(str(r) for r in indexed_roots)

    try:
        proc = subprocess.run(
            cmd, capture_output=True, text=True, timeout=15, check=False
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"error": f"ripgrep failed: {exc}"}

    if proc.returncode > 1:
        # rg returns 1 for "no matches" — that's fine. Anything else is bad.
        return {"error": proc.stderr.strip() or f"ripgrep returncode={proc.returncode}"}

    # Walk rg --json output. Group matches by source file.
    matches_by_path: dict[str, list[dict]] = {}
    current_path: str | None = None
    for line in proc.stdout.splitlines():
        if not line:
            continue
        try:
            evt = json.loads(line)
        except json.JSONDecodeError:
            continue
        t = evt.get("type")
        data = evt.get("data") or {}
        if t == "begin":
            current_path = (data.get("path") or {}).get("text")
            if current_path:
                matches_by_path.setdefault(current_path, [])
        elif t in ("match", "context") and current_path:
            line_no = data.get("line_number")
            text = (data.get("lines") or {}).get("text", "").rstrip("\n")
            if line_no is None:
                continue
            matches_by_path[current_path].append({
                "line_number": line_no,
                "kind": t,
                "text": text,
            })

    # Resolve each path to a Doc, apply the sensitivity gate, and drop
    # matches that fall inside the frontmatter block.
    by_path_to_doc = {str(d.path): d for d in idx.docs}
    hits_by_doc: list[dict] = []
    excluded = {"secret_adjacent_skipped": 0, "unindexed_skipped": 0}

    for path_str, hits in matches_by_path.items():
        doc = by_path_to_doc.get(path_str)
        if doc is None:
            # File matched but isn't in our index (untagged, in 00 Templates/, etc.)
            excluded["unindexed_skipped"] += 1
            continue
        if doc.sensitivity is Sensitivity.SECRET_ADJACENT and not include_secret_adjacent:
            excluded["secret_adjacent_skipped"] += 1
            continue
        # Drop any hit that lands inside the frontmatter block.
        in_body = [h for h in hits if h["line_number"] >= doc.body_start_line]
        if not in_body:
            continue
        # Match-only count (excluding context lines) for ranking.
        match_count = sum(1 for h in in_body if h["kind"] == "match")
        if match_count == 0:
            continue
        hits_by_doc.append({
            **_hit_to_dict(doc),
            "match_count": match_count,
            "snippets": in_body,
            **({"advisory": advisory(doc.sensitivity)}
               if doc.sensitivity is Sensitivity.SENSITIVE else {}),
        })

    # Sort by match count (desc), then last_reviewed (desc), then title.
    hits_by_doc.sort(
        key=lambda h: (h["match_count"], h.get("last_reviewed") or "", h["title"]),
        reverse=True,
    )
    capped = hits_by_doc[: max(1, min(int(limit), 50))]

    return {
        "query": query,
        "doc_count": len(capped),
        "total_match_count": sum(h["match_count"] for h in capped),
        "excluded": excluded,
        "hits_by_doc": capped,
    }


# ----- write tool -------------------------------------------------------------

def _yaml_escape(value: str) -> str:
    """Tiny YAML scalar emitter; matches the hand-rolled parser in vault.py."""
    if value is None or value == "":
        return '""'
    if any(ch in value for ch in [":", "#", "@", "|", ">", "{", "}", "[", "]", ",", "&", "*", "!", "%", "`"]):
        return '"' + value.replace('"', '\\"') + '"'
    if value.strip() != value:
        return '"' + value + '"'
    return value


def _render_frontmatter(payload: dict[str, Any]) -> str:
    lines: list[str] = ["---"]
    lines.append(f'doc_id: {payload["doc_id"]}')
    lines.append(f'title: {_yaml_escape(payload["title"])}')
    lines.append(f'doc_type: {payload["doc_type"]}')
    lines.append(f'system: {_yaml_escape(payload["system"])}')
    lines.append(f'environment: {payload["environment"]}')
    lines.append(f'status: {payload["status"]}')
    lines.append(f'sensitivity: {payload["sensitivity"]}')
    lines.append(f'last_reviewed: {payload["last_reviewed"]}')

    def emit_list(name: str, values: list[str]) -> None:
        if not values:
            lines.append(f"{name}: []")
            return
        lines.append(f"{name}:")
        for v in values:
            lines.append(f"  - {v}")

    emit_list("related_projects", payload["related_projects"])
    emit_list("related_assets", payload["related_assets"])
    emit_list("tags", payload["tags"])

    lines.append("---")
    lines.append("")
    return "\n".join(lines) + "\n"


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

    After success, the file is reloaded into the vault cache and the caller
    is reminded to run `hq sync` to upsert HQ.

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
        sensitivity: One of public | internal | sensitive | secret_adjacent.
            Default "internal" — pick conservatively for credential/key-adjacent docs.
        tags: Optional kebab-case tag list.
        related_projects: Optional HQ Project slugs.
        related_assets: Optional HQ Asset slugs.
        last_reviewed: Optional ISO date (YYYY-MM-DD). Defaults to today.
    """
    # ---- validate enums --------------------------------------------------
    errors: list[str] = []
    if doc_type not in DOC_TYPES:
        errors.append(f"doc_type {doc_type!r} not in {sorted(DOC_TYPES)}")
    if environment not in ENVIRONMENTS:
        errors.append(f"environment {environment!r} not in {sorted(ENVIRONMENTS)}")
    if status not in STATUSES:
        errors.append(f"status {status!r} not in {sorted(STATUSES)}")
    if sensitivity not in SENSITIVITIES:
        errors.append(f"sensitivity {sensitivity!r} not in {sorted(SENSITIVITIES)}")
    if not doc_id.startswith(DOC_ID_PREFIXES):
        errors.append(
            f"doc_id {doc_id!r} must start with one of {list(DOC_ID_PREFIXES)}"
        )
    if errors:
        return {"ok": False, "errors": errors}

    # ---- validate path is inside an indexed vault dir --------------------
    vault_root = _LOADER.config.vault_path.resolve()
    full_path = (vault_root / relative_path).resolve()
    try:
        full_path.relative_to(vault_root)
    except ValueError:
        return {"ok": False, "errors": [f"path escapes vault root: {relative_path}"]}

    if not full_path.is_file():
        return {"ok": False, "errors": [f"file not found: {relative_path}"]}

    indexed_ok = any(
        full_path.is_relative_to(vault_root / sub)
        for sub in _LOADER.config.indexed_dirs
    )
    if not indexed_ok:
        return {
            "ok": False,
            "errors": [
                f"path is outside the indexed dirs: {list(_LOADER.config.indexed_dirs)}"
            ],
        }

    # ---- refuse to overwrite existing frontmatter ------------------------
    body = full_path.read_text(encoding="utf-8")
    if body.lstrip().startswith("---"):
        return {
            "ok": False,
            "errors": [
                "file already starts with `---` (existing frontmatter); "
                "edit it in Obsidian instead of using this tool."
            ],
        }

    # ---- doc_id uniqueness ----------------------------------------------
    idx = _LOADER.index(force=True)
    if doc_id in idx.by_doc_id:
        return {
            "ok": False,
            "errors": [
                f"doc_id {doc_id!r} already exists at "
                f"{idx.by_doc_id[doc_id].relative_path}"
            ],
        }

    # ---- compose + write -------------------------------------------------
    payload = {
        "doc_id": doc_id,
        "title": title,
        "doc_type": doc_type,
        "system": system,
        "environment": environment,
        "status": status,
        "sensitivity": sensitivity,
        "last_reviewed": last_reviewed or date.today().isoformat(),
        "tags": [str(t) for t in (tags or [])],
        "related_projects": [str(s) for s in (related_projects or [])],
        "related_assets": [str(s) for s in (related_assets or [])],
    }
    new_body = _render_frontmatter(payload) + body
    full_path.write_text(new_body, encoding="utf-8")

    # Invalidate cache so subsequent reads see the new doc.
    _LOADER.index(force=True)

    return {
        "ok": True,
        "doc_id": doc_id,
        "relative_path": str(full_path.relative_to(vault_root)),
        "wrote_bytes": len(new_body.encode("utf-8")),
        "next_step": "run `hq sync` from the Mac so HQ picks it up",
    }


# ----- update existing frontmatter -------------------------------------------

# Keys we render in a fixed order; anything else found in the existing block
# is preserved verbatim at the end (e.g. a doc that already had `created: ...`).
_KNOWN_KEY_ORDER = (
    "doc_id", "title", "doc_type", "system", "environment",
    "status", "sensitivity", "last_reviewed",
    "related_projects", "related_assets", "tags", "notes",
)


def _serialize_frontmatter(data: dict[str, Any]) -> str:
    """Round-trip a parsed frontmatter dict back to YAML, known keys first."""
    lines = ["---"]
    seen: set[str] = set()
    for key in _KNOWN_KEY_ORDER:
        if key not in data:
            continue
        seen.add(key)
        value = data[key]
        if isinstance(value, list):
            if not value:
                lines.append(f"{key}: []")
            else:
                lines.append(f"{key}:")
                for item in value:
                    lines.append(f"  - {item}")
        elif value is None:
            lines.append(f"{key}: null")
        elif isinstance(value, bool):
            lines.append(f"{key}: {'true' if value else 'false'}")
        else:
            lines.append(f"{key}: {_yaml_escape(str(value))}")
    # Preserve unknown keys (e.g. `created:`) at the tail of the block.
    for key, value in data.items():
        if key in seen:
            continue
        if isinstance(value, list):
            if not value:
                lines.append(f"{key}: []")
            else:
                lines.append(f"{key}:")
                for item in value:
                    lines.append(f"  - {item}")
        elif value is None:
            lines.append(f"{key}: null")
        elif isinstance(value, bool):
            lines.append(f"{key}: {'true' if value else 'false'}")
        else:
            lines.append(f"{key}: {_yaml_escape(str(value))}")
    lines.append("---")
    lines.append("")
    return "\n".join(lines) + "\n"


def _apply_list_op(
    current: list[str],
    set_to: list[str] | None,
    add: list[str] | None,
    remove: list[str] | None,
) -> list[str]:
    if set_to is not None:
        return [str(x) for x in set_to]
    out = list(current)
    if remove:
        rs = {str(r) for r in remove}
        out = [x for x in out if x not in rs]
    if add:
        for a in add:
            if a not in out:
                out.append(str(a))
    return out


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
            list ops for `related_projects` (HQ Project slugs).
        set_related_assets / add_related_assets / remove_related_assets:
            list ops for `related_assets` (HQ Asset slugs).
    """
    # ---- enum validation -------------------------------------------------
    errors: list[str] = []
    if doc_type is not None and doc_type not in DOC_TYPES:
        errors.append(f"doc_type {doc_type!r} not in {sorted(DOC_TYPES)}")
    if environment is not None and environment not in ENVIRONMENTS:
        errors.append(f"environment {environment!r} not in {sorted(ENVIRONMENTS)}")
    if status is not None and status not in STATUSES:
        errors.append(f"status {status!r} not in {sorted(STATUSES)}")
    if sensitivity is not None and sensitivity not in SENSITIVITIES:
        errors.append(f"sensitivity {sensitivity!r} not in {sorted(SENSITIVITIES)}")
    if errors:
        return {"ok": False, "errors": errors}

    # ---- path validation -------------------------------------------------
    vault_root = _LOADER.config.vault_path.resolve()
    full_path = (vault_root / relative_path).resolve()
    try:
        full_path.relative_to(vault_root)
    except ValueError:
        return {"ok": False, "errors": [f"path escapes vault root: {relative_path}"]}
    if not full_path.is_file():
        return {"ok": False, "errors": [f"file not found: {relative_path}"]}
    indexed_ok = any(
        full_path.is_relative_to(vault_root / sub)
        for sub in _LOADER.config.indexed_dirs
    )
    if not indexed_ok:
        return {
            "ok": False,
            "errors": [
                f"path is outside the indexed dirs: {list(_LOADER.config.indexed_dirs)}"
            ],
        }

    # ---- load existing ---------------------------------------------------
    text = full_path.read_text(encoding="utf-8")
    fm, body, _body_start = _split_frontmatter(text)
    if fm is None:
        return {
            "ok": False,
            "errors": [
                "file has no frontmatter — call `add_frontmatter` instead."
            ],
        }

    changed: dict[str, Any] = {}

    # Scalar updates.
    for key, value in (
        ("title", title),
        ("doc_type", doc_type),
        ("system", system),
        ("environment", environment),
        ("status", status),
        ("sensitivity", sensitivity),
    ):
        if value is not None and fm.get(key) != value:
            fm[key] = value
            changed[key] = value

    new_last_reviewed: str | None
    if touch_last_reviewed:
        new_last_reviewed = date.today().isoformat()
    elif last_reviewed is not None:
        new_last_reviewed = last_reviewed
    else:
        new_last_reviewed = None
    if new_last_reviewed is not None and fm.get("last_reviewed") != new_last_reviewed:
        fm["last_reviewed"] = new_last_reviewed
        changed["last_reviewed"] = new_last_reviewed

    # List updates.
    def _maybe_update_list(field: str, set_v, add_v, rem_v) -> None:
        if set_v is None and add_v is None and rem_v is None:
            return
        current = fm.get(field) or []
        if not isinstance(current, list):
            current = [str(current)]
        new = _apply_list_op(current, set_v, add_v, rem_v)
        if new != current:
            fm[field] = new
            changed[field] = new

    _maybe_update_list("tags", set_tags, add_tags, remove_tags)
    _maybe_update_list(
        "related_projects",
        set_related_projects, add_related_projects, remove_related_projects,
    )
    _maybe_update_list(
        "related_assets",
        set_related_assets, add_related_assets, remove_related_assets,
    )

    if not changed:
        return {
            "ok": True,
            "no_op": True,
            "doc_id": fm.get("doc_id"),
            "relative_path": str(full_path.relative_to(vault_root)),
            "message": "No fields differ — nothing written.",
        }

    new_text = _serialize_frontmatter(fm) + body
    full_path.write_text(new_text, encoding="utf-8")
    _LOADER.index(force=True)

    return {
        "ok": True,
        "doc_id": fm.get("doc_id"),
        "relative_path": str(full_path.relative_to(vault_root)),
        "changed_fields": sorted(changed.keys()),
        "next_step": "run `hq sync` from the Mac so HQ picks it up",
    }


# ----- entry point ------------------------------------------------------------

def run() -> None:
    """Start the MCP server over stdio. Invoked by `__main__.py` and the console script."""
    mcp.run()


__all__ = ["mcp", "run"]
