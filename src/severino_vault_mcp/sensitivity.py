"""Sensitivity policy gate.

The MCP runs locally and is consumed by the operator's own Claude Code / Claude
Desktop session. The threat model is "don't surface live secrets in a chat
window where they could be copy-pasted or persisted in conversation logs,"
not "treat every operational runbook as forbidden knowledge."

So the gate is narrow:

    public          — body released
    internal        — body released
    sensitive  — body released + advisory note ("handle carefully")
    restricted — body withheld by default. `read_doc` can request a one-shot
                 local unlock, but this module never treats a caller flag
                 alone as sufficient authorization.

Downstream exports may choose stricter rules, such as returning only title and
path for sensitive docs. That policy belongs to those consumers, not this
local MCP read path.
"""

from __future__ import annotations

from enum import StrEnum


class Sensitivity(StrEnum):
    PUBLIC = "public"
    INTERNAL = "internal"
    SENSITIVE = "sensitive"
    RESTRICTED = "restricted"
    # Backwards-compatible enum alias for existing code and older vaults.
    SECRET_ADJACENT = "restricted"

    @classmethod
    def parse(cls, value: str | None) -> Sensitivity:
        if not value:
            # Conservative default for missing labels — admin / arch docs
            # without an explicit sensitivity get treated as internal so
            # bodies are still returnable.
            return cls.INTERNAL
        normalized = value.strip().lower()
        aliases = {
            "secret_adjacent": cls.RESTRICTED,
            "credential_adjacent": cls.RESTRICTED,
            "confidential": cls.SENSITIVE,
        }
        if normalized in aliases:
            return aliases[normalized]
        try:
            return cls(normalized)
        except ValueError:
            return cls.INTERNAL


def body_is_releasable(sensitivity: Sensitivity, *, include_secret_adjacent: bool = False) -> bool:
    """Whether `read_doc` should include the body.

    Public / internal / sensitive: always yes. restricted is never
    released by this generic helper; `read_doc` handles the local unlock gate.
    """
    return sensitivity is not Sensitivity.RESTRICTED


def advisory(sensitivity: Sensitivity, *, override_used: bool = False) -> str:
    """Free-text advisory string for the response.

    For sensitive docs: a short "handle carefully" reminder.
    For restricted docs:
      - default (no override): the refusal explanation
      - override=True: a strong reminder that the body is being released
    """
    if sensitivity is Sensitivity.SENSITIVE:
        return (
            "Doc is labeled `sensitive`. Body returned because the MCP runs "
            "locally, but treat this content as private — don't paste it into "
            "untrusted contexts."
        )
    if sensitivity is Sensitivity.RESTRICTED and not override_used:
        return (
            "Body withheld: sensitivity=restricted. This doc is near "
            "credentials, keys, or recovery paths. To request release, rerun read_doc with "
            "include_restricted=True; the local MCP will require an "
            "interactive unlock on the Mac."
        )
    if sensitivity is Sensitivity.RESTRICTED and override_used:
        return (
            "Body released after explicit request plus local interactive unlock. "
            "This doc is labeled restricted — be deliberate about what you "
            "do with the content."
        )
    return ""


# Backwards-compat alias — older code in this repo may still reference policy_note.
def policy_note(sensitivity: Sensitivity) -> str:
    return advisory(sensitivity)
