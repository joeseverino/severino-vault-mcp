"""Emit the repo's own command surface as structured data.

The "Code/guards" leg of emit-once, render-many (see the vault decision record
`report-emit-once-render-many` and `docs/federated-retrieval.md`): the argparse
parser in `__main__.py` *is* the command surface, so we walk it rather than
restate it in prose. One emitter, three consumers — an AI session reads the JSON
token-minimally instead of parsing `AGENTS.md`, a TUI renders it as a command
picker, and a guard can diff it. It can't drift from `--help` because it is
generated from the same parser that produces `--help`.

Pure and FastMCP-free: :func:`describe_parser` takes an
:class:`argparse.ArgumentParser` and returns a JSON-serializable dict. It reads
argparse internals (`_actions`, `_SubParsersAction`, `_choices_actions`) — the
stable, widely-used way to introspect a parser without re-declaring its shape.
"""

from __future__ import annotations

import argparse
from typing import Any

# Contract level shared with the tools repo's lib/describe.sh. This emitter is a
# subset of that contract (no paras/examples/delegates) but carries the same
# schema_version and the v3 `effect` blast-radius field, so a federated consumer
# (`tools describe --repos`) reads one uniform shape across repos.
SCHEMA_VERSION = 3


def _describe_arg(action: argparse.Action) -> dict[str, Any]:
    """One argument/option as a structured entry."""
    positional = not action.option_strings
    entry: dict[str, Any] = {
        "name": action.option_strings[0] if action.option_strings else action.dest,
        "positional": positional,
        # A positional is always required; an option is required only if declared so.
        "required": True if positional else bool(action.required),
        "help": action.help or "",
    }
    if action.option_strings:
        entry["flags"] = list(action.option_strings)
    # store_true / store_false are boolean switches that take no value.
    if isinstance(action, argparse._StoreTrueAction | argparse._StoreFalseAction):
        entry["takes_value"] = False
    if action.choices:
        entry["choices"] = list(action.choices)
    if action.type is not None:
        entry["type"] = getattr(action.type, "__name__", str(action.type))
    if action.default is not None:
        entry["default"] = action.default
    return entry


def describe_parser(parser: argparse.ArgumentParser) -> dict[str, Any]:
    """Structured self-description of a parser: name, global options, commands.

    Returns ``{name, description, global_options, commands}`` where every
    command carries its one-line summary and its arguments. The caller wraps
    this in the ``{ok: True, ...}`` envelope.
    """
    subparsers_action = next(
        (a for a in parser._actions if isinstance(a, argparse._SubParsersAction)),
        None,
    )

    global_options = [
        _describe_arg(a)
        for a in parser._actions
        if a.option_strings
        and not isinstance(a, argparse._HelpAction)
        and not isinstance(a, argparse._SubParsersAction)
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
                    "effect": getattr(subparser, "_svmc_effect", "read"),
                    "args": args,
                }
            )

    return {
        "schema_version": SCHEMA_VERSION,
        "name": parser.prog,
        "description": parser.description or "",
        # Tool-level blast radius: the entry point itself only dispatches; the
        # per-command effects above carry the real signal.
        "effect": "read",
        "global_options": global_options,
        "commands": commands,
    }


__all__ = ["describe_parser"]
