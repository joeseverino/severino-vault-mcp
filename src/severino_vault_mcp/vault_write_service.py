"""Schema-aware generic vault frontmatter mutations.

Every writer here resolves through :func:`paths.validate_indexed_path`, renders
through :func:`frontmatter.serialize_frontmatter`, and reports failures as
``{"ok": False, "error": "<message>"}`` so the MCP, the ``site`` CLI, and
``site manage`` all parse one envelope.
"""

from __future__ import annotations

from datetime import date
from typing import Any

from .atomic_write import atomic_write_text
from .frontmatter import serialize_frontmatter, split_frontmatter
from .paths import validate_indexed_path
from .schema import (
    DOC_ID_PREFIXES,
    DOC_TYPES,
    ENVIRONMENTS,
    SENSITIVITIES,
    STATUSES,
)
from .vault import VaultLoader


def add_frontmatter(
    loader: VaultLoader,
    relative_path: str,
    doc_id: str,
    title: str,
    doc_type: str,
    system: str,
    *,
    environment: str = "other",
    status: str = "active",
    sensitivity: str = "internal",
    tags: list[str] | None = None,
    related_projects: list[str] | None = None,
    related_assets: list[str] | None = None,
    last_reviewed: str | None = None,
) -> dict[str, Any]:
    errors: list[str] = []
    if doc_type not in DOC_TYPES:
        errors.append(f"doc_type {doc_type!r} not in {sorted(DOC_TYPES)}")
    if environment not in ENVIRONMENTS:
        errors.append(
            f"environment {environment!r} not in {sorted(ENVIRONMENTS)}"
        )
    if status not in STATUSES:
        errors.append(f"status {status!r} not in {sorted(STATUSES)}")
    if sensitivity not in SENSITIVITIES:
        errors.append(
            f"sensitivity {sensitivity!r} not in {sorted(SENSITIVITIES)}"
        )
    if not doc_id.startswith(DOC_ID_PREFIXES):
        errors.append(
            f"doc_id {doc_id!r} must start with one of "
            f"{list(DOC_ID_PREFIXES)}"
        )
    if errors:
        return {"ok": False, "error": "; ".join(errors)}

    full_path, path_error = validate_indexed_path(loader.config, relative_path)
    if path_error:
        return path_error
    assert full_path is not None
    body = full_path.read_text(encoding="utf-8")
    if body.lstrip().startswith("---"):
        return {
            "ok": False,
            "error": (
                "file already starts with `---` (existing frontmatter); "
                "use `update_frontmatter` instead."
            ),
        }

    index = loader.index(force=True)
    if doc_id in index.duplicate_doc_ids:
        return {
            "ok": False,
            "error": (
                f"doc_id {doc_id!r} is already duplicated at "
                f"{index.duplicate_doc_ids[doc_id]}"
            ),
        }
    if doc_id in index.by_doc_id:
        return {
            "ok": False,
            "error": (
                f"doc_id {doc_id!r} already exists at "
                f"{index.by_doc_id[doc_id].relative_path}"
            ),
        }

    payload = {
        "doc_id": doc_id,
        "title": title,
        "doc_type": doc_type,
        "system": system,
        "environment": environment,
        "status": status,
        "sensitivity": sensitivity,
        "last_reviewed": last_reviewed or date.today().isoformat(),
        "related_projects": [
            str(project) for project in (related_projects or [])
        ],
        "related_assets": [str(asset) for asset in (related_assets or [])],
        "tags": [str(tag) for tag in (tags or [])],
    }
    new_body = serialize_frontmatter(payload) + body
    try:
        atomic_write_text(full_path, new_body)
    except OSError as exc:
        return {"ok": False, "error": f"write failed: {exc}"}
    loader.index(force=True)
    return {
        "ok": True,
        "doc_id": doc_id,
        "relative_path": str(full_path.relative_to(loader.config.vault_path)),
        "wrote_bytes": len(new_body.encode("utf-8")),
        "next_step": (
            "run any downstream vault metadata sync if your workflow uses one"
        ),
    }


def _apply_list_op(
    current: list[str],
    set_to: list[str] | None,
    add: list[str] | None,
    remove: list[str] | None,
) -> list[str]:
    if set_to is not None:
        return [str(value) for value in set_to]
    output = list(current)
    if remove:
        removed = {str(value) for value in remove}
        output = [value for value in output if value not in removed]
    if add:
        for value in add:
            text = str(value)
            if text not in output:
                output.append(text)
    return output


def update_frontmatter(
    loader: VaultLoader,
    relative_path: str,
    *,
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
    errors: list[str] = []
    if doc_type is not None and doc_type not in DOC_TYPES:
        errors.append(f"doc_type {doc_type!r} not in {sorted(DOC_TYPES)}")
    if environment is not None and environment not in ENVIRONMENTS:
        errors.append(
            f"environment {environment!r} not in {sorted(ENVIRONMENTS)}"
        )
    if status is not None and status not in STATUSES:
        errors.append(f"status {status!r} not in {sorted(STATUSES)}")
    if sensitivity is not None and sensitivity not in SENSITIVITIES:
        errors.append(
            f"sensitivity {sensitivity!r} not in {sorted(SENSITIVITIES)}"
        )
    if errors:
        return {"ok": False, "error": "; ".join(errors)}

    full_path, path_error = validate_indexed_path(loader.config, relative_path)
    if path_error:
        return path_error
    assert full_path is not None
    vault_root = loader.config.vault_path.resolve()
    text = full_path.read_text(encoding="utf-8")
    frontmatter, body, _body_start = split_frontmatter(text)
    if frontmatter is None:
        return {
            "ok": False,
            "error": "file has no frontmatter — call `add_frontmatter` instead.",
        }

    changed: dict[str, Any] = {}
    for key, value in (
        ("title", title),
        ("doc_type", doc_type),
        ("system", system),
        ("environment", environment),
        ("status", status),
        ("sensitivity", sensitivity),
    ):
        if value is not None and frontmatter.get(key) != value:
            frontmatter[key] = value
            changed[key] = value

    reviewed = (
        date.today().isoformat()
        if touch_last_reviewed
        else last_reviewed
    )
    if reviewed is not None and frontmatter.get("last_reviewed") != reviewed:
        frontmatter["last_reviewed"] = reviewed
        changed["last_reviewed"] = reviewed

    def update_list(field: str, set_value, add_value, remove_value) -> None:
        if (
            set_value is None
            and add_value is None
            and remove_value is None
        ):
            return
        current = frontmatter.get(field) or []
        if not isinstance(current, list):
            current = [str(current)]
        new_value = _apply_list_op(
            current,
            set_value,
            add_value,
            remove_value,
        )
        if new_value != current:
            frontmatter[field] = new_value
            changed[field] = new_value

    update_list("tags", set_tags, add_tags, remove_tags)
    update_list(
        "related_projects",
        set_related_projects,
        add_related_projects,
        remove_related_projects,
    )
    update_list(
        "related_assets",
        set_related_assets,
        add_related_assets,
        remove_related_assets,
    )

    if not changed:
        return {
            "ok": True,
            "no_op": True,
            "doc_id": frontmatter.get("doc_id"),
            "relative_path": str(full_path.relative_to(vault_root)),
            "message": "No fields differ — nothing written.",
        }
    try:
        atomic_write_text(full_path, serialize_frontmatter(frontmatter) + body)
    except OSError as exc:
        return {"ok": False, "error": f"write failed: {exc}"}
    loader.index(force=True)
    return {
        "ok": True,
        "doc_id": frontmatter.get("doc_id"),
        "relative_path": str(full_path.relative_to(vault_root)),
        "changed_fields": sorted(changed),
        "next_step": (
            "run any downstream vault metadata sync if your workflow uses one"
        ),
    }


def touch_reviewed(loader: VaultLoader, relative_path: str) -> dict[str, Any]:
    """Set one indexed vault doc's ``last_reviewed`` to today, skipping reindex.

    This is the hot path for the drift guards (cf-dns / adguard / nginx /
    ts-acl), which call it after every successful pull. It deliberately avoids
    the ``loader.index(force=True)`` rebuild that the general writers pay for,
    since the guards only need the file on disk updated.
    """
    full_path, path_error = validate_indexed_path(loader.config, relative_path)
    if path_error:
        return path_error
    assert full_path is not None
    vault_root = loader.config.vault_path.resolve()
    frontmatter, body, _body_start = split_frontmatter(
        full_path.read_text(encoding="utf-8")
    )
    if frontmatter is None:
        return {
            "ok": False,
            "error": "file has no frontmatter — call `add_frontmatter` instead.",
        }
    reviewed = date.today().isoformat()
    if frontmatter.get("last_reviewed") == reviewed:
        return {
            "ok": True,
            "no_op": True,
            "doc_id": frontmatter.get("doc_id"),
            "relative_path": str(full_path.relative_to(vault_root)),
            "message": "No fields differ — nothing written.",
        }
    frontmatter["last_reviewed"] = reviewed
    try:
        atomic_write_text(full_path, serialize_frontmatter(frontmatter) + body)
    except OSError as exc:
        return {"ok": False, "error": f"write failed: {exc}"}
    return {
        "ok": True,
        "doc_id": frontmatter.get("doc_id"),
        "relative_path": str(full_path.relative_to(vault_root)),
        "changed_fields": ["last_reviewed"],
        "next_step": (
            "run any downstream vault metadata sync if your workflow uses one"
        ),
    }


__all__ = [
    "add_frontmatter",
    "touch_reviewed",
    "update_frontmatter",
]


def backfill_aliases(loader: VaultLoader) -> dict[str, Any]:
    """Give every folder-note (``<folder>/index.md``) an Obsidian ``aliases``
    entry equal to its ``title``.

    Notes stored as ``index.md`` (projects, infra, reports that own an asset
    folder) can't be wikilinked or autocompleted by title in Obsidian, because
    their filename is the non-unique ``index``. Setting ``aliases: [title]`` makes
    ``[[Title]]`` resolve and autocomplete, with no path-qualified links to
    maintain. The alias is *derived* from ``title`` (emit-once), so this is
    idempotent and safe to re-run any time to repair drift; a new folder-note
    picks up its alias on the next run. Only indexed docs (those with valid
    frontmatter) are touched — writeups, which carry their own schema and are
    addressed by slug, are left alone. CLI-only, like the other maintenance
    fast paths.
    """
    idx = loader.index(force=True)
    vault_root = loader.config.vault_path.resolve()
    updated: list[str] = []
    skipped = 0
    for doc in idx.docs:
        if not doc.relative_path.endswith("/index.md"):
            continue
        full_path = vault_root / doc.relative_path
        frontmatter, body, _body_start = split_frontmatter(
            full_path.read_text(encoding="utf-8")
        )
        title = (frontmatter or {}).get("title")
        if not frontmatter or not title:
            skipped += 1
            continue
        title = str(title)
        existing = frontmatter.get("aliases") or []
        if not isinstance(existing, list):
            existing = [existing]
        if title in existing:
            skipped += 1
            continue
        # Title first, preserving any hand-added aliases after it.
        frontmatter["aliases"] = [title, *(a for a in existing if a != title)]
        try:
            atomic_write_text(
                full_path, serialize_frontmatter(frontmatter) + body
            )
        except OSError as exc:
            return {
                "ok": False,
                "error": f"write failed for {doc.relative_path}: {exc}",
            }
        updated.append(doc.relative_path)
    return {
        "ok": True,
        "updated": updated,
        "updated_count": len(updated),
        "skipped": skipped,
    }
