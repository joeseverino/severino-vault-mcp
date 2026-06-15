"""Emit the repo's command surface — bound to cordon's reference emitter.

The "Code/guards" leg of emit-once, render-many (see the vault decision record
`report-emit-once-render-many` and `docs/federated-retrieval.md`): the argparse
parser in `cli.build_parser` *is* the command surface, so we introspect it rather
than restate it in prose. One emitter, three consumers — an AI session reads the
JSON token-minimally instead of parsing `AGENTS.md`, a TUI renders it as a command
picker, and a guard can diff it. It can't drift from `--help` because it is
generated from the same parser that produces `--help`.

This is now a thin binding over **cordon's Python reference emitter**
(`cordon_emit`, the `cordon-emit` dependency) rather than a private copy: cordon
owns the algorithm and the schema, and we converge on its output, so
`tools describe --repos` folds this CLI in as a homogeneous sibling and validates
every member against the one `cordon-v4.json`. Per-command blast radius is
declared with `cordon_emit.set_effect` on each subparser in `cli.build_parser`;
the emitter reads it back here. All this module adds is this repo's inventory
coordinates (`group` / `order`).
"""

from __future__ import annotations

import argparse
from typing import Any

from cordon_emit import describe_parser as _emit

# This MCP's coordinates when folded into the federated surface. Uniqueness of
# `order` is enforced within a repo's own tools, not across siblings, so a fixed
# pair is correct: this repo presents as one sibling, not a tool list.
GROUP = "Vault MCP"
ORDER = 1


def describe_parser(parser: argparse.ArgumentParser) -> dict[str, Any]:
    """Project the CLI parser to a complete Cordon v4 document via cordon's emitter.

    Returns the full ``{ok, schema_version, ...}`` envelope. Tool-level effect is
    ``read`` (the entry point only dispatches); per-command effects come from the
    ``set_effect`` annotations in :func:`cli.build_parser`.
    """
    return _emit(parser, group=GROUP, order=ORDER)


__all__ = ["describe_parser"]
