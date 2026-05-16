"""Sensitivity policy gate.

The MCP runs locally and is consumed by Joe's own Claude Code / Claude
Desktop session. The threat model is "don't surface live secrets in a chat
window where they could be copy-pasted or persisted in conversation logs,"
not "treat every operational runbook as forbidden knowledge."

So the gate is narrow:

    public          — body released
    internal        — body released
    sensitive       — body released + advisory note ("handle carefully")
    secret_adjacent — body withheld by default; caller can pass
                      `include_secret_adjacent=True` to read_doc to override,
                      and the response will mark that an override was used.

The HQ Markdown / JSON exports use a stricter rule (sensitive → title+path
only) — that's a separate consumer for AI-prep contexts.
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
            # Conservative default for missing labels — admin / arch docs
            # without an explicit sensitivity get treated as internal so
            # bodies are still returnable.
            return cls.INTERNAL
        try:
            return cls(value.strip().lower())
        except ValueError:
            return cls.INTERNAL


def body_is_releasable(sensitivity: Sensitivity, *, include_secret_adjacent: bool = False) -> bool:
    """Whether `read_doc` should include the body.

    Public / internal / sensitive: always yes. secret_adjacent only if the
    caller explicitly opted in.
    """
    if sensitivity is Sensitivity.SECRET_ADJACENT:
        return include_secret_adjacent
    return True


def advisory(sensitivity: Sensitivity, *, override_used: bool = False) -> str:
    """Free-text advisory string for the response.

    For sensitive docs: a short "handle carefully" reminder.
    For secret_adjacent docs:
      - default (no override): the refusal explanation
      - override=True: a strong reminder that the body is being released
    """
    if sensitivity is Sensitivity.SENSITIVE:
        return (
            "Doc is labeled `sensitive`. Body returned because the MCP runs "
            "locally, but treat this content as private — don't paste it into "
            "untrusted contexts."
        )
    if sensitivity is Sensitivity.SECRET_ADJACENT and not override_used:
        return (
            "Body withheld: sensitivity=secret_adjacent. This doc is adjacent "
            "to credentials/keys. Pass include_secret_adjacent=True to read_doc "
            "if you really need the body."
        )
    if sensitivity is Sensitivity.SECRET_ADJACENT and override_used:
        return (
            "Body released under explicit override (include_secret_adjacent=True). "
            "This doc is labeled secret_adjacent — be deliberate about what you "
            "do with the content."
        )
    return ""


# Backwards-compat alias — older code in this repo may still reference policy_note.
def policy_note(sensitivity: Sensitivity) -> str:
    return advisory(sensitivity)
