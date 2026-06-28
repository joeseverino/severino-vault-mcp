"""jseverino.com operations: fixed Cloudflare D1 reads and live header checks.

FastMCP-free, like the writeup and vault-write services, so the same fixed
workflows can back both the MCP tools and any shell wrapper. This is
deliberately not a generic shell: every D1 statement is internal and
parameter-bounded, and writes refuse without an explicit confirm.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

_SECURITY_HEADER_NAMES = (
    "content-security-policy",
    "reporting-endpoints",
    "strict-transport-security",
    "x-content-type-options",
    "referrer-policy",
    "permissions-policy",
    "cross-origin-opener-policy",
    "cross-origin-resource-policy",
)


@dataclass(frozen=True)
class SiteOpsRuntime:
    d1_database: str
    site_repo: Path
    site_origin: str

    @classmethod
    def from_env(cls) -> SiteOpsRuntime:
        return cls(
            d1_database=os.environ.get(
                "SVMC_JSEVERINO_D1_DATABASE", "jseverino-contact"
            ),
            site_repo=Path(
                os.path.expanduser(
                    os.environ.get(
                        "SVMC_JSEVERINO_SITE_REPO",
                        "~/Documents/Code/Projects/jseverino.com",
                    )
                )
            ),
            site_origin=os.environ.get(
                "SVMC_JSEVERINO_SITE_ORIGIN", "https://jseverino.com"
            ),
        )


def _bounded_limit(value: int, *, default: int = 20, max_value: int = 100) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = default
    return max(1, min(parsed, max_value))


def _sql_string(value: str) -> str:
    """SQLite single-quoted literal for fixed internal filters."""
    return "'" + value.replace("'", "''") + "'"


def _run_d1_json(
    runtime: SiteOpsRuntime,
    command: str,
    *,
    timeout: int = 20,
) -> dict[str, Any]:
    """Run one fixed D1 SQL command through Wrangler and parse JSON output."""
    wrangler = shutil.which("wrangler")
    if not wrangler:
        return {"ok": False, "error": "wrangler not found on PATH"}

    try:
        proc = subprocess.run(
            [
                wrangler,
                "d1",
                "execute",
                runtime.d1_database,
                "--remote",
                "--json",
                "--command",
                command,
            ],
            cwd=str(runtime.site_repo),
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"ok": False, "error": f"wrangler d1 execute failed: {exc}"}

    if proc.returncode != 0:
        return {
            "ok": False,
            "returncode": proc.returncode,
            "stderr": proc.stderr.strip(),
            "stdout": proc.stdout.strip(),
        }

    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {
            "ok": False,
            "error": "wrangler returned non-JSON output",
            "stdout": proc.stdout,
        }

    result = payload[0] if isinstance(payload, list) and payload else {}
    return {
        "ok": bool(result.get("success", True)),
        "database": runtime.d1_database,
        "results": result.get("results", []),
        "meta": result.get("meta", {}),
    }


def _head_headers(url: str, *, timeout: int = 10) -> dict[str, Any]:
    curl = shutil.which("curl")
    if curl:
        try:
            proc = subprocess.run(
                [curl, "-sI", "--max-time", str(timeout), url],
                capture_output=True,
                text=True,
                timeout=timeout + 2,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            return {"ok": False, "url": url, "error": f"curl HEAD failed: {exc}"}

        if proc.returncode == 0 and proc.stdout:
            lines = [line.strip() for line in proc.stdout.splitlines() if line.strip()]
            status = 0
            headers: dict[str, str] = {}
            for line in lines:
                if line.lower().startswith("http/"):
                    parts = line.split()
                    if len(parts) > 1 and parts[1].isdigit():
                        status = int(parts[1])
                    continue
                if ":" in line:
                    key, value = line.split(":", 1)
                    headers[key.lower()] = value.strip()
            return {"ok": 200 <= status < 400, "url": url, "status": status, "headers": headers}

    request = urllib.request.Request(url, method="HEAD")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            headers = {key.lower(): value for key, value in response.headers.items()}
            return {
                "ok": True,
                "url": url,
                "status": response.status,
                "headers": headers,
            }
    except urllib.error.HTTPError as exc:
        headers = {key.lower(): value for key, value in exc.headers.items()}
        return {"ok": False, "url": url, "status": exc.code, "headers": headers}
    except (OSError, urllib.error.URLError) as exc:
        return {"ok": False, "url": url, "error": str(exc)}


def _selected_security_headers(headers: dict[str, str]) -> dict[str, str | None]:
    return {name: headers.get(name) for name in _SECURITY_HEADER_NAMES}


_CONTACT_PII_ADVISORY = (
    "Contact PII redacted (name abbreviated, email masked, message previewed). "
    "Pass include_pii=True for full values — this enters the model context and "
    "is recorded in the local audit log."
)
_CSP_PII_ADVISORY = (
    "CSP client fields (ip_address, user_agent, raw_report) omitted. "
    "Pass include_pii=True to include them."
)


def _redact_email(email: str) -> str:
    """`jane@acme.com` -> `j***@acme.com`; the domain is kept, the local part not."""
    email = (email or "").strip()
    if "@" not in email:
        return "***" if email else ""
    local, _, domain = email.partition("@")
    head = local[0] if local else ""
    return f"{head}***@{domain}"


def _abbreviate_name(name: str) -> str:
    """`Jane Doe` -> `Jane D.`; single tokens are left intact."""
    parts = (name or "").split()
    if not parts:
        return ""
    if len(parts) == 1:
        return parts[0]
    return f"{parts[0]} {parts[-1][0]}."


def _preview(text: str, *, limit: int = 80) -> str:
    collapsed = " ".join((text or "").split())
    if len(collapsed) <= limit:
        return collapsed
    return collapsed[:limit].rstrip() + "…"


def _redact_contact_row(row: dict[str, Any]) -> dict[str, Any]:
    """Drop ip_address/user_agent/admin_notes; mask name/email; preview message."""
    message = str(row.get("message") or "")
    return {
        "id": row.get("id"),
        "created_at": row.get("created_at"),
        "name": _abbreviate_name(str(row.get("name") or "")),
        "email": _redact_email(str(row.get("email") or "")),
        "message_preview": _preview(message),
        "message_chars": len(message),
        "status": row.get("status"),
        "browser": row.get("browser"),
        "device": row.get("device"),
        "country": row.get("country"),
        "source_url": row.get("source_url"),
    }


def list_contact_submissions(
    runtime: SiteOpsRuntime,
    limit: int = 10,
    *,
    include_pii: bool = False,
) -> dict[str, Any]:
    """List recent contact submissions, redacting PII unless ``include_pii``.

    The full rows are fetched locally through Wrangler, but by default only a
    redacted projection (abbreviated name, masked email, message preview) is
    returned, so contact PII does not enter the model context unless explicitly
    requested.
    """
    limit = _bounded_limit(limit, default=10, max_value=100)
    sql = (
        "SELECT id, created_at, name, email, message, status, browser, device, "
        "country, source_url, ip_address, user_agent, admin_notes "
        "FROM contact_submissions ORDER BY created_at DESC LIMIT "
        f"{limit};"
    )
    result = _run_d1_json(runtime, sql)
    if not result.get("ok"):
        return result
    rows = result.get("results", [])
    if include_pii:
        result["pii_released"] = True
    else:
        result["pii_released"] = False
        result["results"] = [_redact_contact_row(row) for row in rows]
        result["advisory"] = _CONTACT_PII_ADVISORY
    return result


def list_csp_reports(
    runtime: SiteOpsRuntime,
    limit: int = 20,
    directive: str | None = None,
    *,
    include_pii: bool = False,
) -> dict[str, Any]:
    """List recent CSP reports. Client identifiers are omitted unless ``include_pii``."""
    limit = _bounded_limit(limit, default=20, max_value=100)
    where = ""
    if directive:
        directive = directive.strip()[:128]
        if directive:
            where = f"WHERE effective_directive = {_sql_string(directive)} "
    columns = (
        "id, created_at, effective_directive, blocked_uri, document_uri, "
        "source_file, status_code"
    )
    if include_pii:
        columns += ", ip_address, user_agent, country, raw_report"
    sql = (
        f"SELECT {columns} FROM csp_reports "
        f"{where}ORDER BY created_at DESC LIMIT {limit};"
    )
    result = _run_d1_json(runtime, sql)
    if result.get("ok"):
        result["pii_released"] = bool(include_pii)
        if not include_pii:
            result["advisory"] = _CSP_PII_ADVISORY
    return result


def count_csp_reports(runtime: SiteOpsRuntime) -> dict[str, Any]:
    total = _run_d1_json(runtime, "SELECT COUNT(*) AS total FROM csp_reports;")
    by_directive = _run_d1_json(
        runtime,
        "SELECT COALESCE(effective_directive, '(unknown)') AS effective_directive, "
        "COUNT(*) AS count FROM csp_reports GROUP BY effective_directive "
        "ORDER BY count DESC, effective_directive ASC;",
    )
    return {
        "ok": bool(total.get("ok") and by_directive.get("ok")),
        "database": runtime.d1_database,
        "total": total,
        "by_directive": by_directive,
    }


def apply_d1_schema(
    runtime: SiteOpsRuntime,
    confirm: bool = False,
) -> dict[str, Any]:
    if not confirm:
        return {
            "ok": False,
            "refused": True,
            "message": "Pass confirm=True to apply db/schema.sql to the remote D1 database.",
        }

    wrangler = shutil.which("wrangler")
    if not wrangler:
        return {"ok": False, "error": "wrangler not found on PATH"}

    schema = runtime.site_repo / "db" / "schema.sql"
    if not schema.is_file():
        return {"ok": False, "error": f"schema file not found: {schema}"}

    try:
        proc = subprocess.run(
            [
                wrangler,
                "d1",
                "execute",
                runtime.d1_database,
                "--remote",
                "--file",
                str(schema),
            ],
            cwd=str(runtime.site_repo),
            capture_output=True,
            text=True,
            timeout=60,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"ok": False, "error": f"wrangler d1 schema apply failed: {exc}"}

    return {
        "ok": proc.returncode == 0,
        "database": runtime.d1_database,
        "schema": str(schema),
        "returncode": proc.returncode,
        "stdout": proc.stdout.strip(),
        "stderr": proc.stderr.strip(),
    }


def check_security_headers(
    runtime: SiteOpsRuntime,
    path: str = "/",
) -> dict[str, Any]:
    path = (path or "/").strip()
    if not path.startswith("/"):
        return {"ok": False, "error": "path must start with '/'"}
    if path.startswith("//"):
        return {"ok": False, "error": "path must be root-relative, not protocol-relative"}

    url = f"{runtime.site_origin.rstrip('/')}{path}"
    result = _head_headers(url)
    headers = result.get("headers", {})
    selected = _selected_security_headers(headers if isinstance(headers, dict) else {})
    csp = selected.get("content-security-policy") or ""
    reporting = selected.get("reporting-endpoints") or ""
    return {
        **result,
        "selected_headers": selected,
        "checks": {
            "has_csp": bool(csp),
            "no_unsafe_inline_script": "script-src 'unsafe-inline'" not in csp,
            "has_csp_report_to": "report-to csp-endpoint" in csp,
            "has_csp_report_uri": "report-uri https://jseverino.com/api/csp-report" in csp,
            "has_reporting_endpoints": "csp-endpoint=" in reporting,
        },
    }


__all__ = [
    "SiteOpsRuntime",
    "apply_d1_schema",
    "check_security_headers",
    "count_csp_reports",
    "list_contact_submissions",
    "list_csp_reports",
]
