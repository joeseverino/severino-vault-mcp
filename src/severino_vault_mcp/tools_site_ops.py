"""jseverino.com site-operations tools — a Labs-domain tool group.

Read-only wrappers around the operator's own Cloudflare D1 database, a fixed
additive schema apply, and a live security-header check. Registered onto a
server's FastMCP instance via :func:`register`, so a server that doesn't run
jseverino.com simply never composes this group. Deliberately not a generic
shell: each tool is a fixed workflow, not arbitrary SQL or fetch.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from . import site_ops_service
from .secret_unlock import audit_event


def register(mcp, *, site_ops, audit_log: Path) -> None:
    """Register the site-ops tool group on ``mcp``.

    Args:
        mcp: the FastMCP instance to register tools on.
        site_ops: the SiteOpsRuntime singleton the tools query.
        audit_log: path to the local audit log for PII-release events.
    """

    @mcp.tool()
    def list_contact_submissions(
        limit: int = 10,
        include_pii: bool = False,
    ) -> dict[str, Any]:
        """List recent jseverino.com contact form submissions from Cloudflare D1.

        This is a fixed, read-only wrapper around `wrangler d1 execute` for the
        operator's `jseverino-contact` database. It does not accept arbitrary SQL.

        By DEFAULT the response is redacted: names are abbreviated, emails are
        masked (`j***@domain`), and the message is returned as a preview plus a
        character count. Pass `include_pii=True` ONLY when the user explicitly needs
        full contact details — that releases names, full emails, message bodies, IP
        addresses, and user agents into the chat context and is recorded in the
        local audit log. Default to the redacted view.

        Args:
            limit: Maximum rows to return, capped at 100.
            include_pii: If True, return full contact PII instead of the redacted
                projection. Default False. Audited when used.
        """
        result = site_ops_service.list_contact_submissions(
            site_ops, limit, include_pii=include_pii
        )
        if include_pii and result.get("pii_released"):
            audit_event(
                audit_log,
                action="contact_pii_access",
                detail=f"rows={len(result.get('results', []))}",
            )
        return result

    @mcp.tool()
    def list_csp_reports(
        limit: int = 20,
        directive: str | None = None,
        include_pii: bool = False,
    ) -> dict[str, Any]:
        """List recent jseverino.com CSP violation reports from Cloudflare D1.

        Browser-extension/off-site noise is filtered by the report receiver before
        rows reach this table. This tool is read-only and does not accept arbitrary SQL.

        By default the client identifier fields (`ip_address`, `user_agent`,
        `raw_report`) are omitted. Pass `include_pii=True` only when the user needs
        them; that release is recorded in the local audit log.

        Args:
            limit: Maximum rows to return, capped at 100.
            directive: Optional exact `effective_directive` filter, e.g. "script-src".
            include_pii: If True, include the client identifier fields. Default
                False. Audited when used.
        """
        result = site_ops_service.list_csp_reports(
            site_ops, limit, directive, include_pii=include_pii
        )
        if include_pii and result.get("pii_released"):
            audit_event(
                audit_log,
                action="csp_pii_access",
                detail=f"rows={len(result.get('results', []))}",
            )
        return result

    @mcp.tool()
    def count_csp_reports() -> dict[str, Any]:
        """Return CSP report counts for jseverino.com from Cloudflare D1.

        Includes total count and grouped counts by `effective_directive`.
        """
        return site_ops_service.count_csp_reports(site_ops)

    @mcp.tool()
    def apply_jseverino_d1_schema(confirm: bool = False) -> dict[str, Any]:
        """Apply `db/schema.sql` to the remote jseverino.com D1 database.

        This is a fixed write operation for the operator's own site. It refuses to
        run unless `confirm=True` is passed. The schema uses `CREATE ... IF NOT
        EXISTS` and is intended for additive table/index updates.

        Args:
            confirm: Must be True to execute the remote schema import.
        """
        return site_ops_service.apply_d1_schema(site_ops, confirm)

    @mcp.tool()
    def check_jseverino_security_headers(path: str = "/") -> dict[str, Any]:
        """Check live jseverino.com security headers for one path.

        Uses a HEAD request against `https://jseverino.com` and returns the security
        headers that matter for the Astro/Cloudflare Pages stack.

        Args:
            path: Site path to check, e.g. "/" or "/contact/". Must be root-relative.
        """
        return site_ops_service.check_security_headers(site_ops, path)
