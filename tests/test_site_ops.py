"""Tests for the jseverino.com site-ops service, focused on PII redaction.

These never invoke Wrangler: `_run_d1_json` is monkeypatched to return canned
rows (and to capture the SQL), so the gate logic is tested in isolation.
"""

from __future__ import annotations

import asyncio
import importlib
import sys
from pathlib import Path

from severino_vault_mcp import site_ops_service as ops

_RUNTIME = ops.SiteOpsRuntime(
    d1_database="jseverino-contact",
    site_repo=Path("/tmp/site"),
    site_origin="https://jseverino.com",
)

_CONTACT_ROW = {
    "id": 7,
    "created_at": "2026-06-10T14:22:00Z",
    "name": "Jane Doe",
    "email": "jane@acme.com",
    "message": "Hi, I'd like to discuss a longer project with you in some detail.",
    "status": "unread",
    "browser": "Firefox",
    "device": "desktop",
    "country": "US",
    "source_url": "/contact",
    "ip_address": "203.0.113.7",
    "user_agent": "Mozilla/5.0",
    "admin_notes": "lead",
}


# ----- redaction helpers -----------------------------------------------------


def test_redact_email_keeps_domain_drops_local() -> None:
    assert ops._redact_email("jane@acme.com") == "j***@acme.com"
    assert ops._redact_email("") == ""
    assert ops._redact_email("noatsign") == "***"


def test_abbreviate_name() -> None:
    assert ops._abbreviate_name("Jane Doe") == "Jane D."
    assert ops._abbreviate_name("Jane") == "Jane"
    assert ops._abbreviate_name("") == ""


def test_preview_truncates_and_collapses() -> None:
    assert ops._preview("a\n  b   c") == "a b c"
    long = "word " * 40
    out = ops._preview(long, limit=20)
    assert len(out) <= 21  # 20 + ellipsis
    assert out.endswith("…")


# ----- contact submissions gate ----------------------------------------------


def test_contact_submissions_redacted_by_default(monkeypatch) -> None:
    monkeypatch.setattr(
        ops,
        "_run_d1_json",
        lambda _rt, _sql, **_kw: {
            "ok": True,
            "database": "jseverino-contact",
            "results": [dict(_CONTACT_ROW)],
            "meta": {},
        },
    )
    result = ops.list_contact_submissions(_RUNTIME)
    assert result["pii_released"] is False
    assert "advisory" in result
    row = result["results"][0]
    assert row["name"] == "Jane D."
    assert row["email"] == "j***@acme.com"
    assert row["message_preview"].startswith("Hi, I'd like")
    assert row["message_chars"] == len(_CONTACT_ROW["message"])
    # Raw PII columns must not leak in the redacted projection.
    for leaked in ("message", "ip_address", "user_agent", "admin_notes"):
        assert leaked not in row


def test_contact_submissions_full_with_include_pii(monkeypatch) -> None:
    monkeypatch.setattr(
        ops,
        "_run_d1_json",
        lambda _rt, _sql, **_kw: {
            "ok": True,
            "database": "jseverino-contact",
            "results": [dict(_CONTACT_ROW)],
            "meta": {},
        },
    )
    result = ops.list_contact_submissions(_RUNTIME, include_pii=True)
    assert result["pii_released"] is True
    assert "advisory" not in result
    row = result["results"][0]
    assert row["email"] == "jane@acme.com"
    assert row["message"] == _CONTACT_ROW["message"]
    assert row["ip_address"] == "203.0.113.7"


def test_contact_submissions_passes_through_d1_error(monkeypatch) -> None:
    monkeypatch.setattr(
        ops,
        "_run_d1_json",
        lambda _rt, _sql, **_kw: {"ok": False, "error": "wrangler not found on PATH"},
    )
    result = ops.list_contact_submissions(_RUNTIME)
    assert result == {"ok": False, "error": "wrangler not found on PATH"}


# ----- CSP reports gate ------------------------------------------------------


def _capture_sql(monkeypatch) -> dict[str, str]:
    captured: dict[str, str] = {}

    def fake(_rt, sql, **_kw):
        captured["sql"] = sql
        return {"ok": True, "database": "x", "results": [], "meta": {}}

    monkeypatch.setattr(ops, "_run_d1_json", fake)
    return captured


def test_csp_reports_omit_client_fields_by_default(monkeypatch) -> None:
    captured = _capture_sql(monkeypatch)
    result = ops.list_csp_reports(_RUNTIME)
    assert result["pii_released"] is False
    assert "advisory" in result
    for col in ("ip_address", "user_agent", "raw_report"):
        assert col not in captured["sql"]


def test_csp_reports_include_client_fields_with_include_pii(monkeypatch) -> None:
    captured = _capture_sql(monkeypatch)
    result = ops.list_csp_reports(_RUNTIME, include_pii=True)
    assert result["pii_released"] is True
    assert "advisory" not in result
    assert "ip_address" in captured["sql"]
    assert "raw_report" in captured["sql"]


# ----- server-layer audit ----------------------------------------------------


def _fresh_server():
    for mod in list(sys.modules):
        if mod.startswith("severino_vault_mcp"):
            del sys.modules[mod]
    return importlib.import_module("severino_vault_mcp.server")


def test_include_pii_writes_audit_line(tmp_path: Path, monkeypatch) -> None:
    audit_path = tmp_path / "audit.log"
    monkeypatch.setenv("SVMC_RESTRICTED_UNLOCK_AUDIT_LOG", str(audit_path))
    server = _fresh_server()
    monkeypatch.setattr(
        server.site_ops_service,
        "_run_d1_json",
        lambda _rt, _sql, **_kw: {
            "ok": True,
            "database": "jseverino-contact",
            "results": [dict(_CONTACT_ROW)],
            "meta": {},
        },
    )

    asyncio.run(server.mcp.call_tool("list_contact_submissions", {"include_pii": True}))
    assert audit_path.is_file()
    audit = audit_path.read_text(encoding="utf-8")
    assert "action=contact_pii_access" in audit
    assert "rows=1" in audit
    # The PII itself must never reach the audit log.
    assert "jane@acme.com" not in audit


def test_redacted_call_writes_no_audit_line(tmp_path: Path, monkeypatch) -> None:
    audit_path = tmp_path / "audit.log"
    monkeypatch.setenv("SVMC_RESTRICTED_UNLOCK_AUDIT_LOG", str(audit_path))
    server = _fresh_server()
    monkeypatch.setattr(
        server.site_ops_service,
        "_run_d1_json",
        lambda _rt, _sql, **_kw: {
            "ok": True,
            "database": "jseverino-contact",
            "results": [dict(_CONTACT_ROW)],
            "meta": {},
        },
    )

    asyncio.run(server.mcp.call_tool("list_contact_submissions", {}))
    assert not audit_path.exists()
