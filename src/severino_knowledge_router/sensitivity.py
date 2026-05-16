"""Sensitivity policy gate.

The vault's frontmatter schema labels every doc with one of four sensitivities.
This module encodes the contract for what an AI assistant is allowed to see:

    public          — anything (full body)
    internal        — anything (full body)
    sensitive       — metadata only (title, doc_id, system, path, tags); body withheld
    secret_adjacent — refuse with a pointer; metadata only

The MCP host (Claude, etc.) gets a structured response either way, but for
sensitive/secret_adjacent the body is replaced with a short policy note so
secrets cannot end up in an LLM context window.
"""

from __future__ import annotations

from enum import StrEnum


class Sensitivity(StrEnum):
    PUBLIC = "public"
    INTERNAL = "internal"
    SENSITIVE = "sensitive"
    SECRET_ADJACENT = "secret_adjacent"

    @classmethod
    def parse(cls, value: str | None) -> Sensitivity:
        if not value:
            return cls.SENSITIVE  # conservative default if missing
        try:
            return cls(value.strip().lower())
        except ValueError:
            return cls.SENSITIVE


def body_is_releasable(sensitivity: Sensitivity) -> bool:
    """Public + internal docs can have their full body returned."""
    return sensitivity in (Sensitivity.PUBLIC, Sensitivity.INTERNAL)


def policy_note(sensitivity: Sensitivity) -> str:
    """Human-readable explanation of why the body was withheld."""
    if sensitivity is Sensitivity.SENSITIVE:
        return (
            "Body withheld by policy (sensitivity=sensitive). "
            "Read it directly in the Obsidian vault."
        )
    if sensitivity is Sensitivity.SECRET_ADJACENT:
        return (
            "Body withheld by policy (sensitivity=secret_adjacent). "
            "This doc is adjacent to credentials/keys — do not request the "
            "body via AI tools. Open it in Obsidian on the Mac."
        )
    return ""
