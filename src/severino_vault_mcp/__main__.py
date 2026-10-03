"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import hashlib
import sys
from pathlib import Path

from vault_engine import jsonio
from vault_engine.context import GovernanceContext

from .cli import build_parser


def _fingerprint() -> str:
    """Stable hash of this package's Python sources.

    tools runs this same function for the installed copy and the source tree
    (`uv run --project . severino-vault-mcp --fingerprint`), so a stale
    `uv tool` install is caught even when the version was never bumped, and
    there is one implementation.
    """
    package_dir = Path(__file__).resolve().parent
    digest = hashlib.sha256()
    # Recursive and keyed on the package-relative path, so a change in any
    # subpackage changes the fingerprint.
    for source in sorted(package_dir.rglob("*.py")):
        if "__pycache__" in source.parts:
            continue
        digest.update(str(source.relative_to(package_dir)).encode())
        digest.update(b"\0")
        digest.update(source.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()[:16]


def _emit(result: dict, *, pretty: bool) -> None:
    """Print a service result as JSON and exit with its ok-derived status.

    The single emit path for every subcommand: the compact-vs-`--pretty`
    serialization lives in :mod:`jsonio`; this adds the ok→exit-code mapping.
    """
    print(jsonio.dumps(result, pretty=pretty))
    raise SystemExit(0 if result.get("ok") else 1)


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    if args.fingerprint:
        print(_fingerprint())
        raise SystemExit(0)

    # Subcommands act on the labs vault through the same governed context the
    # server uses.
    ctx = GovernanceContext.load()

    if args.command == "doctor":
        from vault_engine.doctor import run_doctor

        raise SystemExit(run_doctor(ctx.config, propose=args.propose))

    if args.command == "export":
        from . import education
        from .vaults import edu_context

        edu = edu_context()
        _emit(education.education_dataset(edu.loader, edu.profile.statuses), pretty=args.pretty)

    if args.command == "update-doc-link":
        from vault_engine.vault_write_service import update_document_link

        result = update_document_link(
            ctx.loader,
            args.doc_id,
            args.label,
            args.expected_href,
            args.replacement_href,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "touch-reviewed":
        from vault_engine.vault_write_service import touch_reviewed

        result = touch_reviewed(ctx.loader, args.relative_path)
        _emit(result, pretty=args.pretty)

    if args.command == "backfill-aliases":
        from vault_engine.vault_write_service import backfill_aliases

        result = backfill_aliases(ctx.loader)
        _emit(result, pretty=args.pretty)

    if args.command == "find":
        from vault_engine.vault_search_service import find_sections

        result = {
            "ok": True,
            **find_sections(ctx.loader, args.query, limit=args.limit),
        }
        _emit(result, pretty=args.pretty)

    if args.command == "read":
        from vault_engine.vault_search_service import read_section

        result = read_section(
            ctx.loader, args.doc_id, args.section
        )
        _emit(result, pretty=args.pretty)

    if args.command == "task-list":
        from vault_engine.task_service import list_tasks

        result = list_tasks(
            ctx.loader,
            status=args.status,
            project=args.project,
            stale_only=args.stale_only,
            include_all=args.include_all,
            stale_days=args.stale_days,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "promote-note":
        from vault_engine.task_service import promote_note

        result = promote_note(
            ctx.loader,
            args.source,
            title=args.title,
            project=args.project,
            effort=args.effort,
            priority=args.priority,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "update-frontmatter":
        from vault_engine.vault_write_service import update_frontmatter

        result = update_frontmatter(
            ctx.loader,
            args.relative_path,
            touch_last_reviewed=args.touch_last_reviewed,
            title=args.title,
            doc_type=args.doc_type,
            system=args.system,
            environment=args.environment,
            status=args.status,
            sensitivity=args.sensitivity,
            set_tags=args.set_tags,
            set_related_projects=args.set_related_projects,
            set_related_assets=args.set_related_assets,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "task-reconcile":
        from vault_engine.task_service import reconcile_tasks

        result = reconcile_tasks(ctx.loader)
        _emit(result, pretty=args.pretty)

    if args.command == "task-projects":
        from vault_engine.task_service import list_projects

        result = list_projects(ctx.loader)
        _emit(result, pretty=args.pretty)

    if args.command == "task-add":
        from vault_engine.task_service import add_task

        result = add_task(
            ctx.loader,
            title=args.title,
            project=args.project,
            related_projects=args.related_projects,
            effort=args.effort,
            priority=args.priority,
            tags=args.tags,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "task-move":
        from vault_engine.task_service import set_task_status

        result = set_task_status(
            ctx.loader, args.doc_id, args.status
        )
        _emit(result, pretty=args.pretty)

    if args.command == "task-delete":
        from vault_engine.task_service import delete_task

        result = delete_task(ctx.loader, args.doc_id)
        _emit(result, pretty=args.pretty)

    if args.command == "describe":
        from vault_engine.cli_introspect import describe_parser

        # cordon's emitter returns the full {ok, schema_version, ...} document.
        result = describe_parser(parser)
        _emit(result, pretty=args.pretty)

    if args.command == "schema":
        if args.check_doc:
            from vault_engine.schema import check_doc_enums

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

        from vault_engine.schema import LABS_PROFILE, as_dict

        if args.schema_fingerprint:
            print(LABS_PROFILE.fingerprint())
        elif args.contract:
            print(jsonio.canonical(LABS_PROFILE.contract_dict()))
        else:
            # Frozen legacy shape: HQ commits this exact output. New consumers
            # opt into --contract / --fingerprint instead of broadening it.
            print(jsonio.canonical(as_dict()))
        raise SystemExit(0)

    if args.command == "hq-manifest":
        from .labs.hq_manifest import build_hq_manifest, default_manifest_dirs

        if args.subdirs:
            subdirs = [part for part in args.subdirs.split(":") if part]
        else:

            subdirs = default_manifest_dirs(ctx.config.indexed_dirs)
        result = build_hq_manifest(Path(args.vault).expanduser(), subdirs)
        if args.report:
            # Full structured result for `hq doctor` — no entries dump.
            print(jsonio.dumps(result, pretty=True))
            raise SystemExit(0 if result.get("ok") else 1)
        if not result.get("ok"):
            print(jsonio.dumps(result, pretty=True), file=sys.stderr)
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
        print(jsonio.dumps(result["entries"], pretty=True))
        print(f"ok: {result['count']} entries", file=sys.stderr)
        raise SystemExit(0)

    if args.command == "brief":
        from vault_engine.brief_service import vault_brief

        result = vault_brief(
            ctx.loader,
            days=args.days,
            review_after_days=args.review_after,
            recent_limit=args.limit,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "daily-write":
        from vault_engine import daily_write

        result = daily_write.write_daily_block(
            ctx.config, sys.stdin.read(), note_date=args.date
        )
        _emit(result, pretty=args.pretty)

    from .server import run

    run()


if __name__ == "__main__":
    main()
