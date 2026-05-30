"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import argparse
import json

from .config import Config
from .doctor import run_doctor


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="severino-vault-mcp",
        description="Local stdio MCP server for Obsidian-style operations vaults.",
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

    args = parser.parse_args()
    if args.command == "doctor":
        raise SystemExit(run_doctor(Config.from_env(), propose=args.propose))

    if args.command == "prepare-writeup-publish":
        from .server import prepare_writeup_publish
        result = prepare_writeup_publish(args.slug)
        print(json.dumps(result, indent=2))
        raise SystemExit(0 if result.get("ok") else 1)

    from .server import run

    run()


if __name__ == "__main__":
    main()
