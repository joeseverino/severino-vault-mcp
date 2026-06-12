"""Canonical frontmatter schema constants.

This module is the single source of truth for the vault frontmatter contract —
the enum sets and prefixes that decide what counts as a valid indexed doc. The
MCP validates writes against it, and Severino HQ consumes the same definition
(emitted as JSON by ``severino-vault-mcp schema --json``, committed to HQ and
checked against this module) so the two systems cannot drift on what `hq sync`
will accept. Edit the sets here and nowhere else.
"""

from __future__ import annotations

import re

DOC_TYPES = {
    "runbook", "architecture_note", "deployment_guide",
    "troubleshooting_guide", "recovery_procedure",
    "public_article_draft", "decision_record",
}
ENVIRONMENTS = {
    "homelab", "vps", "wordpress", "cloudflare", "tailscale",
    "adguard", "unifi", "local_mac", "other",
}
STATUSES = {"draft", "active", "deprecated", "archived"}
SENSITIVITIES = {"public", "internal", "sensitive", "restricted"}
DOC_ID_PREFIXES = ("rb-", "infra-", "report-", "project-", "note-")
REQUIRED_FIELDS = (
    "doc_id", "title", "doc_type", "system", "environment", "status", "sensitivity",
)


def as_dict() -> dict[str, list[str]]:
    """Return the schema as a deterministic, JSON-serializable dict.

    Set-valued fields are sorted so the emitted JSON is byte-stable across runs;
    ordered fields (prefixes, required fields) keep their declared order. This
    is the exact shape HQ commits and validates against.
    """
    return {
        "doc_types": sorted(DOC_TYPES),
        "environments": sorted(ENVIRONMENTS),
        "statuses": sorted(STATUSES),
        "sensitivities": sorted(SENSITIVITIES),
        "doc_id_prefixes": list(DOC_ID_PREFIXES),
        "required_fields": list(REQUIRED_FIELDS),
    }


_DOC_ENUM_FIELDS = {
    "doc_type": DOC_TYPES,
    "environment": ENVIRONMENTS,
    "status": STATUSES,
    "sensitivity": SENSITIVITIES,
}


def check_doc_enums(text: str) -> list[str]:
    """Compare a human schema doc's enum lines to the canonical sets.

    The vault's ``Frontmatter Schema.md`` documents each enum as a pipe-separated
    line (``doc_type: a | b | c``). This finds the first such list per field and
    reports any divergence from the canonical sets, so the human doc cannot
    silently drift from ``schema.py``. Returns a list of human-readable
    mismatches; empty means the doc is current.
    """
    pattern = re.compile(
        r"^\s*(doc_type|environment|status|sensitivity)\s*:\s*(.+?)\s*$"
    )
    found: dict[str, set[str]] = {}
    for line in text.splitlines():
        match = pattern.match(line)
        if not match:
            continue
        field, rhs = match.group(1), match.group(2)
        rhs = rhs.split("#", 1)[0]  # strip trailing comment
        tokens = {token.strip() for token in rhs.split("|") if token.strip()}
        if len(tokens) <= 1:
            continue  # a single example value, not the enum list
        found.setdefault(field, tokens)

    mismatches: list[str] = []
    for field, canonical in _DOC_ENUM_FIELDS.items():
        documented = found.get(field)
        if documented is None:
            mismatches.append(f"{field}: no enum list found in the doc")
            continue
        if documented != canonical:
            parts = []
            if extra := sorted(documented - canonical):
                parts.append(f"doc lists unknown {extra}")
            if missing := sorted(canonical - documented):
                parts.append(f"doc is missing {missing}")
            mismatches.append(f"{field}: " + "; ".join(parts))
    return mismatches
