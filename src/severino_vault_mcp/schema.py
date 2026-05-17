"""Shared frontmatter schema constants."""

DOC_TYPES = {
    "runbook", "architecture_note", "deployment_guide",
    "troubleshooting_guide", "recovery_procedure",
    "public_article_draft", "decision_record",
}
ENVIRONMENTS = {
    "lab", "homelab", "vps", "wordpress", "cloudflare", "tailscale",
    "adguard", "unifi", "local_mac", "other",
}
STATUSES = {"draft", "active", "deprecated", "archived"}
SENSITIVITIES = {
    "public", "internal", "sensitive", "restricted",
    # Backwards-compatible aliases accepted by doctor/write tools.
    "secret_adjacent", "credential_adjacent", "confidential",
}
DOC_ID_PREFIXES = ("rb-", "infra-", "report-", "project-", "note-")
REQUIRED_FIELDS = (
    "doc_id", "title", "doc_type", "system", "environment", "status", "sensitivity",
)
