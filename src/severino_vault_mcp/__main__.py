"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

from .config import Config
from .doctor import run_doctor


def _fingerprint() -> str:
    """Stable hash of this (installed) package's Python sources.

    `site doctor` computes the same hash over the source repo and compares,
    so a stale `uv tool` install is caught even when the version was never
    bumped. Keep the hashing scheme in sync with cmd_doctor in the tools
    repo's bin/site.
    """
    package_dir = Path(__file__).resolve().parent
    digest = hashlib.sha256()
    for source in sorted(package_dir.glob("*.py")):
        digest.update(source.name.encode())
        digest.update(b"\0")
        digest.update(source.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()[:16]


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="severino-vault-mcp",
        description="Local stdio MCP server for Obsidian-style operations vaults.",
    )
    parser.add_argument(
        "--fingerprint",
        action="store_true",
        help=(
            "Print a hash of the installed package's Python sources and exit. "
            "Compared against the source repo by `site doctor` to detect a "
            "stale install."
        ),
    )
    subparsers = parser.add_subparsers(dest="command")

    doctor = subparsers.add_parser(
        "doctor",
        help="Validate configured vault frontmatter without starting the MCP server.",
    )
    doctor.add_argument(
        "--propose",
        action="store_true",
        help="Print starter frontmatter for markdown files that are missing it.",
    )

    prepare_publish = subparsers.add_parser(
        "prepare-writeup-publish",
        help=(
            "Run prepare_writeup_publish for a writeup slug and print JSON. "
            "Exits 0 if ok, 1 if blockers / missing slugs / unresolved refs. "
            "Wrapped by `site publish-writeup` in the operator's shell tooling."
        ),
    )
    prepare_publish.add_argument(
        "slug",
        help="Writeup slug, e.g. building-a-custom-mcp-layer.",
    )
    prepare_publish.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )
    prepare_publish.add_argument(
        "--include-tag-usage",
        action="store_true",
        help="Include per-technology usage stats in the response.",
    )

    list_writeups = subparsers.add_parser(
        "list-writeups",
        help=(
            "Run list_writeups for a filter and print JSON. The featured "
            "filter sorts by featured_order ascending — the order the home "
            "cloud renders. Wrapped by `site featured` in the operator's "
            "shell tooling."
        ),
    )
    list_writeups.add_argument(
        "--filter",
        default="all",
        choices=["all", "published", "draft", "featured"],
        help="Which writeups to list (default: all).",
    )
    list_writeups.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    technology_catalog = subparsers.add_parser(
        "technology-catalog",
        help=(
            "Run get_technology_catalog and print JSON: every slug, label, "
            "and featured flag grouped by section. Wrapped by `site tech` "
            "in the operator's shell tooling."
        ),
    )
    technology_catalog.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    validate_all = subparsers.add_parser(
        "validate-all-writeups",
        help=(
            "Run validate_all_writeups and print JSON. Exits 0 only when "
            "every (published, by default) writeup passes the gate. Wrapped "
            "by `site publish` as the slug-free pre-flight."
        ),
    )
    validate_all.add_argument(
        "--include-drafts",
        action="store_true",
        help="Validate published: false writeups too (default: published only).",
    )
    validate_all.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    reorder = subparsers.add_parser(
        "reorder-featured",
        help=(
            "Run reorder_featured: move a writeup to a 1-indexed featured "
            "slot (0 unfeatures it) and renumber the list sequential 1..N. "
            "Wrapped by `site featured <slug> <slot>`."
        ),
    )
    reorder.add_argument("slug", help="Writeup slug to move.")
    reorder.add_argument(
        "position",
        type=int,
        help="Target slot (1-indexed), or 0 to unfeature.",
    )
    reorder.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    update_writeup = subparsers.add_parser(
        "update-writeup",
        help=(
            "Update scalar writeup frontmatter fields via "
            "update_writeup_frontmatter and print JSON. Omitted flags leave "
            "fields unchanged. Wrapped by `site manage`."
        ),
    )
    update_writeup.add_argument("slug", help="Writeup slug to update.")
    update_writeup.add_argument("--title", default=None)
    update_writeup.add_argument("--description", default=None)
    update_writeup.add_argument("--published", default=None, choices=["true", "false"])
    update_writeup.add_argument("--published-at", default=None)
    update_writeup.add_argument("--last-reviewed", default=None)
    update_writeup.add_argument("--touch-last-reviewed", action="store_true")
    update_writeup.add_argument("--cover-image", default=None)
    update_writeup.add_argument("--cover-alt", default=None)
    update_writeup.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    touch_reviewed = subparsers.add_parser(
        "touch-reviewed",
        help=(
            "Set last_reviewed to today on a vault doc via update_frontmatter "
            "and print JSON. Exits 0 if ok, 1 otherwise. Wrapped by the drift "
            "guards (cf-dns / adguard / nginx / ts-acl) after a successful "
            "pull — a pull is a review, so the date moves."
        ),
    )
    touch_reviewed.add_argument(
        "relative_path",
        help=(
            "Vault-relative path, e.g. "
            "'02 Infrastructure/AdGuard/DNS Rewrites — homelab.md'."
        ),
    )
    touch_reviewed.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    args = parser.parse_args()
    if args.fingerprint:
        print(_fingerprint())
        raise SystemExit(0)

    if args.command == "doctor":
        raise SystemExit(run_doctor(Config.from_env(), propose=args.propose))

    if args.command == "prepare-writeup-publish":
        from .server import prepare_writeup_publish
        result = prepare_writeup_publish(args.slug, include_tag_usage=args.include_tag_usage)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "list-writeups":
        from .server import list_writeups as list_writeups_tool
        result = list_writeups_tool(args.filter)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "technology-catalog":
        from .server import get_technology_catalog
        result = get_technology_catalog()
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "validate-all-writeups":
        from .server import validate_all_writeups
        result = validate_all_writeups(only_published=not args.include_drafts)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "reorder-featured":
        from .server import reorder_featured
        result = reorder_featured(args.slug, args.position)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "update-writeup":
        from .server import update_writeup_frontmatter
        result = update_writeup_frontmatter(
            args.slug,
            title=args.title,
            description=args.description,
            published=None if args.published is None else args.published == "true",
            published_at=args.published_at,
            last_reviewed=args.last_reviewed,
            touch_last_reviewed=args.touch_last_reviewed,
            cover_image=args.cover_image,
            cover_alt=args.cover_alt,
        )
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "touch-reviewed":
        from .server import update_frontmatter
        result = update_frontmatter(args.relative_path, touch_last_reviewed=True)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    from .server import run

    run()


if __name__ == "__main__":
    main()
