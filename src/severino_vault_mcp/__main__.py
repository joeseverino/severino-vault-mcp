"""Entry point: `python -m severino_vault_mcp` and the console script."""

from __future__ import annotations

import argparse

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

    args = parser.parse_args()
    if args.command == "doctor":
        raise SystemExit(run_doctor(Config.from_env(), propose=args.propose))

    from .server import run

    run()


if __name__ == "__main__":
    main()
