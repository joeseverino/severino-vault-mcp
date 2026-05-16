"""Tests for the vault loader, search ranker, and write tools.

These don't depend on a running MCP host — each test sets up a tiny fake
vault on disk and exercises the loader / tools directly.
"""

from __future__ import annotations

from pathlib import Path

import pytest


@pytest.fixture
def fake_vault(tmp_path: Path, monkeypatch) -> Path:
    """Spin up a 3-doc fake vault."""
    (tmp_path / "01 Projects").mkdir()
    (tmp_path / "02 Infrastructure").mkdir()
    (tmp_path / "03 Runbooks").mkdir()

    (tmp_path / "03 Runbooks" / "Add Nginx Proxy Host.md").write_text(
        """---
doc_id: rb-add-nginx-proxy-host
title: Add Nginx Proxy Host
doc_type: runbook
system: Nginx Proxy Manager
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2025-01-01
related_projects: []
related_assets: []
tags:
  - nginx
  - homelab
---

## Goal

Expose an internal homelab service over HTTPS via NPM.
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
sensitivity: secret_adjacent
last_reviewed: 2026-04-01
tags: [pki, ca]
---

# Local PKI

CA private key lives offline.
""",
        encoding="utf-8",
    )

    (tmp_path / "01 Projects" / "untagged.md").write_text(
        "# Untagged\n\nNo frontmatter yet.\n",
        encoding="utf-8",
    )

    monkeypatch.setenv("SKR_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SKR_CACHE_SECONDS", "0")
    return tmp_path


def _fresh_module(name: str):
    """Import the module after env vars are set so module-level Config is fresh."""
    import importlib
    import sys
    for mod in list(sys.modules):
        if mod.startswith("severino_knowledge_router"):
            del sys.modules[mod]
    return importlib.import_module(name)


def test_loader_indexes_only_tagged_docs(fake_vault: Path) -> None:
    vault_mod = _fresh_module("severino_knowledge_router.vault")
    from severino_knowledge_router.config import Config
    loader = vault_mod.VaultLoader(Config.from_env())
    idx = loader.index()
    doc_ids = {d.doc_id for d in idx.docs}
    assert doc_ids == {"rb-add-nginx-proxy-host", "infra-local-pki"}


def test_find_runbook_ranks_nginx_query(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    result = server.find_runbook("nginx proxy")
    assert result["hits"], result
    assert result["hits"][0]["doc_id"] == "rb-add-nginx-proxy-host"


def test_read_doc_default_refuses_secret_adjacent(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    result = server.read_doc("infra-local-pki")
    assert result["found"] is True
    assert result["body_released"] is False
    assert "body" not in result
    assert "secret_adjacent" in result["advisory"].lower()


def test_read_doc_overrides_secret_adjacent(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    result = server.read_doc("infra-local-pki", include_secret_adjacent=True)
    assert result["body_released"] is True
    assert "CA private key" in result["body"]
    assert result["override_used"] is True
    assert "override" in result["advisory"].lower()


def test_read_doc_returns_body_for_internal(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    result = server.read_doc("rb-add-nginx-proxy-host")
    assert result["body_released"] is True
    assert "## Goal" in result["body"]


def test_read_doc_releases_sensitive_with_advisory(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        sensitivity="sensitive",
    )
    result = server.read_doc("rb-add-nginx-proxy-host")
    assert result["body_released"] is True
    assert "## Goal" in result["body"]
    assert "sensitive" in result["advisory"].lower()


def test_add_frontmatter_validates_enums(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    result = server.add_frontmatter(
        relative_path="01 Projects/untagged.md",
        doc_id="bad-prefix-foo",
        title="Foo",
        doc_type="runbook",
        system="Foo",
    )
    assert result["ok"] is False
    assert any("doc_id" in e for e in result["errors"])


def test_add_frontmatter_writes(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
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
    server = _fresh_module("severino_knowledge_router.server")
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
    server = _fresh_module("severino_knowledge_router.server")
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
    server = _fresh_module("severino_knowledge_router.server")
    result = server.update_frontmatter(
        relative_path="01 Projects/untagged.md",
        status="active",
    )
    assert result["ok"] is False
    assert "no frontmatter" in result["errors"][0].lower()


def test_search_body_finds_text_in_body(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    result = server.search_body("HTTPS via NPM")
    doc_ids = [h["doc_id"] for h in result["hits_by_doc"]]
    assert "rb-add-nginx-proxy-host" in doc_ids


def test_search_body_excludes_secret_adjacent_by_default(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    default = server.search_body("CA private key")
    assert default["doc_count"] == 0
    assert default["excluded"]["secret_adjacent_skipped"] >= 1

    overridden = server.search_body("CA private key", include_secret_adjacent=True)
    assert overridden["doc_count"] == 1
    assert overridden["hits_by_doc"][0]["doc_id"] == "infra-local-pki"


def test_search_body_skips_frontmatter_hits(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    # "nginx" appears in the nginx runbook frontmatter tags AND in the body.
    # The frontmatter hit should be excluded; the body hit should remain.
    result = server.search_body("nginx")
    for hit in result["hits_by_doc"]:
        for snip in hit["snippets"]:
            # Frontmatter spans the first ~16 lines of this fixture doc.
            assert snip["line_number"] >= 17, (hit["doc_id"], snip)


def test_inventory_for_project_filters_by_slug(fake_vault: Path) -> None:
    server = _fresh_module("severino_knowledge_router.server")
    # Tag the nginx runbook with a related project, then look it up.
    server.update_frontmatter(
        relative_path="03 Runbooks/Add Nginx Proxy Host.md",
        add_related_projects=["homelab-dns"],
    )
    result = server.inventory_for_project("homelab-dns")
    assert result["match_count"] == 1
    assert "runbook" in result["by_doc_type"]
