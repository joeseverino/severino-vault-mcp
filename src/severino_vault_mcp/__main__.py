"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import hashlib
import sys
from pathlib import Path

from vault_engine import jsonio
from vault_engine.context import GovernanceContext

from .cli import build_parser


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

    # One governed runtime per invocation. Every CLI branch uses the same
    # config/profile/loader composition as the MCP adapter; neither face calls
    # the other transport or reconstructs vault policy ad hoc.
    ctx = GovernanceContext.load()

    if args.command == "doctor":
        from vault_engine.doctor import run_doctor

        raise SystemExit(run_doctor(ctx.config, propose=args.propose))

    if args.command == "prepare-writeup-publish":
        from .labs.writeup_service import WriteupRuntime, prepare_writeup_publish

        result = prepare_writeup_publish(
            WriteupRuntime.from_config(ctx.config, loader=ctx.loader),
            args.slug,
            include_tag_usage=args.include_tag_usage,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "validate-writeup":
        from .labs.writeup_service import WriteupRuntime, validate_writeup

        result = validate_writeup(
            WriteupRuntime.from_config(ctx.config, loader=ctx.loader), args.slug, draft=args.draft
        )
        _emit(result, pretty=args.pretty)

    if args.command == "list-writeups":
        from .labs.writeup_service import WriteupRuntime, list_writeups

        result = list_writeups(WriteupRuntime.from_config(ctx.config, loader=ctx.loader), args.filter)
        _emit(result, pretty=args.pretty)

    if args.command == "technology-catalog":
        from .labs.writeup_service import WriteupRuntime, get_technology_catalog

        result = get_technology_catalog(WriteupRuntime.from_config(ctx.config, loader=ctx.loader))
        _emit(result, pretty=args.pretty)

    if args.command == "validate-all-writeups":
        from .labs.writeup_service import WriteupRuntime, validate_all_writeups

        result = validate_all_writeups(
            WriteupRuntime.from_config(ctx.config, loader=ctx.loader),
            only_published=not args.include_drafts,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "writeup-dashboard":
        from .labs.writeup_service import WriteupRuntime, writeup_dashboard

        result = writeup_dashboard(WriteupRuntime.from_config(ctx.config, loader=ctx.loader))
        _emit(result, pretty=args.pretty)

    if args.command == "writeup-contract":
        from .contracts.site_content import public_contract

        _emit(public_contract(), pretty=args.pretty)

    if args.command == "apply-writeup-plan":
        from .labs.writeup_service import WriteupRuntime, apply_writeup_plan

        try:
            plan = jsonio.loads(sys.stdin.read(), source="writeup plan")
        except jsonio.JsonError as exc:
            result = {"ok": False, "error": str(exc)}
        else:
            result = apply_writeup_plan(
                WriteupRuntime.from_config(ctx.config, loader=ctx.loader), plan
            )
        _emit(result, pretty=args.pretty)

    if args.command == "reorder-featured":
        from .labs.writeup_service import WriteupRuntime, reorder_featured

        result = reorder_featured(
            WriteupRuntime.from_config(ctx.config, loader=ctx.loader),
            args.slug,
            args.position,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "update-writeup":
        from .contracts.site_content import cli_fields
        from .labs.writeup_service import (
            WriteupRuntime,
            update_writeup_frontmatter,
        )

        updates = {
            name: (
                getattr(args, name) == "true"
                if spec.get("type") == "boolean"
                else getattr(args, name)
            )
            for name, spec in cli_fields()
            if getattr(args, name) is not None
        }
        result = update_writeup_frontmatter(
            WriteupRuntime.from_config(ctx.config, loader=ctx.loader),
            args.slug,
            **updates,
            touch_last_reviewed=args.touch_last_reviewed,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "update-writeup-link":
        from .labs.writeup_service import WriteupRuntime, update_writeup_link

        result = update_writeup_link(
            WriteupRuntime.from_config(ctx.config, loader=ctx.loader),
            args.slug,
            args.label,
            args.expected_href,
            args.replacement_href,
        )
        _emit(result, pretty=args.pretty)

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

    if args.command == "topology":
        import datetime

        from .labs import infra_datasets
        from .labs import topology as topo_mod

        config = ctx.config
        # Reflected pointer list comes from the one registry, not topology.json.
        references = tuple(infra_datasets.reflected_references(config))

        if args.check_doc:
            try:
                topo = topo_mod.load_topology(config.topology_path)
            except topo_mod.TopologyError as exc:
                print(f"topology: {exc}", file=sys.stderr)
                raise SystemExit(1) from exc
            text = Path(args.check_doc).expanduser().read_text(
                encoding="utf-8", errors="replace"
            )
            mismatches = topo_mod.check_doc(topo, text, references)
            if mismatches:
                print(f"topology doc drift in {args.check_doc}:", file=sys.stderr)
                for mismatch in mismatches:
                    print(f"  - {mismatch}", file=sys.stderr)
                raise SystemExit(1)
            print(f"ok: {args.check_doc} matches the topology inventory")
            raise SystemExit(0)

        if args.emit == "schema":
            # The declared inventory contract — static, no inventory load. The
            # canonical form so HQ can commit and validate against it, exactly
            # like `schema --json`.
            print(jsonio.canonical(topo_mod.inventory_schema()))
            raise SystemExit(0)

        if args.emit == "summary":
            _emit(topo_mod.get_topology(config), pretty=args.pretty)

        # inventory / tables / doc / figure load the inventory directly.
        try:
            topo = topo_mod.load_topology(config.topology_path)
        except topo_mod.TopologyError as exc:
            _emit({"ok": False, "error": str(exc)}, pretty=args.pretty)

        if args.emit == "inventory":
            print(jsonio.dumps(topo.raw, pretty=args.pretty))
        elif args.emit == "tables":
            print(topo_mod.render_tables(topo, references))
        elif args.emit == "doc":
            today = datetime.date.today().isoformat()
            print(topo_mod.render_doc(topo, last_reviewed=today, references=references))
        elif args.emit == "figure":
            print(jsonio.dumps(topo_mod.render_figure(topo), pretty=args.pretty))
        raise SystemExit(0)

    if args.command == "infra":

        from .labs import infra_datasets

        config = ctx.config
        if args.dataset_id:
            result = infra_datasets.read_dataset(
                config, args.dataset_id, refresh=args.refresh
            )
        else:
            result = infra_datasets.list_datasets(config)
        _emit(result, pretty=args.pretty)

    if args.command == "infra-write":

        from .labs import infra_datasets

        result = infra_datasets.write_dataset(
            ctx.config, args.dataset_id, sys.stdin.read()
        )
        _emit(result, pretty=args.pretty)

    if args.command == "daily-write":
        from vault_engine import daily_write

        result = daily_write.write_daily_block(
            ctx.config, sys.stdin.read(), note_date=args.date
        )
        _emit(result, pretty=args.pretty)

    if args.command == "topology-write":

        from .labs import topology as topo_mod

        payload = sys.stdin.read() if args.replace else None
        result = topo_mod.write_topology(ctx.config, payload)
        _emit(result, pretty=args.pretty)

    from .server import run

    run()


if __name__ == "__main__":
    main()
