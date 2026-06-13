"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path


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

    dashboard = subparsers.add_parser(
        "writeup-dashboard",
        help=(
            "Return every writeup summary and validation result from one "
            "shared vault snapshot. Used by `site manage` for fast startup."
        ),
    )
    dashboard.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    apply_plan = subparsers.add_parser(
        "apply-writeup-plan",
        help=(
            "Read a JSON writeup mutation plan from stdin and apply all "
            "scalar updates plus the complete featured order transactionally."
        ),
    )
    apply_plan.add_argument(
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

    update_mirror = subparsers.add_parser(
        "update-mirror-block",
        help=(
            "Replace the fenced ```json mirror block under a heading in a "
            "vault doc with JSON read from stdin — section-scoped, one atomic "
            "write, optionally stamping last_reviewed in the same write. "
            "Wrapped by the drift guards' `pull` (cf-dns / adguard / ts-acl)."
        ),
    )
    update_mirror.add_argument(
        "relative_path",
        help=(
            "Vault-relative path, e.g. "
            "'02 Infrastructure/AdGuard/DNS Rewrites — homelab.md'."
        ),
    )
    update_mirror.add_argument(
        "--heading",
        required=True,
        help=(
            "Section heading whose ```json block is the mirror, "
            "e.g. '## DNS Rewrites'."
        ),
    )
    update_mirror.add_argument(
        "--touch-reviewed",
        action="store_true",
        help="Also set last_reviewed to today in the same atomic write.",
    )
    update_mirror.add_argument(
        "--pretty",
        action="store_true",
        help="Pretty-print JSON with indentation (default: compact).",
    )

    hq_manifest = subparsers.add_parser(
        "hq-manifest",
        help=(
            "Build the Severino HQ manifest with the package's shared "
            "frontmatter parser."
        ),
    )
    hq_manifest.add_argument("vault", help="Vault root path.")
    hq_manifest.add_argument(
        "subdirs",
        help="Colon-separated vault subdirectories to index.",
    )
    hq_manifest.add_argument(
        "--report",
        action="store_true",
        help=(
            "Print the full result (missing_frontmatter, duplicates, counts) "
            "as JSON instead of the manifest entries. Backs `hq doctor`."
        ),
    )

    schema_cmd = subparsers.add_parser(
        "schema",
        help=(
            "Emit the canonical frontmatter schema (enum sets) as JSON. "
            "Severino HQ commits this output and validates against it so the "
            "two systems share one definition."
        ),
    )
    schema_cmd.add_argument(
        "--json",
        action="store_true",
        help="Emit the schema as JSON (the default).",
    )
    schema_cmd.add_argument(
        "--check-doc",
        metavar="PATH",
        help=(
            "Instead of emitting, verify that a human schema doc's enum lines "
            "(doc_type/environment/status/sensitivity) match the canonical "
            "schema. Exit 1 and print mismatches on drift."
        ),
    )

    args = parser.parse_args()
    if args.fingerprint:
        print(_fingerprint())
        raise SystemExit(0)

    if args.command == "doctor":
        from .config import Config
        from .doctor import run_doctor

        raise SystemExit(run_doctor(Config.from_env(), propose=args.propose))

    if args.command == "prepare-writeup-publish":
        from .writeup_service import WriteupRuntime, prepare_writeup_publish

        result = prepare_writeup_publish(
            WriteupRuntime.from_env(),
            args.slug,
            include_tag_usage=args.include_tag_usage,
        )
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "list-writeups":
        from .writeup_service import WriteupRuntime, list_writeups

        result = list_writeups(WriteupRuntime.from_env(), args.filter)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "technology-catalog":
        from .writeup_service import WriteupRuntime, get_technology_catalog

        result = get_technology_catalog(WriteupRuntime.from_env())
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "validate-all-writeups":
        from .writeup_service import WriteupRuntime, validate_all_writeups

        result = validate_all_writeups(
            WriteupRuntime.from_env(),
            only_published=not args.include_drafts,
        )
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "writeup-dashboard":
        from .writeup_service import WriteupRuntime, writeup_dashboard

        result = writeup_dashboard(WriteupRuntime.from_env())
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "apply-writeup-plan":
        from .writeup_service import WriteupRuntime, apply_writeup_plan

        try:
            plan = json.load(sys.stdin)
        except json.JSONDecodeError as exc:
            result = {"ok": False, "error": f"invalid JSON plan: {exc}"}
        else:
            result = apply_writeup_plan(WriteupRuntime.from_env(), plan)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "reorder-featured":
        from .writeup_service import WriteupRuntime, reorder_featured

        result = reorder_featured(
            WriteupRuntime.from_env(),
            args.slug,
            args.position,
        )
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "update-writeup":
        from .writeup_service import (
            WriteupRuntime,
            update_writeup_frontmatter,
        )

        result = update_writeup_frontmatter(
            WriteupRuntime.from_env(),
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
        from .config import Config
        from .vault import VaultLoader
        from .vault_write_service import touch_reviewed

        result = touch_reviewed(VaultLoader(Config.from_env()), args.relative_path)
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "update-mirror-block":
        from .config import Config
        from .vault import VaultLoader
        from .vault_write_service import update_mirror_block

        result = update_mirror_block(
            VaultLoader(Config.from_env()),
            args.relative_path,
            args.heading,
            sys.stdin.read(),
            touch_reviewed=args.touch_reviewed,
        )
        if args.pretty:
            print(json.dumps(result, indent=2))
        else:
            print(json.dumps(result, separators=(",", ":")))
        raise SystemExit(0 if result.get("ok") else 1)

    if args.command == "schema":
        if args.check_doc:
            from .schema import check_doc_enums

            text = Path(args.check_doc).expanduser().read_text(
                encoding="utf-8", errors="replace"
            )
            mismatches = check_doc_enums(text)
            if mismatches:
                print(f"schema doc drift in {args.check_doc}:", file=sys.stderr)
                for mismatch in mismatches:
                    print(f"  - {mismatch}", file=sys.stderr)
                raise SystemExit(1)
            print(f"ok: {args.check_doc} matches the canonical schema")
            raise SystemExit(0)

        from .schema import as_dict

        # Sorted keys + indent so the committed HQ copy is a stable diff.
        print(json.dumps(as_dict(), indent=2, sort_keys=True))
        raise SystemExit(0)

    if args.command == "hq-manifest":
        from .hq_manifest import build_hq_manifest

        result = build_hq_manifest(
            Path(args.vault).expanduser(),
            [part for part in args.subdirs.split(":") if part],
        )
        if args.report:
            # Full structured result for `hq doctor` — no entries dump.
            print(json.dumps(result, indent=2))
            raise SystemExit(0 if result.get("ok") else 1)
        if not result.get("ok"):
            print(json.dumps(result, indent=2), file=sys.stderr)
            raise SystemExit(1)
        for subdir in result["missing_dirs"]:
            print(f"warn: {subdir} not under vault, skipping", file=sys.stderr)
        missing = result["missing_frontmatter"]
        if missing:
            print(
                f"warn: {len(missing)} file(s) missing frontmatter "
                "(skipped) — run `hq doctor` to list them",
                file=sys.stderr,
            )
        print(json.dumps(result["entries"], indent=2))
        print(f"ok: {result['count']} entries", file=sys.stderr)
        raise SystemExit(0)

    from .server import run

    run()


if __name__ == "__main__":
    main()
