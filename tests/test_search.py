"""Tests for the vault loader, search ranker, and write tools.

These don't depend on a running MCP host — each test sets up a tiny fake
vault on disk and exercises the loader / tools directly.
"""

from __future__ import annotations

import asyncio
import hashlib
from pathlib import Path

import pytest


@pytest.fixture
def fake_vault(tmp_path: Path, monkeypatch) -> Path:
    """Spin up a tiny fake vault."""
    (tmp_path / "01 Projects").mkdir()
    (tmp_path / "02 Infrastructure").mkdir()
    (tmp_path / "03 Runbooks").mkdir()
    (tmp_path / "00 Inbox" / "Daily Note").mkdir(parents=True)
    (tmp_path / ".svmc").mkdir()

    (tmp_path / "03 Runbooks" / "Add Nginx Proxy Host.md").write_text(
        """---
doc_id: rb-add-nginx-proxy-host
title: Add Nginx Proxy Host
doc_type: runbook
system: Nginx Proxy Manager
environment: other
status: active
sensitivity: internal
last_reviewed: 2025-01-01
related_projects: []
related_assets: []
tags:
  - nginx
  - network-operations
---

## Goal

Expose an internal service over HTTPS via NPM.
""",
        encoding="utf-8",
    )

    (tmp_path / "02 Infrastructure" / "Local PKI.md").write_text(
        """---
doc_id: infra-local-pki
title: Local PKI
doc_type: architecture_note
system: Local PKI
environment: local_mac
status: active
sensitivity: restricted
last_reviewed: 2026-04-01
tags: [pki, ca]
---

# Local PKI

CA private key lives offline.
""",
        encoding="utf-8",
    )

    (tmp_path / "03 Runbooks" / "Quick Index.md").write_text(
        """---
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [index, mcp, navigation]
---

# Example Operations Vault Quick Index

| Intent | Start Here |
|---|---|
| Add HTTPS service | rb-add-nginx-proxy-host |
""",
        encoding="utf-8",
    )

    (tmp_path / "01 Projects" / "untagged.md").write_text(
        "# Untagged\n\nNo frontmatter yet.\n",
        encoding="utf-8",
    )

    (tmp_path / "00 Inbox" / "Daily Note" / "2026-06-19.md").write_text(
        """---
doc_id: daily-20260619
created: 2026-06-19 22:39:04
date: 2026-06-19
---

- [x] Split daily notes from inbox captures.
- Added a dedicated Daily Note template.
- Fixed Obsidian archive command escaping.
""",
        encoding="utf-8",
    )

    (tmp_path / ".svmc" / "aliases.toml").write_text(
        """[aliases]
"https proxy" = "rb-add-nginx-proxy-host"
"offline ca" = "infra-local-pki"
"missing target" = "rb-does-not-exist"
""",
        encoding="utf-8",
    )

    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SVMC_CACHE_SECONDS", "0")
    return tmp_path


def test_vault_brief_flags_stale_docs_and_inbox(fake_vault: Path) -> None:
    from severino_vault_mcp.brief_service import vault_brief
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader

    # A top-level inbox capture (the fixture only seeds a Daily Note subdir).
    (fake_vault / "00 Inbox" / "idea.md").write_text(
        "---\ndoc_id: inbox-20260620-000000\ncreated: 2026-06-20 00:00:00\n---\n\nthought\n",
        encoding="utf-8",
    )

    result = vault_brief(VaultLoader(Config.from_env()), review_after_days=180)

    assert result["ok"] is True
    review_ids = {doc["doc_id"] for doc in result["docs_to_review"]["docs"]}
    assert "rb-add-nginx-proxy-host" in review_ids  # last_reviewed 2025-01-01
    assert "infra-local-pki" not in review_ids       # 2026-04-01, still fresh
    assert result["inbox"]["count"] == 1
    # tmp vault is not a git repo, so recent_changes degrades gracefully
    assert result["recent_changes"]["count"] == 0


def _fresh_module(name: str):
    """Import the module after env vars are set so module-level Config is fresh."""
    import importlib
    import sys
    for mod in list(sys.modules):
        if mod.startswith("severino_vault_mcp"):
            del sys.modules[mod]
    return importlib.import_module(name)


def _vws_runtime():
    """A freshly-imported vault_write_service plus a loader on the env vault."""
    vws = _fresh_module("severino_vault_mcp.vault_write_service")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader

    return vws, VaultLoader(Config.from_env())


def _encoded_unlock_hash(phrase: str, salt: bytes = b"test-salt") -> str:
    digest = hashlib.sha256(salt + phrase.encode("utf-8")).hexdigest()
    return f"sha256:{salt.hex()}:{digest}"


def test_loader_indexes_only_tagged_docs(fake_vault: Path) -> None:
    vault_mod = _fresh_module("severino_vault_mcp.vault")
    from severino_vault_mcp.config import Config
    loader = vault_mod.VaultLoader(Config.from_env())
    idx = loader.index()
    doc_ids = {d.doc_id for d in idx.docs}
    assert doc_ids == {
        "rb-add-nginx-proxy-host",
        "infra-local-pki",
        "report-playbook-mcp-index",
    }
    assert "daily-20260619" not in doc_ids


def test_daily_progress_resolves_friday_from_anchor(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")

    result = server.daily_progress(
        "what progress did i make on friday?",
        today="2026-06-20",
    )

    assert result["found"] is True
    assert result["resolved_date"] == "2026-06-19"
    assert result["date_resolution"] == "weekday"
    assert result["doc_id"] == "daily-20260619"
    assert result["obsidian_path"] == "00 Inbox/Daily Note/2026-06-19.md"
    assert "Daily Note template" in result["body"]
    assert any("archive command" in item for item in result["progress_items"])


def test_daily_progress_reports_missing_note(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")

    result = server.daily_progress("what happened yesterday?", today="2026-06-19")

    assert result["found"] is False
    assert result["resolved_date"] == "2026-06-18"
    assert result["expected_path"] == "00 Inbox/Daily Note/2026-06-18.md"
    assert result["progress_items"] == []


def test_loader_indexes_local_aliases(fake_vault: Path) -> None:
    vault_mod = _fresh_module("severino_vault_mcp.vault")
    from severino_vault_mcp.config import Config

    loader = vault_mod.VaultLoader(Config.from_env())
    idx = loader.index()

    assert idx.aliases["https proxy"] == "rb-add-nginx-proxy-host"
    assert idx.aliases["offline ca"] == "infra-local-pki"
    assert idx.invalid_aliases["missing target"] == "rb-does-not-exist"


def test_config_file_sets_vault_path_and_env_overrides(tmp_path: Path, monkeypatch) -> None:
    vault_a = tmp_path / "vault-a"
    vault_b = tmp_path / "vault-b"
    config_path = tmp_path / "config.toml"
    config_path.write_text(
        f"""
[vault]
path = "{vault_a}"
indexed_dirs = ["Docs"]

[cache]
seconds = 12

[metadata]
url = "https://metadata.example.test"
""",
        encoding="utf-8",
    )

    monkeypatch.setenv("SVMC_CONFIG", str(config_path))
    monkeypatch.delenv("SVMC_VAULT_PATH", raising=False)
    monkeypatch.delenv("SVMC_INDEXED_DIRS", raising=False)
    monkeypatch.delenv("SVMC_CACHE_SECONDS", raising=False)

    config_mod = _fresh_module("severino_vault_mcp.config")
    config = config_mod.Config.from_env()
    assert config.vault_path == vault_a
    assert config.indexed_dirs == ("Docs",)
    assert config.daily_notes_dir == "00 Inbox/Daily Note"
    assert config.cache_seconds == 12
    assert config.metadata_url == "https://metadata.example.test"

    monkeypatch.setenv("SVMC_VAULT_PATH", str(vault_b))
    monkeypatch.setenv("SVMC_INDEXED_DIRS", "Ops:Runbooks")
    monkeypatch.setenv("SVMC_CACHE_SECONDS", "0")
    monkeypatch.setenv("SVMC_METADATA_URL", "https://override.example.test")
    config = config_mod.Config.from_env()
    assert config.vault_path == vault_b
    assert config.indexed_dirs == ("Ops", "Runbooks")
    assert config.cache_seconds == 0
    assert config.metadata_url == "https://override.example.test"


def test_doctor_reports_missing_frontmatter_and_proposes_fix(fake_vault: Path) -> None:
    doctor = _fresh_module("severino_vault_mcp.doctor")
    from severino_vault_mcp.config import Config

    report = doctor.validate_vault(Config.from_env(), propose=True)
    assert report.ok is False
    finding = next(f for f in report.findings if f.relative_path == "01 Projects/untagged.md")
    assert finding.message == "missing YAML frontmatter"
    assert "doc_id: project-untagged" in finding.proposal
    assert "sensitivity: internal" in finding.proposal


def test_doctor_reports_invalid_frontmatter(fake_vault: Path) -> None:
    bad_doc = fake_vault / "03 Runbooks" / "Bad.md"
    bad_doc.write_text(
        """---
doc_id: nope
title: Bad
doc_type: made_up
system: Bad
environment: other
status: active
sensitivity: internal
---

# Bad
""",
        encoding="utf-8",
    )

    doctor = _fresh_module("severino_vault_mcp.doctor")
    from severino_vault_mcp.config import Config

    report = doctor.validate_vault(Config.from_env())
    messages = [f.message for f in report.findings if f.relative_path == "03 Runbooks/Bad.md"]
    assert any("doc_id must start" in message for message in messages)
    assert any("doc_type='made_up'" in message for message in messages)


def test_doctor_reports_duplicate_doc_id(fake_vault: Path) -> None:
    duplicate = fake_vault / "03 Runbooks" / "Duplicate.md"
    duplicate.write_text(
        """---
doc_id: rb-add-nginx-proxy-host
title: Duplicate
doc_type: runbook
system: Duplicate
environment: other
status: active
sensitivity: internal
---

# Duplicate
""",
        encoding="utf-8",
    )

    doctor = _fresh_module("severino_vault_mcp.doctor")
    from severino_vault_mcp.config import Config

    report = doctor.validate_vault(Config.from_env())
    messages = [f.message for f in report.findings if f.relative_path == "03 Runbooks/Duplicate.md"]
    assert any("duplicate doc_id" in message for message in messages)


def test_duplicate_doc_id_is_excluded_and_reported_at_runtime(
    fake_vault: Path,
) -> None:
    duplicate = fake_vault / "03 Runbooks" / "Duplicate.md"
    duplicate.write_text(
        """---
doc_id: rb-add-nginx-proxy-host
title: Duplicate Nginx Runbook
doc_type: runbook
system: Duplicate
environment: other
status: active
sensitivity: internal
---

# Duplicate
""",
        encoding="utf-8",
    )

    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("rb-add-nginx-proxy-host")
    assert result["found"] is False
    assert result["ambiguous"] is True
    assert sorted(result["paths"]) == [
        "03 Runbooks/Add Nginx Proxy Host.md",
        "03 Runbooks/Duplicate.md",
    ]

    search = server.find_runbook("nginx proxy")
    assert all(
        hit["doc_id"] != "rb-add-nginx-proxy-host"
        for hit in search["hits"]
    )
    resource = server.vault_doc("rb-add-nginx-proxy-host")
    assert "# Duplicate Vault Doc ID" in resource
    assert "03 Runbooks/Duplicate.md" in resource


def test_find_runbook_ranks_nginx_query(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("nginx proxy")
    assert result["hits"], result
    assert result["hits"][0]["doc_id"] == "rb-add-nginx-proxy-host"


def test_find_runbook_matches_body_only_term(fake_vault: Path) -> None:
    # "expose" appears only in the nginx doc's body ("Expose an internal
    # service..."), never in its title/tags/system/doc_id. Before the capped
    # body signal this scored 0 and returned nothing; now it surfaces the doc.
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("expose a service")
    assert result["hits"], result
    assert result["hits"][0]["doc_id"] == "rb-add-nginx-proxy-host"


def test_find_runbook_body_signal_does_not_outrank_a_direct_tag_hit(
    fake_vault: Path,
) -> None:
    # A short doc whose tag is a direct hit must beat a doc that only mentions
    # the term in passing prose — the body signal is capped for exactly this.
    (fake_vault / "02 Infrastructure" / "Mentions HTTPS.md").write_text(
        """---
doc_id: infra-mentions-https
title: Edge Notes
doc_type: architecture_note
system: Misc
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [notes]
---

We terminate HTTPS at the edge. HTTPS HTTPS HTTPS everywhere, lots of HTTPS.
""",
        encoding="utf-8",
    )
    (fake_vault / "03 Runbooks" / "HTTPS Runbook.md").write_text(
        """---
doc_id: rb-https-setup
title: HTTPS Setup
doc_type: runbook
system: TLS
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [https, tls]
---

Steps.
""",
        encoding="utf-8",
    )
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("https")
    assert result["hits"][0]["doc_id"] == "rb-https-setup", result["hits"]


def test_find_runbook_ignores_pure_stopword_query(fake_vault: Path) -> None:
    # Every token here is a query stopword, so nothing is left to match on —
    # filler words must not manufacture hits against unrelated docs.
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("a the of and to")
    assert result["hits"] == [], result


def test_find_runbook_ranks_normal_ssh_above_recovery(fake_vault: Path) -> None:
    (fake_vault / "03 Runbooks" / "SSH Into VPS.md").write_text(
        """---
doc_id: rb-ssh-into-vps
title: SSH Into VPS
doc_type: runbook
system: sl-cloud-edge-01
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-05-17
tags: [vps, ssh, access, cloud-edge]
---

## Connect

```bash
ssh edge
```
""",
        encoding="utf-8",
    )
    (fake_vault / "03 Runbooks" / "Recover SSH Access.md").write_text(
        """---
doc_id: rb-recover-ssh-cloud-edge
title: Recover SSH Access
doc_type: recovery_procedure
system: sl-cloud-edge-01
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-05-16
tags: [vps]
---

Use only when `ssh edge` is refused or times out.
""",
        encoding="utf-8",
    )

    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("how do i ssh into the VPS")
    assert result["hits"][0]["doc_id"] == "rb-ssh-into-vps"


def test_get_runbook_returns_selected_body_in_one_call(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_runbook("nginx proxy")
    assert result["found"] is True
    assert result["selected"]["doc_id"] == "rb-add-nginx-proxy-host"
    assert result["selected"]["body_released"] is True
    assert "Expose an internal service" in result["selected"]["body"]


def test_get_runbook_includes_quick_index_recommendation(fake_vault: Path) -> None:
    (fake_vault / "02 Infrastructure" / "AdGuard Home Setup.md").write_text(
        """---
doc_id: infra-adguard-home
title: AdGuard Home Setup
doc_type: architecture_note
system: AdGuard Home
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-05-17
tags: [homelab, adguard, home]
---

# AdGuard Home Setup

AdGuard Home runs as a Docker container.
""",
        encoding="utf-8",
    )
    (fake_vault / "03 Runbooks" / "Quick Index.md").write_text(
        """---
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [index, mcp, navigation]
---

# Example Operations Vault Quick Index

| Intent | Command | Doc |
|---|---|---|
| Check AdGuard Home container status | `ssh homelab-server 'cd /opt/apps/adguard && docker compose ps'` | [[AdGuard Home Setup]] |
""",
        encoding="utf-8",
    )

    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_runbook("how do i check the status of the adguard home container?")
    assert result["found"] is True
    assert result["selected"]["doc_id"] == "infra-adguard-home"
    assert result["recommended"]["source"] == "vault://quick-index"
    assert result["recommended"]["target_doc_id"] == "infra-adguard-home"
    assert "docker compose ps" in result["recommended"]["command"]


def test_get_runbook_does_not_recommend_conflicting_quick_index_doc(
    fake_vault: Path,
) -> None:
    (fake_vault / "03 Runbooks" / "Quick Index.md").write_text(
        """---
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [index, mcp, navigation]
---

# Example Operations Vault Quick Index

| Intent | Command | Doc |
|---|---|---|
| Run Vault MCP tests | `cd repo && scripts/check.sh` | [[Generate Internal Service Certificate]] |
| Check Vault MCP config index | `severino-vault-mcp doctor` | [[Generate Internal Service Certificate]] |
""",
        encoding="utf-8",
    )

    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_runbook("restart vault mcp")

    assert result["found"] is True
    assert result["selected"]["doc_id"] != "rb-generate-internal-cert"
    assert "quick_index_matches" in result
    assert "recommended" not in result


def test_get_runbook_withholds_secret_adjacent_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_runbook("local pki")
    assert result["found"] is True
    assert result["selected"]["doc_id"] == "infra-local-pki"
    assert result["selected"]["body_released"] is False
    assert "body" not in result["selected"]
    assert result["selected"]["unlock"]["result"] == "not_requested"


def test_read_doc_default_refuses_secret_adjacent(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("infra-local-pki")
    assert result["found"] is True
    assert result["body_released"] is False
    assert "body" not in result
    assert "restricted" in result["advisory"].lower()


def test_read_doc_secret_adjacent_request_requires_local_unlock(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("infra-local-pki", include_restricted=True)
    assert result["body_released"] is False
    assert "body" not in result
    assert result["unlock"]["result"] == "disabled"
    assert "interactive unlock is disabled" in result["unlock"]["message"].lower()


def test_read_doc_releases_secret_adjacent_after_local_unlock(
    fake_vault: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("SVMC_ALLOW_RESTRICTED_UNLOCK", "1")
    monkeypatch.setenv("SVMC_RESTRICTED_UNLOCK_HASH", _encoded_unlock_hash("open sesame"))
    monkeypatch.setenv(
        "SVMC_RESTRICTED_UNLOCK_AUDIT_LOG",
        str(fake_vault / "audit.log"),
    )

    server = _fresh_module("severino_vault_mcp.server")
    monkeypatch.setattr(server, "prompt_unlock_phrase", lambda _doc_id, _title: "open sesame")

    result = server.read_doc("infra-local-pki", include_restricted=True)
    assert result["body_released"] is True
    assert "CA private key" in result["body"]
    assert result["override_used"] is True
    assert result["unlock"]["result"] == "released"

    audit = (fake_vault / "audit.log").read_text(encoding="utf-8")
    assert "doc_id=infra-local-pki" in audit
    assert "result=released" in audit


def test_read_doc_keeps_secret_adjacent_locked_after_bad_unlock(
    fake_vault: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("SVMC_ALLOW_RESTRICTED_UNLOCK", "1")
    monkeypatch.setenv("SVMC_RESTRICTED_UNLOCK_HASH", _encoded_unlock_hash("open sesame"))
    monkeypatch.setenv(
        "SVMC_RESTRICTED_UNLOCK_AUDIT_LOG",
        str(fake_vault / "audit.log"),
    )

    server = _fresh_module("severino_vault_mcp.server")
    monkeypatch.setattr(server, "prompt_unlock_phrase", lambda _doc_id, _title: "wrong")

    result = server.read_doc("infra-local-pki", include_restricted=True)
    assert result["body_released"] is False
    assert "body" not in result
    assert result["unlock"]["result"] == "failed"

    audit = (fake_vault / "audit.log").read_text(encoding="utf-8")
    assert "doc_id=infra-local-pki" in audit
    assert "result=failed" in audit


def test_read_doc_returns_body_for_internal(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("rb-add-nginx-proxy-host")
    assert result["body_released"] is True
    assert "## Goal" in result["body"]


def test_read_doc_resolves_local_aliases(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("https proxy")
    assert result["found"] is True
    assert result["doc_id"] == "rb-add-nginx-proxy-host"
    assert result["resolved_from_alias"]["matched_alias"] == "https proxy"
    assert result["body_released"] is True


def test_read_doc_alias_preserves_secret_adjacent_gate(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("offline ca")
    assert result["found"] is True
    assert result["doc_id"] == "infra-local-pki"
    assert result["body_released"] is False
    assert "body" not in result
    assert result["resolved_from_alias"]["target_doc_id"] == "infra-local-pki"


def test_read_doc_missing_doc_guides_discovery(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("not a doc")
    assert result["found"] is False
    assert "stable `doc_id`" in result["guidance"]
    assert result["suggested_tools"] == ["find_runbook", "lookup_system", "search_body"]


def test_quick_index_resource_returns_index_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.quick_index()
    assert "# Example Operations Vault Quick Index" in result
    assert "rb-add-nginx-proxy-host" in result


def test_vault_doc_resource_returns_releasable_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.vault_doc("rb-add-nginx-proxy-host")
    assert "## Goal" in result
    assert "Expose an internal service" in result


def test_vault_doc_resource_withholds_secret_adjacent_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.vault_doc("infra-local-pki")
    assert "restricted" in result
    assert "infra-local-pki" in result
    assert "CA private key lives offline" not in result


def test_vault_doc_resource_handles_missing_doc(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.vault_doc("rb-does-not-exist")
    assert "# Vault Doc Not Found" in result
    assert "rb-does-not-exist" in result


def test_mcp_resources_are_registered_and_resolvable(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    manager = server.mcp._resource_manager

    resources = {str(resource.uri): resource for resource in manager.list_resources()}
    templates = {
        template.uri_template: template for template in manager.list_templates()
    }

    assert "vault://quick-index" in resources
    assert "vault://doc/{doc_id}" in templates

    async def read_template_resource() -> str:
        resource = await manager.get_resource("vault://doc/rb-add-nginx-proxy-host")
        return await resource.read()

    rendered = asyncio.run(read_template_resource())
    assert "## Goal" in rendered
    assert "Expose an internal service" in rendered


def test_read_doc_releases_sensitive_with_advisory(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        sensitivity="sensitive",
    )
    result = server.read_doc("rb-add-nginx-proxy-host")
    assert result["body_released"] is True
    assert "## Goal" in result["body"]
    assert "sensitive" in result["advisory"].lower()


def test_add_frontmatter_validates_enums(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.add_frontmatter(
        relative_path="01 Projects/untagged.md",
        doc_id="bad-prefix-foo",
        title="Foo",
        doc_type="runbook",
        system="Foo",
    )
    assert result["ok"] is False
    assert "doc_id" in result["error"]


def test_add_frontmatter_accepts_homelab_environment(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.add_frontmatter(
        relative_path="01 Projects/untagged.md",
        doc_id="project-homelab-untagged",
        title="Homelab Untagged",
        doc_type="architecture_note",
        system="Homelab",
        environment="homelab",
    )
    assert result["ok"] is True, result
    body = (fake_vault / "01 Projects" / "untagged.md").read_text(encoding="utf-8")
    assert "environment: homelab" in body


def test_add_frontmatter_writes(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.add_frontmatter(
        relative_path="01 Projects/untagged.md",
        doc_id="project-untagged",
        title="Untagged",
        doc_type="architecture_note",
        system="Untagged",
        environment="other",
    )
    assert result["ok"] is True, result
    body = (fake_vault / "01 Projects" / "untagged.md").read_text(encoding="utf-8")
    assert body.startswith("---")
    assert "doc_id: project-untagged" in body


def test_add_frontmatter_refuses_overwrite(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.add_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        doc_id="rb-something-else",
        title="X",
        doc_type="runbook",
        system="X",
    )
    assert result["ok"] is False
    assert "already starts with" in result["error"]


def test_update_frontmatter_touches_last_reviewed(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        touch_last_reviewed=True,
        add_tags=["proxy"],
    )
    assert result["ok"] is True
    assert "last_reviewed" in result["changed_fields"]
    assert "tags" in result["changed_fields"]
    body = (fake_vault / "03 Runbooks" / "Add Nginx Proxy Host.md").read_text(encoding="utf-8")
    assert "proxy" in body  # tag added
    # doc_id unchanged
    assert "doc_id: rb-add-nginx-proxy-host" in body


def test_update_frontmatter_preserves_multiline_scalar(fake_vault: Path) -> None:
    path = fake_vault / "03 Runbooks" / "Add Nginx Proxy Host.md"
    text = path.read_text(encoding="utf-8")
    path.write_text(
        text.replace(
            "tags:\n  - nginx\n  - network-operations\n",
            "tags:\n  - nginx\n  - network-operations\n"
            "notes: >-\n"
            "  First line of context.\n"
            "  Second line of context.\n",
        ),
        encoding="utf-8",
    )

    server = _fresh_module("severino_vault_mcp.server")
    result = server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        title="Add an Nginx Proxy Host",
    )
    assert result["ok"] is True

    frontmatter = _fresh_module(
        "severino_vault_mcp.frontmatter"
    ).read_frontmatter(path)
    assert frontmatter is not None
    assert frontmatter["notes"] == (
        "First line of context. Second line of context."
    )


def test_update_frontmatter_keeps_original_on_atomic_write_failure(
    fake_vault: Path,
    monkeypatch,
) -> None:
    path = fake_vault / "03 Runbooks" / "Add Nginx Proxy Host.md"
    original = path.read_text(encoding="utf-8")
    server = _fresh_module("severino_vault_mcp.server")

    def fail_write(_path, _text) -> None:
        raise OSError("simulated replacement failure")

    monkeypatch.setattr(
        server.vault_write_service,
        "atomic_write_text",
        fail_write,
    )
    result = server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        title="Should Not Persist",
    )
    assert result["ok"] is False
    assert "simulated replacement failure" in result["error"]
    assert path.read_text(encoding="utf-8") == original


def test_update_frontmatter_refuses_without_frontmatter(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.update_frontmatter(
        relative_path="01 Projects/untagged.md",
        status="active",
    )
    assert result["ok"] is False
    assert "no frontmatter" in result["error"].lower()


def test_search_body_finds_text_in_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.search_body("HTTPS via NPM")
    doc_ids = [h["doc_id"] for h in result["hits_by_doc"]]
    assert "rb-add-nginx-proxy-host" in doc_ids


def test_search_body_always_excludes_restricted(fake_vault: Path) -> None:
    # Restricted bodies are never searched: search_body has no unlock affordance
    # (that one-shot local unlock is a read_doc-only path), so there is no flag
    # to widen this — exclusion is structural.
    server = _fresh_module("severino_vault_mcp.server")
    default = server.search_body("CA private key")
    assert default["doc_count"] == 0
    assert default["excluded"]["restricted_skipped"] >= 1


def test_search_body_skips_frontmatter_hits(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    # "NPM" appears in the nginx runbook frontmatter system AND in the body.
    # The frontmatter hit should be excluded; the body hit should remain.
    result = server.search_body("NPM")
    nginx_hit = next(
        h for h in result["hits_by_doc"] if h["doc_id"] == "rb-add-nginx-proxy-host"
    )
    for snip in nginx_hit["snippets"]:
        # Frontmatter spans the first ~16 lines of this fixture doc.
        assert snip["line_number"] >= 17, snip


def test_inventory_for_project_filters_by_slug(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    # Tag the nginx runbook with a related project, then look it up.
    server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        add_related_projects=["client-edge-dns"],
    )
    result = server.inventory_for_project("client-edge-dns")
    assert result["match_count"] == 1
    assert "runbook" in result["by_doc_type"]


def test_sample_vault_is_reproducible(monkeypatch) -> None:
    sample_vault = Path(__file__).resolve().parents[1] / "examples" / "sample-vault"
    monkeypatch.setenv("SVMC_VAULT_PATH", str(sample_vault))
    monkeypatch.setenv("SVMC_CACHE_SECONDS", "0")

    server = _fresh_module("severino_vault_mcp.server")

    index_body = server.quick_index()
    assert "Example Operations Vault Quick Index" in index_body
    assert "rb-generate-internal-cert" in index_body

    doc_body = server.vault_doc("rb-generate-internal-cert")
    assert "## Commands" in doc_body
    assert "./cert-gen <service>.internal.example" in doc_body

    cert_result = server.find_runbook("generate internal certificate")
    assert cert_result["hits"][0]["doc_id"] == "rb-generate-internal-cert"

    ca_result = server.read_doc("infra-offline-ca")
    assert ca_result["found"] is True
    assert ca_result["body_released"] is False
    assert "body" not in ca_result

    ca_title_result = server.read_doc("Offline CA")
    assert ca_title_result["found"] is True
    assert ca_title_result["doc_id"] == "infra-offline-ca"
    assert ca_title_result["body_released"] is False
    assert "body" not in ca_title_result

    ca_slug_result = server.read_doc("offline ca")
    assert ca_slug_result["found"] is True
    assert ca_slug_result["doc_id"] == "infra-offline-ca"

    system_result = server.lookup_system("Offline CA")
    assert any(match["doc_id"] == "infra-offline-ca" for match in system_result["matches"])


# ----- P1 section-scoped retrieval -------------------------------------------

_MULTISECTION_BODY = """Overview line before any heading.

## Routine operations

Run the daily job to keep things current.

### Backing commands

Use `./backup nightly` to snapshot the data.

## Troubleshooting

Check the resolver logs first when latency spikes.
"""


def test_parse_sections_splits_at_h2_and_keeps_h3_inside() -> None:
    from severino_vault_mcp.sections import parse_sections

    secs = parse_sections(_MULTISECTION_BODY, body_start_line=5)
    # preamble + two H2s; the H3 stays inside its parent (under the token cap).
    assert [s.slug for s in secs] == [
        "overview",
        "routine-operations",
        "troubleshooting",
    ]
    routine = secs[1]
    assert routine.level == 2
    assert "Backing commands" in routine.body  # H3 folded in, not its own span
    # start_line is the source line of the H2, offset by body_start_line.
    assert routine.start_line == 5 + 2


def test_parse_sections_disambiguates_duplicate_headings() -> None:
    from severino_vault_mcp.sections import parse_sections

    secs = parse_sections("## Notes\n\nfirst\n\n## Notes\n\nsecond\n")
    assert [s.slug for s in secs] == ["notes", "notes-2"]


def test_parse_sections_ignores_headings_inside_code_fences() -> None:
    from severino_vault_mcp.sections import parse_sections

    body = "## Real\n\n```sh\n## not a heading\necho hi\n```\n\nstill real\n"
    secs = parse_sections(body)
    assert [s.slug for s in secs] == ["real"]


def test_parse_sections_subsplits_oversized_h2_at_h3() -> None:
    from severino_vault_mcp.sections import parse_sections

    filler = ("word " * 60 + "\n") * 3  # well over a tiny cap, per H3
    body = f"## Big\n\nlead\n\n### One\n\n{filler}\n### Two\n\n{filler}"
    secs = parse_sections(body, token_cap=40)
    paths = [s.heading_path for s in secs]
    # The oversized H2 split at its H3 boundaries (parts may hard-wrap further).
    assert any(p.startswith("Big > One") for p in paths)
    assert any(p.startswith("Big > Two") for p in paths)
    assert all(s.level in (2, 3) for s in secs)


def test_resolve_section_by_slug_and_heading_path() -> None:
    from severino_vault_mcp.sections import parse_sections, resolve_section

    secs = parse_sections(_MULTISECTION_BODY)
    assert resolve_section(secs, "troubleshooting").heading == "Troubleshooting"
    assert resolve_section(secs, "Routine operations").slug == "routine-operations"
    assert resolve_section(secs, "no-such-section") is None


def test_best_section_picks_the_query_matching_span() -> None:
    from severino_vault_mcp.search import best_section
    from severino_vault_mcp.sections import parse_sections
    from severino_vault_mcp.sensitivity import Sensitivity
    from severino_vault_mcp.vault import Doc

    doc = Doc(
        doc_id="x", title="X", doc_type="runbook", system="", environment="other",
        status="active", sensitivity=Sensitivity.INTERNAL, last_reviewed=None,
        tags=[], related_projects=[], related_assets=[], path=Path("x"),
        relative_path="x.md", body=_MULTISECTION_BODY,
        sections=parse_sections(_MULTISECTION_BODY),
    )
    sec, score = best_section(doc, "resolver latency")
    assert sec.slug == "troubleshooting"
    assert score > 0


def _write_multisection_doc(vault: Path) -> None:
    (vault / "03 Runbooks" / "Backup Ops.md").write_text(
        """---
doc_id: rb-backup-ops
title: Backup Ops
doc_type: runbook
system: Backup
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-05-01
tags: [backup]
---

Overview line before any heading.

## Routine operations

Run the daily job to keep things current.

## Troubleshooting

Check the resolver logs first when latency spikes.
""",
        encoding="utf-8",
    )


def test_read_doc_default_returns_whole_body_unchanged(fake_vault: Path) -> None:
    # Back-compat: the no-section path is byte-identical to before P1.
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("rb-add-nginx-proxy-host")
    assert "body_scope" not in result
    assert "section" not in result
    assert result["body"].startswith("## Goal")


def test_read_doc_section_returns_only_that_span(fake_vault: Path) -> None:
    _write_multisection_doc(fake_vault)
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("rb-backup-ops", section="troubleshooting")
    assert result["body_scope"] == "section"
    assert result["section"] == "troubleshooting"
    assert "resolver logs" in result["body"]
    assert "Routine operations" not in result["body"]


def test_read_doc_unknown_section_lists_available(fake_vault: Path) -> None:
    _write_multisection_doc(fake_vault)
    server = _fresh_module("severino_vault_mcp.server")
    result = server.read_doc("rb-backup-ops", section="nope")
    assert result["body_released"] is False
    assert "body" not in result
    slugs = {s["section"] for s in result["available_sections"]}
    assert {"routine-operations", "troubleshooting"} <= slugs


def test_find_runbook_hit_carries_section_menu(fake_vault: Path) -> None:
    _write_multisection_doc(fake_vault)
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("resolver latency troubleshooting")
    top = result["hits"][0]
    assert top["doc_id"] == "rb-backup-ops"
    assert top["section"] == "troubleshooting"
    assert "body" not in top  # menu line never carries a body
    assert top["section_summary"]


def test_get_runbook_returns_matched_section_body(fake_vault: Path) -> None:
    _write_multisection_doc(fake_vault)
    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_runbook("resolver latency")
    selected = result["selected"]
    assert selected["doc_id"] == "rb-backup-ops"
    assert selected["body_scope"] == "section"
    assert selected["full_body_available"] is True
    assert "resolver logs" in selected["body"]
    assert "Routine operations" not in selected["body"]


def test_get_runbook_metadata_only_match_returns_whole_body(fake_vault: Path) -> None:
    # "backup" hits the tag/system but no section scores it -> keep the full doc
    # so a metadata-only match never drops the part holding the answer.
    _write_multisection_doc(fake_vault)
    server = _fresh_module("severino_vault_mcp.server")
    result = server.get_runbook("backup")
    selected = result["selected"]
    assert selected["doc_id"] == "rb-backup-ops"
    assert selected["body_scope"] == "doc"
    assert "Routine operations" in selected["body"]


# ----- emit-once CLI: find_sections / read_section shared with the MCP --------


def test_find_sections_matches_find_runbook_menu(fake_vault: Path) -> None:
    # Emit-once invariant: the service builds the same hit shape find_runbook
    # renders, so the CLI and MCP can never drift on the menu.
    _write_multisection_doc(fake_vault)
    server = _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader
    from severino_vault_mcp.vault_search_service import find_sections

    query = "resolver latency troubleshooting"
    service = find_sections(VaultLoader(Config.from_env()), query)
    mcp = server.find_runbook(query)
    # find_runbook adds the Quick Index routing hint on top; the menu hits match.
    assert service["hits"] == mcp["hits"]
    assert service["indexed_doc_count"] == mcp["indexed_doc_count"]
    top = service["hits"][0]
    assert top["doc_id"] == "rb-backup-ops"
    assert top["section"] == "troubleshooting"
    assert "body" not in top


def test_read_section_returns_one_span(fake_vault: Path) -> None:
    _write_multisection_doc(fake_vault)
    _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader
    from severino_vault_mcp.vault_search_service import read_section

    loader = VaultLoader(Config.from_env())
    result = read_section(loader, "rb-backup-ops", "troubleshooting")
    assert result["ok"] is True
    assert result["body_scope"] == "section"
    assert result["section"] == "troubleshooting"
    assert "resolver logs" in result["body"]
    assert "Routine operations" not in result["body"]


def test_read_section_whole_body_when_no_section(fake_vault: Path) -> None:
    _write_multisection_doc(fake_vault)
    _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader
    from severino_vault_mcp.vault_search_service import read_section

    result = read_section(VaultLoader(Config.from_env()), "rb-backup-ops")
    assert result["ok"] is True
    assert result["body_scope"] == "doc"
    assert "Routine operations" in result["body"]


def test_read_section_unknown_section_is_not_ok_and_lists_available(
    fake_vault: Path,
) -> None:
    _write_multisection_doc(fake_vault)
    _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader
    from severino_vault_mcp.vault_search_service import read_section

    result = read_section(VaultLoader(Config.from_env()), "rb-backup-ops", "nope")
    assert result["ok"] is False
    assert result["body_released"] is False
    assert "body" not in result
    slugs = {s["section"] for s in result["available_sections"]}
    assert {"routine-operations", "troubleshooting"} <= slugs


def test_read_section_withholds_restricted_without_unlock(fake_vault: Path) -> None:
    # The CLI path never offers the interactive unlock read_doc has — restricted
    # bodies stay withheld, the same as search_body.
    _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader
    from severino_vault_mcp.vault_search_service import read_section

    result = read_section(VaultLoader(Config.from_env()), "infra-local-pki")
    assert result["ok"] is True
    assert result["body_released"] is False
    assert "body" not in result
    assert result["advisory"]


def test_read_section_missing_doc_is_not_ok(fake_vault: Path) -> None:
    _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.config import Config
    from severino_vault_mcp.vault import VaultLoader
    from severino_vault_mcp.vault_search_service import read_section

    result = read_section(VaultLoader(Config.from_env()), "rb-nope")
    assert result["ok"] is False
    assert result["found"] is False


def test_cli_find_and_read_emit_the_menu(fake_vault: Path) -> None:
    # End-to-end: the console subcommands emit the JSON envelope manage-tui reads.
    import json
    import os
    import subprocess
    import sys

    repo_root = Path(__file__).resolve().parents[1]
    env = {
        **os.environ,
        "PYTHONPATH": str(repo_root / "src"),
        "SVMC_VAULT_PATH": str(fake_vault),
        "SVMC_CACHE_SECONDS": "0",
    }

    find = subprocess.run(
        [sys.executable, "-m", "severino_vault_mcp", "find", "nginx proxy"],
        capture_output=True, text=True, check=True, cwd=repo_root, env=env,
    )
    payload = json.loads(find.stdout)
    assert payload["ok"] is True
    assert payload["hits"][0]["doc_id"] == "rb-add-nginx-proxy-host"
    section = payload["hits"][0]["section"]

    read = subprocess.run(
        [
            sys.executable, "-m", "severino_vault_mcp", "read",
            "rb-add-nginx-proxy-host", "--section", section,
        ],
        capture_output=True, text=True, check=True, cwd=repo_root, env=env,
    )
    body = json.loads(read.stdout)
    assert body["ok"] is True
    assert body["body_scope"] == "section"
    assert "Expose an internal service" in body["body"]


# ----- emit-once CLI: describe (the command-surface leg) ----------------------


def test_describe_parser_emits_command_surface() -> None:
    # The 'Code/guards' leg: describe is generated from the same parser that
    # backs --help, so it can never drift from the real command surface.
    from severino_vault_mcp.cli import build_parser
    from severino_vault_mcp.cli_introspect import describe_parser

    surface = describe_parser(build_parser())
    assert surface["name"] == "severino-vault-mcp"
    # Emits the shared v4 contract shape verbatim (so `tools describe --repos`
    # folds it in as a homogeneous sibling): version, tool-level blast radius,
    # and the inventory + prose fields the schema requires (empty here).
    assert surface["schema_version"] == 4
    assert surface["effect"] == "read"
    assert surface["group"] and isinstance(surface["order"], int)
    assert surface["paras"] == [] and surface["examples"] == []
    names = {c["name"] for c in surface["commands"]}
    # find / read / describe itself are all part of the emitted surface.
    assert {"find", "read", "describe", "schema"} <= names
    # --fingerprint is a global option, not a subcommand.
    assert any(o["name"] == "--fingerprint" for o in surface["global_options"])

    # Per-command effect: the writers declare vault_write, readers stay read.
    by_name = {c["name"]: c for c in surface["commands"]}
    assert by_name["touch-reviewed"]["effect"] == "vault_write"
    assert by_name["infra-write"]["effect"] == "vault_write"
    assert by_name["find"]["effect"] == "read"
    assert by_name["read"]["effect"] == "read"

    # Every command carries the schema's required keys, prose included.
    assert all({"args", "effect", "paras", "examples"} <= c.keys() for c in surface["commands"])

    find = next(c for c in surface["commands"] if c["name"] == "find")
    args = {a["name"]: a for a in find["args"]}
    assert args["query"]["positional"] is True
    assert args["query"]["required"] is True
    assert args["--limit"]["takes_value"] is True
    assert args["--pretty"]["takes_value"] is False
    # argparse-only extras (type / default) must not leak past the shared schema.
    assert "default" not in args["--limit"] and "type" not in args["--limit"]
    # -h/--help is argparse boilerplate and must not leak into the surface.
    assert "-h" not in args


def test_describe_conforms_to_cordon_validator() -> None:
    # The conformance gate: pipe the emitted surface through cordon's own
    # canonical validator (cordon-v4.json via conformance/validate.mjs), so we
    # validate against the single source of truth — no schema copy in this repo.
    # Skips when the cordon repo isn't a sibling checkout (set CORDON_HOME), the
    # typical CI-for-this-repo case; the tools repo's `--repos` federation is the
    # always-on cross-repo backstop.
    import os
    import shutil
    import subprocess
    import sys

    repo_root = Path(__file__).resolve().parents[1]
    candidates = []
    if os.environ.get("CORDON_HOME"):
        candidates.append(Path(os.environ["CORDON_HOME"]))
    candidates.append(repo_root.parent / "cordon")
    cordon = next(
        (c for c in candidates if (c / "conformance" / "validate.mjs").exists()), None
    )
    if cordon is None:
        pytest.skip("cordon repo not found as sibling (set CORDON_HOME to enable)")
    if shutil.which("node") is None:
        pytest.skip("node not available")
    if not (cordon / "node_modules").exists() and not (cordon / "node_modules.nosync").exists():
        pytest.skip("cordon dependencies not installed (run `npm ci` in cordon)")

    describe = subprocess.run(
        [sys.executable, "-m", "severino_vault_mcp", "describe"],
        capture_output=True, text=True, check=True, cwd=repo_root,
        env={**os.environ, "PYTHONPATH": str(repo_root / "src")},
    )
    result = subprocess.run(
        ["node", str(cordon / "conformance" / "validate.mjs"), "-"],
        input=describe.stdout, capture_output=True, text=True,
    )
    assert result.returncode == 0, (result.stdout + result.stderr)


def test_cli_describe_emits_json() -> None:
    import json
    import os
    import subprocess
    import sys

    repo_root = Path(__file__).resolve().parents[1]
    proc = subprocess.run(
        [sys.executable, "-m", "severino_vault_mcp", "describe"],
        capture_output=True, text=True, check=True, cwd=repo_root,
        env={**os.environ, "PYTHONPATH": str(repo_root / "src")},
    )
    payload = json.loads(proc.stdout)
    assert payload["ok"] is True
    assert {"find", "read", "describe"} <= {c["name"] for c in payload["commands"]}


def test_mcp_describe_commands_matches_cli(fake_vault: Path) -> None:
    # The MCP tool and the CLI subcommand render the identical surface.
    server = _fresh_module("severino_vault_mcp.server")
    from severino_vault_mcp.cli import build_parser
    from severino_vault_mcp.cli_introspect import describe_parser

    result = server.describe_commands()
    assert result["ok"] is True
    assert {"find", "read", "describe"} <= {c["name"] for c in result["commands"]}
    assert result["commands"] == describe_parser(build_parser())["commands"]


def test_backfill_aliases_sets_title_alias_idempotently(fake_vault: Path) -> None:
    proj = fake_vault / "01 Projects" / "sitedrift"
    proj.mkdir(parents=True)
    (proj / "index.md").write_text(
        "---\n"
        "doc_id: project-sitedrift\n"
        "title: 'sitedrift: DEV/LIVE compare & SEO'\n"
        "doc_type: decision_record\n"
        "system: tools\n"
        "environment: local_mac\n"
        "status: active\n"
        "sensitivity: internal\n"
        "---\n\nbody\n",
        encoding="utf-8",
    )
    vws, loader = _vws_runtime()
    result = vws.backfill_aliases(loader)
    assert result["ok"] is True
    assert "01 Projects/sitedrift/index.md" in result["updated"]

    # The special-char title is YAML-escaped on write, so it round-trips cleanly.
    from severino_vault_mcp.frontmatter import read_frontmatter

    fm = read_frontmatter(proj / "index.md")
    assert fm is not None
    assert fm["aliases"] == ["sitedrift: DEV/LIVE compare & SEO"]

    # Derived from title -> re-running is a no-op (idempotent, drift-repairing).
    vws2, loader2 = _vws_runtime()
    result2 = vws2.backfill_aliases(loader2)
    assert "01 Projects/sitedrift/index.md" not in result2["updated"]
