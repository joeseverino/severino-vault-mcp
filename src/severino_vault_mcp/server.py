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

import json
import os
import re
import shutil
import subprocess
import urllib.error
import urllib.request
from datetime import date
from pathlib import Path
from typing import Any

from mcp.server.fastmcp import FastMCP

from .config import Config
from .schema import DOC_ID_PREFIXES, DOC_TYPES, ENVIRONMENTS, SENSITIVITIES, STATUSES
from .search import rank, tokenize
from .secret_unlock import (
    SecretUnlockResult,
    audit_secret_unlock,
    load_unlock_hash,
    prompt_unlock_phrase,
    verify_unlock_phrase,
)
from .sensitivity import Sensitivity, advisory, body_is_releasable
from .tech_groups import load_technology_catalog
from .vault import Doc, VaultLoader, _normalize_alias, _split_frontmatter
from .writeups import extract_body_image_refs, load_writeups

QUICK_INDEX_DOC_ID = "report-playbook-mcp-index"
QUICK_INDEX_RESOURCE_URI = "vault://quick-index"
DOC_RESOURCE_TEMPLATE_URI = "vault://doc/{doc_id}"
JSEVERINO_D1_DATABASE = os.environ.get("SVMC_JSEVERINO_D1_DATABASE", "jseverino-contact")
JSEVERINO_SITE_REPO = Path(
    os.path.expanduser(
        os.environ.get(
            "SVMC_JSEVERINO_SITE_REPO",
            "~/Documents/Code/Projects/jseverino.com",
        )
    )
)
JSEVERINO_SITE_ORIGIN = os.environ.get("SVMC_JSEVERINO_SITE_ORIGIN", "https://jseverino.com")


_CONFIG = Config.from_env()
_LOADER = VaultLoader(_CONFIG)

JSEVERINO_WRITEUPS_DIR = Path(
    os.path.expanduser(
        os.environ.get(
            "SVMC_JSEVERINO_WRITEUPS_DIR",
            str(_CONFIG.vault_path / "05 Writeups"),
        )
    )
)
JSEVERINO_TECH_GROUPS = Path(
    os.path.expanduser(
        os.environ.get(
            "SVMC_JSEVERINO_TECH_GROUPS",
            str(_CONFIG.vault_path / "06 Pages" / "_technology-groups.md"),
        )
    )
)

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

1. `list_writeups(filter)` — for ANY question about which writeups
   exist, what's published, what's featured, or what `featured_order`
   each writeup has. Do NOT grep `05 Writeups/*/index.md` for these.
   The `filter="featured"` view sorts by `featured_order` ascending and
   is the only correct way to reason about the home-cloud order.

2. `validate_writeup(slug)` — before claiming a writeup is publish-ready.
   Returns a structured report on frontmatter completeness, tech-slug
   coverage against the catalog, and image references vs files on disk.
   "I read the file" is not the same as "I validated it."

3. `get_technology_catalog()` — for any question about technology slugs,
   their groups, or their featured state. Do NOT parse
   `_technology-groups.md` markdown tables by hand.

4. `find_writeups_using_tag(slug)` — before recommending that any tag be
   promoted to featured. The site's home cloud rule is "featured AND
   referenced by >=1 published writeup"; this tool answers the second
   half directly without grep.

5. `prepare_writeup_publish(slug)` — ONE call that composes (1)+(2)+(4)
   into a single response (validation + featured order + tag usage).
   Prefer this over chaining the individual tools by hand when you are
   about to publish a writeup.

WRITEUP MUTATIONS (use these, never `Edit` on writeup YAML):

6. `update_writeup_frontmatter(slug, ...)` — for any single-writeup
   frontmatter change (flip `published`, bump `last_reviewed`, change
   `cover_image`, etc.). Mutates only the changed lines; surrounding
   formatting is preserved.

7. `reorder_featured(slug, position)` — for ANY change to the featured
   list (insert new, move existing, unfeature). The tool guarantees the
   resulting order is sequential 1..N. Hand-shuffling featured_order
   across multiple files is the exact failure mode that produced v2.4.2.

VERIFY BEFORE SHIPPING: call `prepare_writeup_publish(slug)` (or at
minimum `validate_writeup(slug)` + `list_writeups("featured")`)
*immediately before* the commit/push of a writeup, not after. The check
exists to catch wrong `featured_order` values, missing tech-catalog
entries, missing images, and unresolved `related_projects` /
`related_assets` references before they reach production. Shipping
first and verifying second is exactly how this MCP came to exist; do
not repeat that pattern.

If you find yourself about to grep frontmatter, parse the catalog
markdown, count featured writeups by hand, or run `Edit` on a writeup's
frontmatter block: stop and call the corresponding tool above. Apply
the same discipline as Rule 2 above about runbooks — don't reinvent
what these tools already do.

The configured vault root and indexed directories are controlled by
`config.example.toml`-style settings or `SVMC_*` environment overrides.
"""

mcp = FastMCP("severino-vault-mcp", instructions=_SERVER_INSTRUCTIONS)


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


def _split_table_row(line: str) -> list[str]:
    return [cell.strip() for cell in line.strip().strip("|").split("|")]


def _is_table_separator(cells: list[str]) -> bool:
    return bool(cells) and all(set(cell.replace(" ", "")) <= {"-", ":"} for cell in cells)


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


def _add_quick_index_recommendation(response: dict[str, Any], idx, query: str) -> None:
    matches = _quick_index_matches(idx, query)
    if not matches:
        return
    response["quick_index_matches"] = matches
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


def _bounded_limit(value: int, *, default: int = 20, max_value: int = 100) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = default
    return max(1, min(parsed, max_value))


def _wrangler_available() -> str | None:
    return shutil.which("wrangler")


def _run_wrangler_d1_json(command: str, *, timeout: int = 20) -> dict[str, Any]:
    """Run one fixed D1 SQL command through Wrangler and parse JSON output."""
    wrangler = _wrangler_available()
    if not wrangler:
        return {"ok": False, "error": "wrangler not found on PATH"}

    try:
        proc = subprocess.run(
            [
                wrangler,
                "d1",
                "execute",
                JSEVERINO_D1_DATABASE,
                "--remote",
                "--json",
                "--command",
                command,
            ],
            cwd=str(JSEVERINO_SITE_REPO),
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"ok": False, "error": f"wrangler d1 execute failed: {exc}"}

    if proc.returncode != 0:
        return {
            "ok": False,
            "returncode": proc.returncode,
            "stderr": proc.stderr.strip(),
            "stdout": proc.stdout.strip(),
        }

    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {"ok": False, "error": "wrangler returned non-JSON output", "stdout": proc.stdout}

    result = payload[0] if isinstance(payload, list) and payload else {}
    return {
        "ok": bool(result.get("success", True)),
        "database": JSEVERINO_D1_DATABASE,
        "results": result.get("results", []),
        "meta": result.get("meta", {}),
    }


def _sql_string(value: str) -> str:
    """SQLite single-quoted literal for fixed internal filters."""
    return "'" + value.replace("'", "''") + "'"


def _head_headers(url: str, *, timeout: int = 10) -> dict[str, Any]:
    curl = shutil.which("curl")
    if curl:
        try:
            proc = subprocess.run(
                [curl, "-sI", "--max-time", str(timeout), url],
                capture_output=True,
                text=True,
                timeout=timeout + 2,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            return {"ok": False, "url": url, "error": f"curl HEAD failed: {exc}"}

        if proc.returncode == 0 and proc.stdout:
            lines = [line.strip() for line in proc.stdout.splitlines() if line.strip()]
            status = 0
            headers: dict[str, str] = {}
            for line in lines:
                if line.lower().startswith("http/"):
                    parts = line.split()
                    if len(parts) > 1 and parts[1].isdigit():
                        status = int(parts[1])
                    continue
                if ":" in line:
                    key, value = line.split(":", 1)
                    headers[key.lower()] = value.strip()
            return {"ok": 200 <= status < 400, "url": url, "status": status, "headers": headers}

    request = urllib.request.Request(url, method="HEAD")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            headers = {key.lower(): value for key, value in response.headers.items()}
            return {
                "ok": True,
                "url": url,
                "status": response.status,
                "headers": headers,
            }
    except urllib.error.HTTPError as exc:
        headers = {key.lower(): value for key, value in exc.headers.items()}
        return {"ok": False, "url": url, "status": exc.code, "headers": headers}
    except (OSError, urllib.error.URLError) as exc:
        return {"ok": False, "url": url, "error": str(exc)}


def _selected_security_headers(headers: dict[str, str]) -> dict[str, str | None]:
    names = [
        "content-security-policy",
        "reporting-endpoints",
        "strict-transport-security",
        "x-content-type-options",
        "referrer-policy",
        "permissions-policy",
        "cross-origin-opener-policy",
        "cross-origin-resource-policy",
    ]
    return {name: headers.get(name) for name in names}


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
    idx = _LOADER.index()
    hits = rank(idx.docs, query, limit=max(1, min(limit, 25)))
    response = {
        "query": query,
        "indexed_doc_count": len(idx.docs),
        "hits": [{"score": h.score, **_hit_to_dict(h.doc)} for h in hits],
    }
    _add_quick_index_recommendation(response, idx, query)
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
    idx = _LOADER.index()
    hits = rank(idx.docs, query, limit=max(1, min(limit, 25)))
    response: dict[str, Any] = {
        "query": query,
        "indexed_doc_count": len(idx.docs),
        "hits": [{"score": h.score, **_hit_to_dict(h.doc)} for h in hits],
    }
    _add_quick_index_recommendation(response, idx, query)
    if not hits:
        response["found"] = False
        return response

    doc = hits[0].doc
    selected: dict[str, Any] = {"score": hits[0].score, **_hit_to_dict(doc)}
    if doc.sensitivity is Sensitivity.SECRET_ADJACENT:
        response["found"] = True
        response["selected"] = _withheld_secret_adjacent_response(selected, "not_requested")
        return response

    selected["body"] = doc.body
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


@mcp.tool()
def read_doc(
    doc_id: str,
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

    Args:
        doc_id: Stable identifier from the doc's frontmatter, e.g. "rb-add-nginx-proxy-host".
            Exact doc titles and vault-relative filenames are also accepted as
            a fallback for smaller local models, but stable `doc_id` values are
            preferred.
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
            return base
        return _withheld_secret_adjacent_response(base, unlock.result)

    if body_is_releasable(doc.sensitivity, include_secret_adjacent=requested_restricted):
        base["body"] = doc.body
        base["body_released"] = True
        if doc.sensitivity is Sensitivity.SENSITIVE:
            base["advisory"] = advisory(doc.sensitivity)
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
    include_restricted: bool = False,
    include_secret_adjacent: bool = False,
) -> dict[str, Any]:
    """Full-text search across vault doc bodies using ripgrep.

    Searches the content of every indexed `.md`, skipping matches that fall
    inside frontmatter blocks. Results are grouped by `doc_id`. Honors the
    sensitivity gate the same way `read_doc` does: `sensitive` docs are
    included with an advisory; `restricted` docs are always excluded.
    Use `read_doc(..., include_restricted=True)` for per-doc local
    unlock requests.

    Args:
        query: Literal string or regex (ripgrep regex syntax).
        limit: Max number of distinct documents to return (default 10).
        context_lines: Lines of context above + below each match (default 1).
        case_sensitive: If True, match case. Default False.
        include_restricted: Deprecated compatibility flag. Restricted bodies
            are never searched; per-doc local unlock is only available through
            `read_doc`.
        include_secret_adjacent: Deprecated compatibility flag. Restricted
            bodies are never searched; per-doc local unlock is only available
            through `read_doc`.
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
    excluded = {
        "restricted_skipped": 0,
        "secret_adjacent_skipped": 0,
        "unindexed_skipped": 0,
    }

    for path_str, hits in matches_by_path.items():
        doc = by_path_to_doc.get(path_str)
        if doc is None:
            # File matched but isn't in our index (untagged, in 00 Templates/, etc.)
            excluded["unindexed_skipped"] += 1
            continue
        if doc.sensitivity is Sensitivity.SECRET_ADJACENT:
            excluded["restricted_skipped"] += 1
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


# ----- jseverino.com operations tools ----------------------------------------

@mcp.tool()
def list_contact_submissions(limit: int = 10) -> dict[str, Any]:
    """List recent jseverino.com contact form submissions from Cloudflare D1.

    This is a fixed, read-only wrapper around `wrangler d1 execute` for the
    operator's `jseverino-contact` database. It does not accept arbitrary SQL.

    Args:
        limit: Maximum rows to return, capped at 100.
    """
    limit = _bounded_limit(limit, default=10, max_value=100)
    sql = (
        "SELECT id, created_at, name, email, status, browser, device, country, source_url "
        "FROM contact_submissions ORDER BY created_at DESC LIMIT "
        f"{limit};"
    )
    return _run_wrangler_d1_json(sql)


@mcp.tool()
def list_csp_reports(limit: int = 20, directive: str | None = None) -> dict[str, Any]:
    """List recent jseverino.com CSP violation reports from Cloudflare D1.

    Browser-extension/off-site noise is filtered by the report receiver before
    rows reach this table. This tool is read-only and does not accept arbitrary SQL.

    Args:
        limit: Maximum rows to return, capped at 100.
        directive: Optional exact `effective_directive` filter, e.g. "script-src".
    """
    limit = _bounded_limit(limit, default=20, max_value=100)
    where = ""
    if directive:
        directive = directive.strip()[:128]
        if directive:
            where = f"WHERE effective_directive = {_sql_string(directive)} "
    sql = (
        "SELECT id, created_at, effective_directive, blocked_uri, document_uri, "
        "source_file, status_code FROM csp_reports "
        f"{where}ORDER BY created_at DESC LIMIT {limit};"
    )
    return _run_wrangler_d1_json(sql)


@mcp.tool()
def count_csp_reports() -> dict[str, Any]:
    """Return CSP report counts for jseverino.com from Cloudflare D1.

    Includes total count and grouped counts by `effective_directive`.
    """
    total = _run_wrangler_d1_json("SELECT COUNT(*) AS total FROM csp_reports;")
    by_directive = _run_wrangler_d1_json(
        "SELECT COALESCE(effective_directive, '(unknown)') AS effective_directive, "
        "COUNT(*) AS count FROM csp_reports GROUP BY effective_directive "
        "ORDER BY count DESC, effective_directive ASC;"
    )
    return {
        "ok": bool(total.get("ok") and by_directive.get("ok")),
        "database": JSEVERINO_D1_DATABASE,
        "total": total,
        "by_directive": by_directive,
    }


@mcp.tool()
def apply_jseverino_d1_schema(confirm: bool = False) -> dict[str, Any]:
    """Apply `db/schema.sql` to the remote jseverino.com D1 database.

    This is a fixed write operation for the operator's own site. It refuses to
    run unless `confirm=True` is passed. The schema uses `CREATE ... IF NOT
    EXISTS` and is intended for additive table/index updates.

    Args:
        confirm: Must be True to execute the remote schema import.
    """
    if not confirm:
        return {
            "ok": False,
            "refused": True,
            "message": "Pass confirm=True to apply db/schema.sql to the remote D1 database.",
        }

    wrangler = _wrangler_available()
    if not wrangler:
        return {"ok": False, "error": "wrangler not found on PATH"}

    schema = JSEVERINO_SITE_REPO / "db" / "schema.sql"
    if not schema.is_file():
        return {"ok": False, "error": f"schema file not found: {schema}"}

    try:
        proc = subprocess.run(
            [
                wrangler,
                "d1",
                "execute",
                JSEVERINO_D1_DATABASE,
                "--remote",
                "--file",
                str(schema),
            ],
            cwd=str(JSEVERINO_SITE_REPO),
            capture_output=True,
            text=True,
            timeout=60,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"ok": False, "error": f"wrangler d1 schema apply failed: {exc}"}

    return {
        "ok": proc.returncode == 0,
        "database": JSEVERINO_D1_DATABASE,
        "schema": str(schema),
        "returncode": proc.returncode,
        "stdout": proc.stdout.strip(),
        "stderr": proc.stderr.strip(),
    }


# ----- jseverino.com writeup tools -------------------------------------------

_WRITEUP_FILTERS = ("all", "published", "draft", "featured")


def _writeup_summary(writeup) -> dict[str, Any]:
    return writeup.to_summary()


@mcp.tool()
def list_writeups(filter: str = "all") -> dict[str, Any]:
    """USE THIS — never grep `05 Writeups/*/index.md` for writeup state.

    Single source of truth for "which writeups exist, what's published,
    what's featured, what's the featured_order." The `filter="featured"`
    view sorts by `featured_order` ascending — that's the order the home
    cloud renders, and the only correct way to reason about placement.

    If you are about to run `rg featured_order 05 Writeups/`, `cat` a
    writeup's frontmatter, or count the featured set in your head: stop
    and call this instead. Hand-counting featured_order across multiple
    files is exactly how publishes ship with the wrong order.

    Args:
        filter: One of "all", "published", "draft", "featured". The
            "featured" filter sorts by `featured_order` ascending.
    """
    chosen = (filter or "all").strip().lower()
    if chosen not in _WRITEUP_FILTERS:
        return {
            "ok": False,
            "error": f"unknown filter {filter!r}; expected one of {list(_WRITEUP_FILTERS)}",
        }

    if not JSEVERINO_WRITEUPS_DIR.is_dir():
        return {"ok": False, "error": f"writeups dir not found: {JSEVERINO_WRITEUPS_DIR}"}

    writeups = load_writeups(JSEVERINO_WRITEUPS_DIR)
    if chosen == "published":
        writeups = [w for w in writeups if w.published]
    elif chosen == "draft":
        writeups = [w for w in writeups if not w.published]
    elif chosen == "featured":
        writeups = [w for w in writeups if w.featured]
        writeups.sort(
            key=lambda w: (
                w.featured_order if w.featured_order is not None else 10**9,
                w.slug,
            )
        )

    return {
        "ok": True,
        "filter": chosen,
        "writeups_dir": str(JSEVERINO_WRITEUPS_DIR),
        "count": len(writeups),
        "writeups": [_writeup_summary(w) for w in writeups],
    }


@mcp.tool()
def get_technology_catalog() -> dict[str, Any]:
    """USE THIS — never parse `_technology-groups.md` markdown by hand.

    Returns the catalog grouped by section with each slug's label and
    featured state. Call before featuring a tag, before adding a new
    technology to a writeup's `technologies:` list, or before answering
    any "does slug X exist?" question. The catalog file is markdown
    tables — parsing them by hand is the path to introducing slugs the
    site build warns about.
    """
    if not JSEVERINO_TECH_GROUPS.is_file():
        return {"ok": False, "error": f"catalog not found: {JSEVERINO_TECH_GROUPS}"}

    catalog = load_technology_catalog(JSEVERINO_TECH_GROUPS)
    if not catalog:
        return {
            "ok": False,
            "error": f"catalog file present but no slugs parsed: {JSEVERINO_TECH_GROUPS}",
        }

    by_group: dict[str, list[dict[str, Any]]] = {}
    for entry in catalog:
        by_group.setdefault(entry.group, []).append(
            {"slug": entry.slug, "label": entry.label, "featured": entry.featured}
        )

    featured_count = sum(1 for entry in catalog if entry.featured)
    return {
        "ok": True,
        "catalog_path": str(JSEVERINO_TECH_GROUPS),
        "total_slugs": len(catalog),
        "featured_count": featured_count,
        "by_group": by_group,
    }


@mcp.tool()
def find_writeups_using_tag(slug: str) -> dict[str, Any]:
    """USE THIS before recommending any tag be promoted to featured.

    The home cloud rule is "featured AND referenced by >=1 published
    writeup." This tool answers the second half directly — never grep
    writeup frontmatter for tag occurrences. A "this tag is earned"
    claim without a call to this tool is unverified.

    Args:
        slug: Technology slug from the catalog, e.g. "obsidian".
    """
    slug = (slug or "").strip()
    if not slug:
        return {"ok": False, "error": "slug required"}

    if not JSEVERINO_WRITEUPS_DIR.is_dir():
        return {"ok": False, "error": f"writeups dir not found: {JSEVERINO_WRITEUPS_DIR}"}

    writeups = load_writeups(JSEVERINO_WRITEUPS_DIR)
    matches = [w for w in writeups if slug in w.technologies]

    return {
        "ok": True,
        "slug": slug,
        "total_matches": len(matches),
        "published_matches": sum(1 for w in matches if w.published),
        "writeups": [
            {
                "slug": w.slug,
                "title": w.title,
                "published": w.published,
                "featured": w.featured,
            }
            for w in matches
        ],
    }


@mcp.tool()
def validate_writeup(slug: str) -> dict[str, Any]:
    """CALL THIS BEFORE every writeup commit. Publish-readiness report.

    Inspects:
    - Frontmatter completeness (title, description, published, published_at,
      cover_image, technologies).
    - Tech slugs vs the catalog at `_technology-groups.md` — flags slugs
      the site build will warn about.
    - Images referenced from the body vs files present in the writeup's
      `images/` folder.
    - Soft nitpicks (description length, missing optional fields).

    Returns `ok: true` only when there are no blockers, no missing tech
    slugs, and no missing images. Nits are informational.

    Args:
        slug: Writeup slug, e.g. "building-a-custom-mcp-layer".
    """
    slug = (slug or "").strip()
    if not slug:
        return {"ok": False, "error": "slug required"}

    if not JSEVERINO_WRITEUPS_DIR.is_dir():
        return {"ok": False, "error": f"writeups dir not found: {JSEVERINO_WRITEUPS_DIR}"}

    writeup_dir = JSEVERINO_WRITEUPS_DIR / slug
    if not writeup_dir.is_dir():
        return {"ok": False, "error": f"writeup folder not found: {slug}"}

    writeups = load_writeups(JSEVERINO_WRITEUPS_DIR)
    writeup = next((w for w in writeups if w.slug == slug), None)
    if writeup is None:
        return {"ok": False, "error": f"writeup has no frontmatter or index.md: {slug}"}

    blockers: list[str] = []
    nits: list[str] = []

    if not writeup.title:
        blockers.append("title missing")
    if not writeup.description:
        blockers.append("description missing")
    elif len(writeup.description) > 300:
        nits.append(f"description is {len(writeup.description)} chars (recommend <=300)")
    if not writeup.published:
        blockers.append("published is false — flip to true to ship")
    if not writeup.published_at:
        blockers.append("published_at empty — set ISO date when ready")
    if not writeup.cover_image:
        nits.append("cover_image missing")
    if not writeup.technologies:
        nits.append("technologies list empty")

    catalog = load_technology_catalog(JSEVERINO_TECH_GROUPS)
    catalog_slugs = {entry.slug for entry in catalog}
    missing_slugs: list[str] = []
    if catalog_slugs:
        missing_slugs = [s for s in writeup.technologies if s not in catalog_slugs]
    else:
        nits.append(f"technology catalog not found at {JSEVERINO_TECH_GROUPS}; skipping slug check")

    body_image_refs = extract_body_image_refs(writeup.body)
    images_dir = writeup_dir / "images"
    present_images = (
        {p.name for p in images_dir.iterdir() if p.is_file()}
        if images_dir.is_dir()
        else set()
    )
    missing_images: list[str] = []
    for ref in body_image_refs:
        ref_name = Path(ref).name
        if ref_name and ref_name not in present_images:
            missing_images.append(ref)

    # related_projects / related_assets must resolve to indexed vault docs.
    # The convention is project-<slug> for projects; assets either share that
    # prefix or match a vault doc filename stem. Anything that doesn't resolve
    # is a soft-shipped dangling reference that HQ flags repeatedly.
    unresolved_refs: list[str] = []
    vault_idx = _LOADER.index()
    project_doc_ids = {d.doc_id for d in vault_idx.docs}
    doc_stems_lower = {d.path.stem.lower() for d in vault_idx.docs}
    for ref in writeup.related_projects:
        if f"project-{ref}" not in project_doc_ids and ref.lower() not in doc_stems_lower:
            unresolved_refs.append(f"related_projects: {ref} (no matching vault doc)")
    for ref in writeup.related_assets:
        if (
            f"project-{ref}" not in project_doc_ids
            and ref not in project_doc_ids
            and ref.lower() not in doc_stems_lower
        ):
            unresolved_refs.append(f"related_assets: {ref} (no matching vault doc)")

    ok = not blockers and not missing_slugs and not missing_images and not unresolved_refs
    return {
        "ok": ok,
        "slug": slug,
        "frontmatter": _writeup_summary(writeup),
        "blockers": blockers,
        "missing_tech_slugs": missing_slugs,
        "missing_images": missing_images,
        "unresolved_refs": unresolved_refs,
        "nits": nits,
    }


@mcp.tool()
def prepare_writeup_publish(slug: str) -> dict[str, Any]:
    """ONE-CALL publish prep. Use this BEFORE every writeup commit.

    Composes `validate_writeup`, `list_writeups("featured")`, and tag
    cross-checks so a single call returns everything you need to decide
    "is this writeup safe to ship right now":

    - `validation`: full `validate_writeup` result (blockers, missing
      tech slugs, missing images, nits).
    - `featured_set`: current featured order, sorted ascending, plus
      this writeup's position in it (or `null` if unfeatured). Confirms
      the order without hand-counting across files.
    - `tag_usage`: for each of this writeup's `technologies:`, how many
      writeups reference it total and how many of those are published.
      Surfaces tags that are over- or under-used.

    `ok: true` means: frontmatter complete, all tech slugs exist in the
    catalog, all referenced images exist on disk. If `ok` is true, the
    writeup is safe to commit + push. If false, the `validation.blockers`
    field tells you what to fix.

    This replaces the 3-4 separate MCP calls you would otherwise chain.
    Prefer this over calling `validate_writeup` + `list_writeups` +
    `find_writeups_using_tag` individually.

    Args:
        slug: Writeup slug, e.g. "building-a-custom-mcp-layer".
    """
    validation = validate_writeup(slug)
    featured = list_writeups("featured")

    featured_writeups = featured.get("writeups", []) if isinstance(featured, dict) else []
    position: int | None = None
    for entry in featured_writeups:
        if entry.get("slug") == slug:
            position = entry.get("featured_order")
            break

    tag_usage: dict[str, dict[str, Any]] = {}
    technologies = (
        validation.get("frontmatter", {}).get("technologies", [])
        if isinstance(validation, dict)
        else []
    )
    for tag in technologies:
        usage = find_writeups_using_tag(tag)
        if isinstance(usage, dict) and usage.get("ok"):
            tag_usage[tag] = {
                "total_writeups": usage.get("total_matches", 0),
                "published_writeups": usage.get("published_matches", 0),
            }

    return {
        "ok": bool(validation.get("ok")) if isinstance(validation, dict) else False,
        "slug": slug,
        "validation": validation,
        "featured_set": {
            "count": featured.get("count", 0) if isinstance(featured, dict) else 0,
            "order": [
                {"slot": entry.get("featured_order"), "slug": entry.get("slug")}
                for entry in featured_writeups
            ],
            "this_writeup_position": position,
        },
        "tag_usage": tag_usage,
    }


# ----- writeup write tools ---------------------------------------------------

_WRITEUP_SCALAR_KEYS = (
    "title", "description", "published", "published_at", "last_reviewed",
    "cover_image", "featured", "featured_order",
)


def _yaml_writeup_scalar(value: Any) -> str:
    """Serialize a Python value to a YAML scalar for writeup frontmatter."""
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    s = str(value)
    if s == "":
        return ""
    needs_quote = (
        s != s.strip()
        or s[0] in "&*!%@`>|"
        or any(ch in s for ch in [":", "#"])
    )
    if needs_quote:
        return '"' + s.replace('"', '\\"') + '"'
    return s


def _replace_writeup_scalar(text: str, key: str, raw_value: str) -> str:
    """Replace the value of a scalar frontmatter key in writeup index.md.

    If the key exists, only its value line is changed (preserving formatting
    of every other line). If it doesn't exist, the key is inserted just
    before the closing `---` of the frontmatter block.
    """
    pattern = re.compile(rf'^({re.escape(key)}):[^\n]*$', re.MULTILINE)
    replacement = f"{key}: {raw_value}".rstrip()
    if pattern.search(text):
        return pattern.sub(replacement, text, count=1)

    # Insert before the closing --- of the frontmatter block.
    lines = text.split("\n")
    fence_count = 0
    for i, line in enumerate(lines):
        if line.strip() == "---":
            fence_count += 1
            if fence_count == 2:
                lines.insert(i, replacement)
                return "\n".join(lines)
    # No closing fence — prepend.
    return replacement + "\n" + text


def _load_writeup_or_error(slug: str) -> tuple[Any, dict[str, Any] | None]:
    """Find a single writeup by slug or return an error response."""
    if not JSEVERINO_WRITEUPS_DIR.is_dir():
        return None, {"ok": False, "error": f"writeups dir not found: {JSEVERINO_WRITEUPS_DIR}"}
    writeup_dir = JSEVERINO_WRITEUPS_DIR / slug
    if not writeup_dir.is_dir():
        return None, {"ok": False, "error": f"writeup folder not found: {slug}"}
    writeups = load_writeups(JSEVERINO_WRITEUPS_DIR)
    writeup = next((w for w in writeups if w.slug == slug), None)
    if writeup is None:
        return None, {"ok": False, "error": f"writeup has no frontmatter or index.md: {slug}"}
    return writeup, None


@mcp.tool()
def update_writeup_frontmatter(
    slug: str,
    title: str | None = None,
    description: str | None = None,
    published: bool | None = None,
    published_at: str | None = None,
    last_reviewed: str | None = None,
    touch_last_reviewed: bool = False,
    cover_image: str | None = None,
    featured: bool | None = None,
    featured_order: int | None = None,
) -> dict[str, Any]:
    """USE THIS — never Edit YAML in `05 Writeups/<slug>/index.md` by hand.

    Mirrors `update_frontmatter` but for the writeup schema (no doc_id,
    has published/featured/featured_order). Mutates scalar fields with
    minimal disruption to surrounding formatting; lines you don't touch
    stay byte-identical.

    For cross-writeup reordering of `featured_order` (slotting a new
    writeup in at position N and shifting others), call
    `reorder_featured(slug, position)` instead — this tool only mutates
    one writeup at a time and won't keep the featured set sequential.

    Args:
        slug: Writeup slug.
        title, description, published_at, cover_image: scalar updates.
            None means leave unchanged.
        published, featured: boolean updates. None means leave unchanged.
        featured_order: integer slot, or null to clear. To fully
            unfeature, pass `featured=False, featured_order=None`.
        last_reviewed: ISO date (YYYY-MM-DD). Ignored if
            `touch_last_reviewed=True`.
        touch_last_reviewed: if True, set last_reviewed to today.
    """
    writeup, err = _load_writeup_or_error(slug)
    if err:
        return err

    if touch_last_reviewed:
        last_reviewed_value: str | None = date.today().isoformat()
    else:
        last_reviewed_value = last_reviewed

    candidates: list[tuple[str, Any, Any]] = [
        ("title", writeup.title, title),
        ("description", writeup.description, description),
        ("published", writeup.published, published),
        ("published_at", writeup.published_at, published_at),
        ("last_reviewed", writeup.last_reviewed, last_reviewed_value),
        ("cover_image", writeup.cover_image, cover_image),
        ("featured", writeup.featured, featured),
        ("featured_order", writeup.featured_order, featured_order),
    ]

    updates: dict[str, Any] = {}
    for key, current, new in candidates:
        if new is None:
            # `None` means "leave this field unchanged." To clear
            # featured_order, callers should use reorder_featured(slug, 0).
            continue
        if current != new:
            updates[key] = new

    if not updates:
        return {
            "ok": True,
            "no_op": True,
            "slug": slug,
            "message": "No fields differ — nothing written.",
        }

    text = writeup.path.read_text(encoding="utf-8")
    for key, value in updates.items():
        text = _replace_writeup_scalar(text, key, _yaml_writeup_scalar(value))
    writeup.path.write_text(text, encoding="utf-8")

    return {
        "ok": True,
        "slug": slug,
        "relative_path": str(writeup.path.relative_to(_LOADER.config.vault_path)),
        "changed_fields": sorted(updates.keys()),
        "values": {k: (None if v is None else str(v)) for k, v in updates.items()},
    }


@mcp.tool()
def reorder_featured(slug: str, position: int) -> dict[str, Any]:
    """USE THIS — never hand-shuffle featured_order across multiple files.

    Atomically reorders the featured-writeups list. The exact failure mode
    this tool prevents is documented in CHANGELOG v2.4.2: hand-editing
    featured_order across 5+ files in a row is how publishes ship with
    the wrong slot value.

    Behavior:

    - position >= 1 and slug currently UNfeatured: insert at `position`,
      shift everyone at >=position down by 1.
    - position >= 1 and slug already featured: move from current slot
      to `position`, shifting others to keep the list sequential 1..N.
    - position == 0: unfeature `slug` (set featured=false, clear
      featured_order); existing featured writeups shift up to close the
      gap.

    The resulting featured order is guaranteed sequential 1..N with no
    gaps and no duplicates.

    Args:
        slug: Writeup slug to move.
        position: Target slot (1-indexed) or 0 to unfeature.
    """
    if not isinstance(position, int) or position < 0:
        return {"ok": False, "error": "position must be an integer >= 0"}

    target, err = _load_writeup_or_error(slug)
    if err:
        return err

    all_writeups = load_writeups(JSEVERINO_WRITEUPS_DIR)
    featured_now = sorted(
        (w for w in all_writeups if w.featured),
        key=lambda w: (
            w.featured_order if w.featured_order is not None else 10**9,
            w.slug,
        ),
    )
    others = [w for w in featured_now if w.slug != slug]

    if position == 0:
        new_order: list = list(others)
        target_new_position: int | None = None
    else:
        max_position = len(others) + 1
        if position > max_position:
            return {
                "ok": False,
                "error": f"position {position} out of range (max {max_position})",
            }
        new_order = others[: position - 1] + [target] + others[position - 1 :]
        target_new_position = position

    changed_writeups: list[str] = []

    # Update every writeup whose featured state or slot differs from the new
    # plan. Target gets unfeatured if position==0; otherwise it joins
    # new_order at the desired slot.
    for i, w in enumerate(new_order, start=1):
        desired_featured = True
        desired_order = i
        if w.featured != desired_featured or w.featured_order != desired_order:
            text = w.path.read_text(encoding="utf-8")
            text = _replace_writeup_scalar(text, "featured", _yaml_writeup_scalar(desired_featured))
            text = _replace_writeup_scalar(text, "featured_order", _yaml_writeup_scalar(desired_order))
            w.path.write_text(text, encoding="utf-8")
            changed_writeups.append(w.slug)

    if position == 0 and (target.featured or target.featured_order is not None):
        text = target.path.read_text(encoding="utf-8")
        text = _replace_writeup_scalar(text, "featured", _yaml_writeup_scalar(False))
        text = _replace_writeup_scalar(text, "featured_order", _yaml_writeup_scalar(None))
        target.path.write_text(text, encoding="utf-8")
        if target.slug not in changed_writeups:
            changed_writeups.append(target.slug)

    # Re-read and report the resulting order.
    after = load_writeups(JSEVERINO_WRITEUPS_DIR)
    featured_after = sorted(
        (w for w in after if w.featured),
        key=lambda w: (
            w.featured_order if w.featured_order is not None else 10**9,
            w.slug,
        ),
    )

    return {
        "ok": True,
        "slug": slug,
        "new_position": target_new_position,
        "changed_writeups": changed_writeups,
        "featured_order_after": [
            {"slot": w.featured_order, "slug": w.slug} for w in featured_after
        ],
    }


# ----- jseverino.com live-site tools -----------------------------------------

@mcp.tool()
def check_jseverino_security_headers(path: str = "/") -> dict[str, Any]:
    """Check live jseverino.com security headers for one path.

    Uses a HEAD request against `https://jseverino.com` and returns the security
    headers that matter for the Astro/Cloudflare Pages stack.

    Args:
        path: Site path to check, e.g. "/" or "/contact/". Must be root-relative.
    """
    path = (path or "/").strip()
    if not path.startswith("/"):
        return {"ok": False, "error": "path must start with '/'"}
    if path.startswith("//"):
        return {"ok": False, "error": "path must be root-relative, not protocol-relative"}

    url = f"{JSEVERINO_SITE_ORIGIN.rstrip('/')}{path}"
    result = _head_headers(url)
    headers = result.get("headers", {})
    selected = _selected_security_headers(headers if isinstance(headers, dict) else {})
    csp = selected.get("content-security-policy") or ""
    reporting = selected.get("reporting-endpoints") or ""
    return {
        **result,
        "selected_headers": selected,
        "checks": {
            "has_csp": bool(csp),
            "no_unsafe_inline_script": "script-src 'unsafe-inline'" not in csp,
            "has_csp_report_to": "report-to csp-endpoint" in csp,
            "has_csp_report_uri": "report-uri https://jseverino.com/api/csp-report" in csp,
            "has_reporting_endpoints": "csp-endpoint=" in reporting,
        },
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
        environment: One of lab | homelab | vps | wordpress | cloudflare |
            tailscale | adguard | unifi | local_mac | other. Default "other".
        status: One of draft | active | deprecated | archived. Default "active".
        sensitivity: One of public | internal | sensitive | restricted.
            Default "internal" — pick conservatively for credential/key-adjacent docs.
        tags: Optional kebab-case tag list.
        related_projects: Optional project slugs.
        related_assets: Optional asset slugs.
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
        "next_step": "run any downstream vault metadata sync if your workflow uses one",
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
            list ops for `related_projects` (project slugs).
        set_related_assets / add_related_assets / remove_related_assets:
            list ops for `related_assets` (asset slugs).
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
        "next_step": "run any downstream vault metadata sync if your workflow uses one",
    }


# ----- entry point ------------------------------------------------------------

def run() -> None:
    """Start the MCP server over stdio. Invoked by `__main__.py` and the console script."""
    mcp.run()


__all__ = ["mcp", "run"]
