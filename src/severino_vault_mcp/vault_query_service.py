"""Read-only vault query logic shared by the MCP server.

These are the last two tools whose logic shells out (``git log`` for change
history, ``ripgrep`` for body search). Keeping them here — FastMCP-free, like
every other service module — means ``server.py`` only registers the tool and
delegates, and the same ``Doc`` -> hit projection (:func:`doc_to_hit`) is used by
every search-shaped response.
"""

from __future__ import annotations

import json
import shutil
import subprocess
from typing import Any

from .sensitivity import Sensitivity, advisory
from .vault import Doc, VaultLoader


def doc_to_hit(doc: Doc) -> dict[str, Any]:
    """Slim search/read projection of a :class:`Doc` (never includes the body)."""
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


def recent_changes(
    loader: VaultLoader,
    days: int = 7,
    limit: int = 50,
) -> dict[str, Any]:
    """Recent vault commits within the indexed folders (metadata only)."""
    days = max(1, min(int(days), 365))
    limit = max(1, min(int(limit), 500))

    cwd = str(loader.config.vault_path)
    try:
        proc = subprocess.run(
            [
                "git", "log",
                f"--since={days}.days.ago",
                f"-n{limit}",
                "--pretty=format:%H|%cI|%s",
                "--",
                *loader.config.indexed_dirs,
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
            commits.append(
                {"sha": parts[0], "committed_at": parts[1], "subject": parts[2]}
            )

    return {
        "days": days,
        "commit_count": len(commits),
        "commits": commits,
    }


def search_body(
    loader: VaultLoader,
    query: str,
    *,
    limit: int = 10,
    context_lines: int = 1,
    case_sensitive: bool = False,
) -> dict[str, Any]:
    """Full-text search across indexed doc bodies via ripgrep.

    Skips matches inside frontmatter blocks and always excludes restricted
    (secret-adjacent) bodies, mirroring the ``read_doc`` sensitivity gate.
    """
    query = (query or "").strip()
    if not query:
        return {"query": query, "hits_by_doc": [], "match_count": 0}

    rg = shutil.which("rg")
    if not rg:
        return {
            "error": (
                "ripgrep (`rg`) not found on PATH. Install via "
                "`brew install ripgrep` or skip this tool."
            ),
        }

    idx = loader.index()
    vault_root = loader.config.vault_path.resolve()
    indexed_roots = [vault_root / sub for sub in loader.config.indexed_dirs]
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
        return {
            "error": proc.stderr.strip() or f"ripgrep returncode={proc.returncode}"
        }

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
            **doc_to_hit(doc),
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


__all__ = ["doc_to_hit", "recent_changes", "search_body"]
