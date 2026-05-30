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


def _fresh_module(name: str):
    """Import the module after env vars are set so module-level Config is fresh."""
    import importlib
    import sys
    for mod in list(sys.modules):
        if mod.startswith("severino_vault_mcp"):
            del sys.modules[mod]
    return importlib.import_module(name)


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


def test_find_runbook_ranks_nginx_query(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.find_runbook("nginx proxy")
    assert result["hits"], result
    assert result["hits"][0]["doc_id"] == "rb-add-nginx-proxy-host"


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
    assert any("doc_id" in e for e in result["errors"])


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
    assert "already starts with" in result["errors"][0]


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


def test_update_frontmatter_refuses_without_frontmatter(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.update_frontmatter(
        relative_path="01 Projects/untagged.md",
        status="active",
    )
    assert result["ok"] is False
    assert "no frontmatter" in result["errors"][0].lower()


def test_search_body_finds_text_in_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    result = server.search_body("HTTPS via NPM")
    doc_ids = [h["doc_id"] for h in result["hits_by_doc"]]
    assert "rb-add-nginx-proxy-host" in doc_ids


def test_search_body_excludes_secret_adjacent_by_default(fake_vault: Path) -> None:
    server = _fresh_module("severino_vault_mcp.server")
    default = server.search_body("CA private key")
    assert default["doc_count"] == 0
    assert default["excluded"]["restricted_skipped"] >= 1

    overridden = server.search_body("CA private key", include_restricted=True)
    assert overridden["doc_count"] == 0
    assert overridden["excluded"]["restricted_skipped"] >= 1


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
