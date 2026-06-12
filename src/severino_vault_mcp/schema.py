"""Canonical frontmatter schema constants.

This module is the single source of truth for the vault frontmatter contract —
the enum sets and prefixes that decide what counts as a valid indexed doc. The
MCP validates writes against it, and Severino HQ consumes the same definition
(emitted as JSON by ``severino-vault-mcp schema --json``, committed to HQ and
checked against this module) so the two systems cannot drift on what `hq sync`
will accept. Edit the sets here and nowhere else.
"""

from __future__ import annotations

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
