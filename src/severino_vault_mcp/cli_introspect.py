"""Emit the repo's own command surface as structured data.

The "Code/guards" leg of emit-once, render-many (see the vault decision record
`report-emit-once-render-many` and `docs/federated-retrieval.md`): the argparse
parser in `__main__.py` *is* the command surface, so we walk it rather than
restate it in prose. One emitter, three consumers — an AI session reads the JSON
token-minimally instead of parsing `AGENTS.md`, a TUI renders it as a command
picker, and a guard can diff it. It can't drift from `--help` because it is
generated from the same parser that produces `--help`.

This emits the EXACT shape the tools repo's `lib/describe.sh` does, validated by
its `schemas/describe-v4.schema.json`: so `tools describe --repos` folds this in
as a sibling and the federated document is homogeneous — one schema across both
repos, no second contract to drift. Fields the MCP CLI has no concept of
(tool-level prose, per-command prose/examples) are emitted as empty arrays to
satisfy the shared schema rather than diverging from it. The schema is owned by
the tools repo and enforced where federation happens (`tools check` validates the
`--repos` document), so this stays the single producer and that stays the single
validator — the same MCP↔HQ split used for the frontmatter `schema.json`.

Pure and FastMCP-free: :func:`describe_parser` takes an
:class:`argparse.ArgumentParser` and returns a JSON-serializable dict. It reads
argparse internals (`_actions`, `_SubParsersAction`, `_choices_actions`) — the
stable, widely-used way to introspect a parser without re-declaring its shape.
"""

from __future__ import annotations

import argparse
from typing import Any

# Contract level shared verbatim with the tools repo's lib/describe.sh and its
# schemas/describe-v4.schema.json. Bump only in lockstep with that schema.
SCHEMA_VERSION = 4

# The MCP's own coordinates when folded into the federated surface. Uniqueness of
# `order` is enforced only within a repo's own tools, not across siblings, so a
# fixed pair is correct: this repo presents as one sibling, not a tool list.
GROUP = "Vault MCP"
ORDER = 1

# argparse nargs values that make a positional non-required / variadic.
_OPTIONAL_NARGS = {"?", "*", argparse.REMAINDER}
_VARIADIC_NARGS = {"*", "+", argparse.REMAINDER}


def _describe_arg(action: argparse.Action) -> dict[str, Any]:
    """One argument/option as a v4 contract entry.

    Emits only the keys the shared schema allows (it is
    ``additionalProperties: false``): name, positional, required, help, and —
    for options — flags / takes_value / metavar; choices / variadic ride along
    when present. No argparse-only extras (type, default) leak in, so the entry
    is byte-identical in shape to what `desc_opt` / `desc_pos` produce.
    """
    positional = not action.option_strings
    entry: dict[str, Any] = {
        "name": action.option_strings[0] if action.option_strings else action.dest,
        "positional": positional,
        # Positionals carry their real requiredness; the shared schema models an
        # option as always-optional (required is const false there), so options
        # pin False regardless of argparse's `required=`.
        "required": (action.nargs not in _OPTIONAL_NARGS) if positional else False,
        "help": action.help or "",
    }
    if positional:
        if action.nargs in _VARIADIC_NARGS:
            entry["variadic"] = True
    else:
        entry["flags"] = list(action.option_strings)
        # store_true / store_false are switches that consume no value.
        takes_value = not isinstance(
            action, argparse._StoreTrueAction | argparse._StoreFalseAction
        )
        entry["takes_value"] = takes_value
        if takes_value and action.metavar:
            entry["metavar"] = action.metavar
    if action.choices:
        entry["choices"] = list(action.choices)
    return entry


def describe_parser(parser: argparse.ArgumentParser) -> dict[str, Any]:
    """Structured self-description of a parser, in the shared v4 contract shape.

    Returns the full document body (the caller wraps it in the ``{ok: True, ...}``
    envelope): tool-level metadata, global options/positionals, and every command
    with its arguments and declared blast-radius effect.
    """
    subparsers_action = next(
        (a for a in parser._actions if isinstance(a, argparse._SubParsersAction)),
        None,
    )

    global_options = [
        _describe_arg(a)
        for a in parser._actions
        if a.option_strings and not isinstance(a, argparse._HelpAction)
    ]
    positionals = [
        _describe_arg(a)
        for a in parser._actions
        if not a.option_strings and not isinstance(a, argparse._SubParsersAction)
    ]

    commands: list[dict[str, Any]] = []
    if subparsers_action is not None:
        summaries = {
            choice.dest: (choice.help or "")
            for choice in subparsers_action._choices_actions
        }
        for name, subparser in subparsers_action.choices.items():
            args = [
                _describe_arg(action)
                for action in subparser._actions
                if not isinstance(action, argparse._HelpAction)
            ]
            commands.append(
                {
                    "name": name,
                    "summary": summaries.get(name, ""),
                    "args": args,
                    # The structured risk signal: vault writers declare
                    # vault_write on the subparser; readers default to read.
                    "effect": getattr(subparser, "_svmc_effect", "read"),
                    # The MCP CLI has no per-command prose/examples; emit empty
                    # arrays so the shape matches the shared schema exactly.
                    "paras": [],
                    "examples": [],
                }
            )

    return {
        "schema_version": SCHEMA_VERSION,
        "name": parser.prog,
        "description": parser.description or "",
        "group": GROUP,
        "order": ORDER,
        # Tool-level blast radius: the entry point itself only dispatches; the
        # per-command effects above carry the real signal.
        "effect": "read",
        "global_options": global_options,
        "positionals": positionals,
        "paras": [],
        "examples": [],
        "commands": commands,
    }


__all__ = ["describe_parser"]
