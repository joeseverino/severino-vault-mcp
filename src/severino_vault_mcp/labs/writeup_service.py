"""Reusable writeup operations for MCP tools and the standalone CLI.

This module deliberately has no FastMCP dependency. CLI commands import it
directly so short-lived shell workflows do not pay MCP server registration
costs. A single :class:`WriteupContext` snapshots writeups, the technology
catalog, and vault references for composite validation calls.
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from datetime import date
from pathlib import Path
from typing import Any

from ..atomic_write import transactional_replace
from ..config import Config
from ..frontmatter import yaml_escape
from ..paths import path_within_root
from ..vault import VaultLoader
from .tech_groups import TechSlug, load_technology_catalog
from .writeups import Writeup, extract_body_image_refs, load_writeups

WRITEUP_FILTERS = ("all", "published", "draft", "featured")
WRITEUP_SCALAR_FIELDS = {
    "title",
    "description",
    "published",
    "published_at",
    "last_reviewed",
    "cover_image",
    "cover_alt",
}


@dataclass(frozen=True)
class WriteupRuntime:
    config: Config
    loader: VaultLoader
    writeups_dir: Path
    technology_catalog: Path

    @classmethod
    def from_env(cls) -> WriteupRuntime:
        config = Config.from_env()
        return cls.from_config(config)

    @classmethod
    def from_config(
        cls,
        config: Config,
        *,
        loader: VaultLoader | None = None,
    ) -> WriteupRuntime:
        writeups_dir = Path(
            os.path.expanduser(
                os.environ.get(
                    "SVMC_JSEVERINO_WRITEUPS_DIR",
                    str(config.vault_path / "05 Writeups"),
                )
            )
        )
        technology_catalog = Path(
            os.path.expanduser(
                os.environ.get(
                    "SVMC_JSEVERINO_TECH_GROUPS",
                    str(config.vault_path / "06 Pages" / "_technology-groups.md"),
                )
            )
        )
        return cls(
            config=config,
            loader=loader or VaultLoader(config),
            writeups_dir=writeups_dir,
            technology_catalog=technology_catalog,
        )

    @property
    def vault_root(self) -> Path:
        return self.config.vault_path.resolve()

    def path_error(self, path: Path, label: str, kind: str) -> dict[str, Any] | None:
        return path_within_root(self.config.vault_path, path, label, kind)


@dataclass(frozen=True)
class WriteupContext:
    runtime: WriteupRuntime
    writeups: tuple[Writeup, ...]
    by_slug: dict[str, Writeup]
    catalog: tuple[TechSlug, ...]
    catalog_slugs: frozenset[str]
    project_doc_ids: frozenset[str]
    doc_stems_lower: frozenset[str]
    catalog_error: str | None = None

    @classmethod
    def load(cls, runtime: WriteupRuntime) -> WriteupContext:
        writeups = tuple(load_writeups(runtime.writeups_dir))
        catalog_error: str | None = None
        catalog: tuple[TechSlug, ...] = ()
        if err := runtime.path_error(
            runtime.technology_catalog,
            "technology catalog",
            "file",
        ):
            catalog_error = str(err["error"])
        else:
            catalog = tuple(load_technology_catalog(runtime.technology_catalog))
            if not catalog:
                catalog_error = (
                    f"no technology slugs parsed from "
                    f"{runtime.technology_catalog}"
                )

        vault_idx = runtime.loader.index()
        return cls(
            runtime=runtime,
            writeups=writeups,
            by_slug={writeup.slug: writeup for writeup in writeups},
            catalog=catalog,
            catalog_slugs=frozenset(entry.slug for entry in catalog),
            project_doc_ids=frozenset(doc.doc_id for doc in vault_idx.docs),
            doc_stems_lower=frozenset(doc.path.stem.lower() for doc in vault_idx.docs),
            catalog_error=catalog_error,
        )


def _writeup_order_entry(
    writeup: Writeup,
    slot: int | None = None,
) -> dict[str, Any]:
    return {
        "slot": slot if slot is not None else writeup.featured_order,
        "slug": writeup.slug,
        "title": writeup.title,
        "published": writeup.published,
        "featured": writeup.featured,
    }


def _featured_writeup_order(writeups: tuple[Writeup, ...] | list[Writeup]) -> list[dict[str, Any]]:
    return [
        _writeup_order_entry(writeup, slot=slot)
        for slot, writeup in enumerate(
            sorted(
                (w for w in writeups if w.published and w.featured),
                key=lambda w: (
                    w.featured_order if w.featured_order is not None else 10**9,
                    w.slug,
                ),
            ),
            start=1,
        )
    ]


def list_featured_writeup_order(runtime: WriteupRuntime) -> dict[str, Any]:
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    order = _featured_writeup_order(load_writeups(runtime.writeups_dir))
    return {
        "ok": True,
        "writeups_dir": str(runtime.writeups_dir),
        "count": len(order),
        "order": order,
    }


def list_writeups(
    runtime: WriteupRuntime,
    filter: str = "all",
    *,
    context: WriteupContext | None = None,
) -> dict[str, Any]:
    chosen = (filter or "all").strip().lower()
    if chosen not in WRITEUP_FILTERS:
        return {
            "ok": False,
            "error": f"unknown filter {filter!r}; expected one of {list(WRITEUP_FILTERS)}",
        }
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err

    all_writeups = list(context.writeups if context else load_writeups(runtime.writeups_dir))
    selected = list(all_writeups)
    if chosen == "published":
        selected = [w for w in selected if w.published]
    elif chosen == "draft":
        selected = [w for w in selected if not w.published]
    elif chosen == "featured":
        selected = [w for w in selected if w.featured]
        selected.sort(
            key=lambda w: (
                w.featured_order if w.featured_order is not None else 10**9,
                w.slug,
            )
        )

    return {
        "ok": True,
        "filter": chosen,
        "writeups_dir": str(runtime.writeups_dir),
        "count": len(selected),
        "order": [
            _writeup_order_entry(writeup, slot=slot)
            for slot, writeup in enumerate(selected, start=1)
        ],
        "featured_order": _featured_writeup_order(all_writeups),
        "writeups": [writeup.to_summary() for writeup in selected],
    }


def get_technology_catalog(
    runtime: WriteupRuntime,
    *,
    context: WriteupContext | None = None,
) -> dict[str, Any]:
    if context is not None:
        # Reuse the catalog already snapshotted by the composite flow rather
        # than re-reading the markdown a second time.
        if context.catalog_error:
            return {"ok": False, "error": context.catalog_error}
        catalog = context.catalog
    else:
        if err := runtime.path_error(
            runtime.technology_catalog,
            "technology catalog",
            "file",
        ):
            return err
        catalog = load_technology_catalog(runtime.technology_catalog)
        if not catalog:
            return {
                "ok": False,
                "error": (
                    f"catalog file present but no slugs parsed: "
                    f"{runtime.technology_catalog}"
                ),
            }
    by_group: dict[str, list[dict[str, Any]]] = {}
    for entry in catalog:
        by_group.setdefault(entry.group, []).append(
            {
                "slug": entry.slug,
                "label": entry.label,
                "featured": entry.featured,
            }
        )
    return {
        "ok": True,
        "catalog_path": str(runtime.technology_catalog),
        "total_slugs": len(catalog),
        "featured_count": sum(1 for entry in catalog if entry.featured),
        "by_group": by_group,
    }


def find_writeups_using_tag(
    runtime: WriteupRuntime,
    slug: str,
    *,
    context: WriteupContext | None = None,
) -> dict[str, Any]:
    slug = (slug or "").strip()
    if not slug:
        return {"ok": False, "error": "slug required"}
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    writeups = context.writeups if context else tuple(load_writeups(runtime.writeups_dir))
    matches = [writeup for writeup in writeups if slug in writeup.technologies]
    return {
        "ok": True,
        "slug": slug,
        "total_matches": len(matches),
        "published_matches": sum(1 for writeup in matches if writeup.published),
        "writeups": [
            {
                "slug": writeup.slug,
                "title": writeup.title,
                "published": writeup.published,
                "featured": writeup.featured,
            }
            for writeup in matches
        ],
    }


def _validate_loaded_writeup(
    context: WriteupContext,
    writeup: Writeup,
    *,
    draft: bool = False,
) -> dict[str, Any]:
    blockers: list[str] = []
    nits: list[str] = []
    if not writeup.title:
        blockers.append("title missing")
    if not writeup.description:
        blockers.append("description missing")
    elif len(writeup.description) > 300:
        nits.append(
            f"description is {len(writeup.description)} chars (recommend <=300)"
        )
    # Publish-state checks are hard blockers for shipping, but in draft mode
    # (mid-authoring) they are demoted to nits so a well-formed draft can pass.
    # This is the single definition of "draft tolerance"; the CLI subcommand and
    # the MCP tool both route through here.
    publish_state: list[str] = []
    if not writeup.published:
        publish_state.append("published is false — flip to true to ship")
    if not writeup.published_at:
        publish_state.append("published_at empty — set ISO date when ready")
    (nits if draft else blockers).extend(publish_state)
    if not writeup.cover_image:
        nits.append("cover_image missing")
    if writeup.cover_image and not writeup.cover_alt:
        nits.append(
            "cover_alt missing — describe the actual image so the card and "
            "hero stop reusing the title"
        )
    if not writeup.technologies:
        nits.append("technologies list empty")

    missing_slugs: list[str] = []
    if context.catalog_error:
        nits.append(f"{context.catalog_error}; skipping slug check")
    else:
        missing_slugs = [
            slug
            for slug in writeup.technologies
            if slug not in context.catalog_slugs
        ]

    images_dir = writeup.path.parent / "images"
    present_images = (
        {path.name for path in images_dir.iterdir() if path.is_file()}
        if images_dir.is_dir()
        else set()
    )
    missing_images = [
        ref
        for ref in extract_body_image_refs(writeup.body)
        if Path(ref).name and Path(ref).name not in present_images
    ]

    unresolved_refs: list[str] = []
    for ref in writeup.related_projects:
        if (
            f"project-{ref}" not in context.project_doc_ids
            and ref.lower() not in context.doc_stems_lower
        ):
            unresolved_refs.append(
                f"related_projects: {ref} (no matching vault doc)"
            )
    for ref in writeup.related_assets:
        if (
            f"project-{ref}" not in context.project_doc_ids
            and ref not in context.project_doc_ids
            and ref.lower() not in context.doc_stems_lower
        ):
            unresolved_refs.append(
                f"related_assets: {ref} (no matching vault doc)"
            )

    return {
        "ok": not blockers
        and not missing_slugs
        and not missing_images
        and not unresolved_refs,
        "slug": writeup.slug,
        "frontmatter": writeup.to_summary(),
        "blockers": blockers,
        "missing_tech_slugs": missing_slugs,
        "missing_images": missing_images,
        "unresolved_refs": unresolved_refs,
        "nits": nits,
    }


def validate_writeup(
    runtime: WriteupRuntime,
    slug: str,
    *,
    context: WriteupContext | None = None,
    draft: bool = False,
) -> dict[str, Any]:
    slug = (slug or "").strip()
    if not slug:
        return {"ok": False, "error": "slug required"}
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    writeup_dir = runtime.writeups_dir / slug
    if not writeup_dir.is_dir():
        return {"ok": False, "error": f"writeup folder not found: {slug}"}
    context = context or WriteupContext.load(runtime)
    writeup = context.by_slug.get(slug)
    if writeup is None:
        return {
            "ok": False,
            "error": f"writeup has no frontmatter or index.md: {slug}",
        }
    return _validate_loaded_writeup(context, writeup, draft=draft)


def validate_all_writeups(
    runtime: WriteupRuntime,
    only_published: bool = True,
    *,
    context: WriteupContext | None = None,
) -> dict[str, Any]:
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    context = context or WriteupContext.load(runtime)
    writeups = [
        writeup
        for writeup in context.writeups
        if writeup.published or not only_published
    ]
    results = [
        _validate_loaded_writeup(context, writeup)
        for writeup in writeups
    ]
    summaries = [
        {
            "slug": result["slug"],
            "ok": result["ok"],
            "blockers": result["blockers"],
            "missing_tech_slugs": result["missing_tech_slugs"],
            "missing_images": result["missing_images"],
            "unresolved_refs": result["unresolved_refs"],
            "nits": result["nits"],
        }
        for result in results
    ]
    failing_slugs = [result["slug"] for result in results if not result["ok"]]
    return {
        "ok": not failing_slugs,
        "count": len(summaries),
        "failing_count": len(failing_slugs),
        "failing_slugs": failing_slugs,
        "total_blockers": sum(len(result["blockers"]) for result in results),
        "total_nits": sum(len(result["nits"]) for result in results),
        "total_missing_tech_slugs": sum(
            len(result["missing_tech_slugs"]) for result in results
        ),
        "total_missing_images": sum(
            len(result["missing_images"]) for result in results
        ),
        "total_unresolved_refs": sum(
            len(result["unresolved_refs"]) for result in results
        ),
        "writeups": summaries,
    }


def prepare_writeup_publish(
    runtime: WriteupRuntime,
    slug: str,
    *,
    include_tag_usage: bool = False,
) -> dict[str, Any]:
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    context = WriteupContext.load(runtime)
    validation = validate_writeup(runtime, slug, context=context)
    featured_writeups = sorted(
        (writeup for writeup in context.writeups if writeup.featured),
        key=lambda writeup: (
            writeup.featured_order
            if writeup.featured_order is not None
            else 10**9,
            writeup.slug,
        ),
    )
    position = next(
        (
            writeup.featured_order
            for writeup in featured_writeups
            if writeup.slug == slug
        ),
        None,
    )
    response: dict[str, Any] = {
        "ok": bool(validation.get("ok")),
        "slug": slug,
        "validation": validation,
        "featured_set": {
            "count": len(featured_writeups),
            "order": [
                {
                    "slot": writeup.featured_order,
                    "slug": writeup.slug,
                }
                for writeup in featured_writeups
            ],
            "this_writeup_position": position,
        },
    }
    if include_tag_usage:
        technologies = validation.get("frontmatter", {}).get("technologies", [])
        response["tag_usage"] = {
            tag: {
                "total_writeups": usage["total_matches"],
                "published_writeups": usage["published_matches"],
            }
            for tag in technologies
            if (
                usage := find_writeups_using_tag(
                    runtime,
                    tag,
                    context=context,
                )
            ).get("ok")
        }
    return response


def writeup_dashboard(runtime: WriteupRuntime) -> dict[str, Any]:
    """Return all writeup summaries and validation results from one snapshot."""
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    context = WriteupContext.load(runtime)
    listing = list_writeups(runtime, "all", context=context)
    validation = validate_all_writeups(
        runtime,
        only_published=False,
        context=context,
    )
    return {
        "ok": True,
        "writeups_dir": str(runtime.writeups_dir),
        "writeups": listing["writeups"],
        "featured_order": listing["featured_order"],
        "validation": validation,
    }


def _yaml_writeup_scalar(value: Any) -> str:
    """Render one writeup scalar for in-place line replacement.

    Non-empty strings route through the shared :func:`frontmatter.yaml_escape`,
    so writeup writes quote exactly when generic vault writes do — no second
    escaping ruleset to drift. ``None`` and empty strings still render as a bare
    key (no value) to preserve the null-valued frontmatter the site build reads,
    rather than ``key: ""``.
    """
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    text = str(value)
    if text == "":
        return ""
    return yaml_escape(text)


def _replace_writeup_scalar(text: str, key: str, raw_value: str) -> str:
    pattern = re.compile(rf"^({re.escape(key)}):[^\n]*$", re.MULTILINE)
    replacement = f"{key}: {raw_value}".rstrip()
    if pattern.search(text):
        return pattern.sub(replacement, text, count=1)
    lines = text.split("\n")
    fence_count = 0
    for index, line in enumerate(lines):
        if line.strip() == "---":
            fence_count += 1
            if fence_count == 2:
                lines.insert(index, replacement)
                return "\n".join(lines)
    return replacement + "\n" + text


def _changed_writeup_text(
    writeup: Writeup,
    updates: dict[str, Any],
) -> tuple[str, list[str]]:
    current_values = writeup.to_summary()
    changed_fields = [
        key
        for key, value in updates.items()
        if current_values.get(key) != value
    ]
    if not changed_fields:
        return writeup.path.read_text(encoding="utf-8"), []
    text = writeup.path.read_text(encoding="utf-8")
    for key in changed_fields:
        text = _replace_writeup_scalar(
            text,
            key,
            _yaml_writeup_scalar(updates[key]),
        )
    return text, sorted(changed_fields)


def update_writeup_frontmatter(
    runtime: WriteupRuntime,
    slug: str,
    *,
    title: str | None = None,
    description: str | None = None,
    published: bool | None = None,
    published_at: str | None = None,
    last_reviewed: str | None = None,
    touch_last_reviewed: bool = False,
    cover_image: str | None = None,
    cover_alt: str | None = None,
) -> dict[str, Any]:
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    writeups = load_writeups(runtime.writeups_dir)
    writeup = next((item for item in writeups if item.slug == slug), None)
    if writeup is None:
        if not (runtime.writeups_dir / slug).is_dir():
            return {"ok": False, "error": f"writeup folder not found: {slug}"}
        return {
            "ok": False,
            "error": f"writeup has no frontmatter or index.md: {slug}",
        }
    values = {
        "title": title,
        "description": description,
        "published": published,
        "published_at": published_at,
        "last_reviewed": (
            date.today().isoformat()
            if touch_last_reviewed
            else last_reviewed
        ),
        "cover_image": cover_image,
        "cover_alt": cover_alt,
    }
    updates = {key: value for key, value in values.items() if value is not None}
    text, changed_fields = _changed_writeup_text(writeup, updates)
    if not changed_fields:
        return {
            "ok": True,
            "no_op": True,
            "slug": slug,
            "message": "No fields differ — nothing written.",
        }
    ok, error = transactional_replace(
        runtime.writeups_dir,
        {writeup.path: text},
    )
    if not ok:
        return {"ok": False, "error": error}
    return {
        "ok": True,
        "slug": slug,
        "relative_path": str(
            writeup.path.resolve().relative_to(runtime.vault_root)
        ),
        "changed_fields": changed_fields,
        "values": {
            key: (
                None
                if updates[key] is None
                else str(updates[key])
            )
            for key in changed_fields
        },
    }


def apply_writeup_plan(
    runtime: WriteupRuntime,
    plan: dict[str, Any],
) -> dict[str, Any]:
    """Apply scalar updates and a complete featured order in one transaction."""
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    if not isinstance(plan, dict):
        return {"ok": False, "error": "plan must be a JSON object"}
    raw_updates = plan.get("updates", [])
    raw_order = plan.get("featured_order")
    if not isinstance(raw_updates, list):
        return {"ok": False, "error": "updates must be a list"}
    if raw_order is not None and not isinstance(raw_order, list):
        return {"ok": False, "error": "featured_order must be a list"}

    writeups = tuple(load_writeups(runtime.writeups_dir))
    by_slug = {writeup.slug: writeup for writeup in writeups}
    updates_by_slug: dict[str, dict[str, Any]] = {}
    for item in raw_updates:
        if not isinstance(item, dict):
            return {"ok": False, "error": "every update must be an object"}
        slug = str(item.get("slug") or "").strip()
        if not slug or slug not in by_slug:
            return {"ok": False, "error": f"unknown writeup slug: {slug!r}"}
        unknown = set(item) - WRITEUP_SCALAR_FIELDS - {"slug"}
        if unknown:
            return {
                "ok": False,
                "error": (
                    f"unsupported fields for {slug}: "
                    f"{sorted(unknown)}"
                ),
            }
        updates_by_slug.setdefault(slug, {}).update(
            {
                key: value
                for key, value in item.items()
                if key != "slug"
            }
        )

    featured_order: list[str] | None = None
    if raw_order is not None:
        featured_order = [str(slug) for slug in raw_order]
        if len(featured_order) != len(set(featured_order)):
            return {"ok": False, "error": "featured_order contains duplicate slugs"}
        unknown_slugs = [
            slug
            for slug in featured_order
            if slug not in by_slug
        ]
        if unknown_slugs:
            return {
                "ok": False,
                "error": f"featured_order contains unknown slugs: {unknown_slugs}",
            }
        slots = {slug: slot for slot, slug in enumerate(featured_order, start=1)}
        for writeup in writeups:
            desired_featured = writeup.slug in slots
            desired_order = slots.get(writeup.slug)
            if (
                writeup.featured != desired_featured
                or writeup.featured_order != desired_order
            ):
                updates_by_slug.setdefault(writeup.slug, {}).update(
                    {
                        "featured": desired_featured,
                        "featured_order": desired_order,
                    }
                )

    replacements: dict[Path, str] = {}
    changed: dict[str, list[str]] = {}
    for slug, updates in updates_by_slug.items():
        writeup = by_slug[slug]
        text, changed_fields = _changed_writeup_text(writeup, updates)
        if changed_fields:
            replacements[writeup.path] = text
            changed[slug] = changed_fields

    if not replacements:
        return {
            "ok": True,
            "no_op": True,
            "changed_writeups": [],
            "featured_order_after": (
                featured_order
                if featured_order is not None
                else [
                    entry["slug"]
                    for entry in _featured_writeup_order(writeups)
                ]
            ),
        }

    ok, error = transactional_replace(runtime.writeups_dir, replacements)
    if not ok:
        return {
            "ok": False,
            "error": f"writeup transaction failed: {error}",
            "rolled_back": True,
        }
    return {
        "ok": True,
        "changed_writeups": sorted(changed),
        "changed_fields": changed,
        "featured_order_after": (
            featured_order
            if featured_order is not None
            else [
                entry["slug"]
                for entry in _featured_writeup_order(
                    load_writeups(runtime.writeups_dir)
                )
            ]
        ),
    }


def reorder_featured(
    runtime: WriteupRuntime,
    slug: str,
    position: int,
) -> dict[str, Any]:
    if not isinstance(position, int) or position < 0:
        return {"ok": False, "error": "position must be an integer >= 0"}
    if err := runtime.path_error(runtime.writeups_dir, "writeups dir", "dir"):
        return err
    writeups = tuple(load_writeups(runtime.writeups_dir))
    target = next((item for item in writeups if item.slug == slug), None)
    if target is None:
        if not (runtime.writeups_dir / slug).is_dir():
            return {"ok": False, "error": f"writeup folder not found: {slug}"}
        return {
            "ok": False,
            "error": f"writeup has no frontmatter or index.md: {slug}",
        }
    featured_now = sorted(
        (writeup for writeup in writeups if writeup.featured),
        key=lambda writeup: (
            writeup.featured_order
            if writeup.featured_order is not None
            else 10**9,
            writeup.slug,
        ),
    )
    others = [writeup.slug for writeup in featured_now if writeup.slug != slug]
    if position == 0:
        new_order = others
        new_position: int | None = None
    else:
        max_position = len(others) + 1
        if position > max_position:
            return {
                "ok": False,
                "error": f"position {position} out of range (max {max_position})",
            }
        new_order = others[: position - 1] + [slug] + others[position - 1 :]
        new_position = position
    result = apply_writeup_plan(
        runtime,
        {"updates": [], "featured_order": new_order},
    )
    if not result.get("ok"):
        return result
    return {
        "ok": True,
        "slug": slug,
        "new_position": new_position,
        "changed_writeups": result.get("changed_writeups", []),
        "featured_order_after": [
            {"slot": slot, "slug": ordered_slug}
            for slot, ordered_slug in enumerate(new_order, start=1)
        ],
    }


__all__ = [
    "WriteupContext",
    "WriteupRuntime",
    "apply_writeup_plan",
    "find_writeups_using_tag",
    "get_technology_catalog",
    "list_featured_writeup_order",
    "list_writeups",
    "prepare_writeup_publish",
    "reorder_featured",
    "update_writeup_frontmatter",
    "validate_all_writeups",
    "validate_writeup",
    "writeup_dashboard",
]
