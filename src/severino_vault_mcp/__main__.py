"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import hashlib
import sys
from pathlib import Path

from . import jsonio
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
        _emit(result, pretty=args.pretty)

    if args.command == "validate-writeup":
        from .writeup_service import WriteupRuntime, validate_writeup

        result = validate_writeup(
            WriteupRuntime.from_env(), args.slug, draft=args.draft
        )
        _emit(result, pretty=args.pretty)

    if args.command == "list-writeups":
        from .writeup_service import WriteupRuntime, list_writeups

        result = list_writeups(WriteupRuntime.from_env(), args.filter)
        _emit(result, pretty=args.pretty)

    if args.command == "technology-catalog":
        from .writeup_service import WriteupRuntime, get_technology_catalog

        result = get_technology_catalog(WriteupRuntime.from_env())
        _emit(result, pretty=args.pretty)

    if args.command == "validate-all-writeups":
        from .writeup_service import WriteupRuntime, validate_all_writeups

        result = validate_all_writeups(
            WriteupRuntime.from_env(),
            only_published=not args.include_drafts,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "writeup-dashboard":
        from .writeup_service import WriteupRuntime, writeup_dashboard

        result = writeup_dashboard(WriteupRuntime.from_env())
        _emit(result, pretty=args.pretty)

    if args.command == "apply-writeup-plan":
        from .writeup_service import WriteupRuntime, apply_writeup_plan

        try:
            plan = jsonio.loads(sys.stdin.read(), source="writeup plan")
        except jsonio.JsonError as exc:
            result = {"ok": False, "error": str(exc)}
        else:
            result = apply_writeup_plan(WriteupRuntime.from_env(), plan)
        _emit(result, pretty=args.pretty)

    if args.command == "reorder-featured":
        from .writeup_service import WriteupRuntime, reorder_featured

        result = reorder_featured(
            WriteupRuntime.from_env(),
            args.slug,
            args.position,
        )
        _emit(result, pretty=args.pretty)

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
        _emit(result, pretty=args.pretty)

    if args.command == "touch-reviewed":
        from .config import Config
        from .vault import VaultLoader
        from .vault_write_service import touch_reviewed

        result = touch_reviewed(VaultLoader(Config.from_env()), args.relative_path)
        _emit(result, pretty=args.pretty)

    if args.command == "backfill-aliases":
        from .config import Config
        from .vault import VaultLoader
        from .vault_write_service import backfill_aliases

        result = backfill_aliases(VaultLoader(Config.from_env()))
        _emit(result, pretty=args.pretty)

    if args.command == "find":
        from .config import Config
        from .vault import VaultLoader
        from .vault_search_service import find_sections

        result = {
            "ok": True,
            **find_sections(VaultLoader(Config.from_env()), args.query, limit=args.limit),
        }
        _emit(result, pretty=args.pretty)

    if args.command == "read":
        from .config import Config
        from .vault import VaultLoader
        from .vault_search_service import read_section

        result = read_section(
            VaultLoader(Config.from_env()), args.doc_id, args.section
        )
        _emit(result, pretty=args.pretty)

    if args.command == "describe":
        from .cli_introspect import describe_parser

        # cordon's emitter returns the full {ok, schema_version, ...} document.
        result = describe_parser(parser)
        _emit(result, pretty=args.pretty)

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

        # canonical(): sorted + indented so the committed HQ copy is a stable diff.
        print(jsonio.canonical(as_dict()))
        raise SystemExit(0)

    if args.command == "hq-manifest":
        from .hq_manifest import build_hq_manifest

        result = build_hq_manifest(
            Path(args.vault).expanduser(),
            [part for part in args.subdirs.split(":") if part],
        )
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
        from .brief_service import vault_brief
        from .config import Config
        from .vault import VaultLoader

        result = vault_brief(
            VaultLoader(Config.from_env()),
            days=args.days,
            review_after_days=args.review_after,
            recent_limit=args.limit,
        )
        _emit(result, pretty=args.pretty)

    if args.command == "topology":
        import datetime

        from . import infra_datasets
        from . import topology as topo_mod
        from .config import Config

        config = Config.from_env()
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

        # tables / doc / figure load the inventory directly.
        try:
            topo = topo_mod.load_topology(config.topology_path)
        except topo_mod.TopologyError as exc:
            _emit({"ok": False, "error": str(exc)}, pretty=args.pretty)

        if args.emit == "tables":
            print(topo_mod.render_tables(topo, references))
        elif args.emit == "doc":
            today = datetime.date.today().isoformat()
            print(topo_mod.render_doc(topo, last_reviewed=today, references=references))
        elif args.emit == "figure":
            print(jsonio.dumps(topo_mod.render_figure(topo), pretty=args.pretty))
        raise SystemExit(0)

    if args.command == "infra":
        from . import infra_datasets
        from .config import Config

        config = Config.from_env()
        if args.dataset_id:
            result = infra_datasets.read_dataset(
                config, args.dataset_id, refresh=args.refresh
            )
        else:
            result = infra_datasets.list_datasets(config)
        _emit(result, pretty=args.pretty)

    if args.command == "infra-write":
        from . import infra_datasets
        from .config import Config

        result = infra_datasets.write_dataset(
            Config.from_env(), args.dataset_id, sys.stdin.read()
        )
        _emit(result, pretty=args.pretty)

    if args.command == "topology-write":
        from . import topology as topo_mod
        from .config import Config

        payload = sys.stdin.read() if args.replace else None
        result = topo_mod.write_topology(Config.from_env(), payload)
        _emit(result, pretty=args.pretty)

    from .server import run

    run()


if __name__ == "__main__":
    main()
