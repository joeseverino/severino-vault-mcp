"""CLI dispatch parity guard.

The CLI surface is declared in two places: ``cli.py`` registers one
``subparsers.add_parser("<name>")`` per subcommand, and ``__main__.py`` routes
each with an ``if args.command == "<name>":`` arm. Nothing enforced that the two
agreed, so a *duplicate* dispatch arm could ship a dead second branch (the
``backfill-aliases`` arm that shadowed the real one with stale code), and an
orphan in either direction would error only at runtime.

This is the Python sibling of the tools repo's spec<->dispatch parity test: parse
both files and assert there are no duplicate arms, no duplicate subparser ids,
and that the two command sets are exactly equal. Pure AST, no imports of the CLI,
so it can't be fooled by import-time side effects.
"""

import ast
from pathlib import Path

import pytest

SRC = Path(__file__).resolve().parent.parent / "src" / "severino_vault_mcp"


def _dispatch_commands() -> list[str]:
    """Every ``args.command == "<str>"`` comparison in __main__.py, in order."""
    tree = ast.parse((SRC / "__main__.py").read_text())
    names: list[str] = []
    for node in ast.walk(tree):
        if not (isinstance(node, ast.Compare) and len(node.ops) == 1
                and isinstance(node.ops[0], ast.Eq)):
            continue
        left = node.left
        if (isinstance(left, ast.Attribute) and left.attr == "command"
                and isinstance(left.value, ast.Name) and left.value.id == "args"):
            comp = node.comparators[0]
            if isinstance(comp, ast.Constant) and isinstance(comp.value, str):
                names.append(comp.value)
    return names


def _subparser_ids() -> list[str]:
    """Every ``*.add_parser("<str>")`` id registered in cli.py, in order."""
    tree = ast.parse((SRC / "cli.py").read_text())
    ids: list[str] = []
    for node in ast.walk(tree):
        if (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
                and node.func.attr == "add_parser" and node.args
                and isinstance(node.args[0], ast.Constant)
                and isinstance(node.args[0].value, str)):
            ids.append(node.args[0].value)
    return ids


def _duplicates(seq: list[str]) -> set[str]:
    seen: set[str] = set()
    dups: set[str] = set()
    for item in seq:
        (dups if item in seen else seen).add(item)
    return dups


def test_no_duplicate_dispatch_arms() -> None:
    arms = _dispatch_commands()
    assert arms, "no dispatch arms parsed — the extractor or __main__.py shape changed"
    dups = _duplicates(arms)
    assert not dups, f"duplicate `args.command ==` dispatch arms (dead branches): {sorted(dups)}"


def test_no_duplicate_subparser_ids() -> None:
    ids = _subparser_ids()
    assert ids, "no subparsers parsed — the extractor or cli.py shape changed"
    dups = _duplicates(ids)
    assert not dups, f"duplicate add_parser ids: {sorted(dups)}"


def test_dispatch_matches_subparsers() -> None:
    arms = set(_dispatch_commands())
    ids = set(_subparser_ids())
    missing_dispatch = ids - arms
    orphan_dispatch = arms - ids
    assert arms == ids, (
        "CLI dispatch and subparsers disagree — "
        f"subcommands with no dispatch arm: {sorted(missing_dispatch)}; "
        f"dispatch arms with no subcommand: {sorted(orphan_dispatch)}"
    )


if __name__ == "__main__":
    raise SystemExit(pytest.main([__file__, "-q"]))
