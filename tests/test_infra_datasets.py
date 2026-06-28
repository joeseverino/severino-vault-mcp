"""Tests for the infra-dataset registry: one catalog, uniform reads."""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from vault_engine.config import Config

from severino_vault_mcp.labs import infra_datasets

REGISTRY = {
    "version": 1,
    "datasets": [
        {
            "id": "topology",
            "kind": "authored",
            "title": "Topology inventory",
            "owner": "topology.json",
            "sensitivity": "internal",
            "doc": "Topology",
            "source": {"type": "file", "path": "02 Infrastructure/Topology/topology.json"},
        },
        {
            "id": "dns_rewrites",
            "kind": "reflected",
            "title": "AdGuard DNS rewrites",
            "owner": "adguard guard",
            "sensitivity": "sensitive",
            "fetcher": "adguard",
            "doc": "DNS Rewrites — lab",
            "source": {"type": "file", "path": "02 Infrastructure/AdGuard/dns-rewrites.json"},
        },
        {
            "id": "secrets_like",
            "kind": "reflected",
            "title": "Restricted thing",
            "owner": "x",
            "sensitivity": "restricted",
            "doc": "Restricted",
            "source": {"type": "file", "path": "02 Infrastructure/Topology/topology.json"},
        },
        {
            "id": "vps_ingress",
            "kind": "reflected",
            "title": "VPS ingress",
            "owner": "Caddyfile",
            "sensitivity": "internal",
            "doc": "Caddy",
            "source": {"type": "doc"},
        },
        {
            "id": "writable",
            "kind": "reflected",
            "title": "Writable dataset",
            "owner": "guard",
            "sensitivity": "internal",
            "doc": "Writable",
            "source": {"type": "file", "path": "02 Infrastructure/AdGuard/writable.json"},
            "render": {
                "doc_path": "02 Infrastructure/AdGuard/Writable.md",
                "columns": [
                    {"label": "Domain", "key": "domain"},
                    {"label": "Answer", "key": "answer"},
                ],
            },
        },
    ],
}

REWRITES = [{"answer": "192.168.1.233", "domain": "homelab"}]


@pytest.fixture
def config(tmp_path: Path, monkeypatch) -> Config:
    (tmp_path / "02 Infrastructure" / "Topology").mkdir(parents=True)
    (tmp_path / "02 Infrastructure" / "AdGuard").mkdir(parents=True)
    (tmp_path / "02 Infrastructure" / "_infra-datasets.json").write_text(
        json.dumps(REGISTRY), encoding="utf-8"
    )
    (tmp_path / "02 Infrastructure" / "Topology" / "topology.json").write_text(
        json.dumps({"version": 1, "hosts": []}), encoding="utf-8"
    )
    (tmp_path / "02 Infrastructure" / "AdGuard" / "dns-rewrites.json").write_text(
        json.dumps(REWRITES), encoding="utf-8"
    )
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    return Config.from_env()


def test_list_datasets_catalog(config: Config) -> None:
    result = infra_datasets.list_datasets(config)
    assert result["ok"] is True
    by_id = {d["id"]: d for d in result["datasets"]}
    assert by_id["dns_rewrites"]["kind"] == "reflected"
    assert by_id["dns_rewrites"]["readable"] is True
    assert by_id["dns_rewrites"]["refreshable"] is True
    assert by_id["dns_rewrites"]["sensitivity"] == "sensitive"
    assert by_id["vps_ingress"]["readable"] is False  # doc-only reference
    assert by_id["topology"]["refreshable"] is False  # no fetcher


def test_read_file_dataset_has_freshness(config: Config) -> None:
    result = infra_datasets.read_dataset(config, "topology")
    assert result["ok"] is True
    assert result["data"]["version"] == 1
    assert result["live"] is False
    assert result["fetched_at"]  # mtime-derived freshness


def test_read_reflected_file_dataset(config: Config) -> None:
    result = infra_datasets.read_dataset(config, "dns_rewrites")
    assert result["ok"] is True
    assert result["data"] == REWRITES
    assert "advisory" in result  # sensitive → handle-carefully note


def test_refresh_returns_live_when_fetcher_succeeds(config: Config, monkeypatch) -> None:
    live = [{"answer": "9.9.9.9", "domain": "fresh"}]
    monkeypatch.setattr(infra_datasets, "_fetch_live", lambda f: (live, None))
    result = infra_datasets.read_dataset(config, "dns_rewrites", refresh=True)
    assert result["ok"] is True
    assert result["live"] is True
    assert result["data"] == live
    assert result["fetched_at"] == "live"


def test_refresh_falls_back_to_cache_when_unreachable(config: Config, monkeypatch) -> None:
    # The NPM-down case: live fetch fails → serve the cache, flagged stale.
    monkeypatch.setattr(infra_datasets, "_fetch_live", lambda f: (None, "unreachable"))
    result = infra_datasets.read_dataset(config, "dns_rewrites", refresh=True)
    assert result["ok"] is True
    assert result["live"] is False
    assert result["stale"] is True
    assert result["refresh_error"] == "unreachable"
    assert result["data"] == REWRITES  # cache still answers


def test_restricted_is_withheld(config: Config) -> None:
    # restricted datasets are withheld; releasing one would need the same
    # interactive local unlock as read_doc, which this read path does not grant.
    result = infra_datasets.read_dataset(config, "secrets_like")
    assert result["ok"] is True
    assert result["withheld"] is True
    assert "data" not in result
    assert "advisory" in result


def test_reference_only_dataset_points_at_doc(config: Config) -> None:
    result = infra_datasets.read_dataset(config, "vps_ingress")
    assert result["ok"] is False
    assert "reference-only" in result["error"]


def test_unknown_dataset_lists_known(config: Config) -> None:
    result = infra_datasets.read_dataset(config, "nope")
    assert result["ok"] is False
    assert "unknown dataset" in result["error"]
    assert "dns_rewrites" in result["error"]


def test_reflected_references_for_topology(config: Config) -> None:
    refs = infra_datasets.reflected_references(config)
    concepts = {r["concept"] for r in refs}
    assert "AdGuard DNS rewrites" in concepts
    assert "Topology inventory" not in concepts  # authored, not reflected


def test_missing_registry_is_error(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    result = infra_datasets.list_datasets(Config.from_env())
    assert result["ok"] is False


def test_fetch_live_rejects_unsafe_fetcher() -> None:
    data, err = infra_datasets._fetch_live("evil; rm -rf /")
    assert data is None
    assert "invalid fetcher" in err


# ── Write side (the canonical pull write) ────────────────────────────────────


def _writable_doc(tmp_path: Path) -> Path:
    doc = tmp_path / "02 Infrastructure" / "AdGuard" / "Writable.md"
    doc.write_text(
        "---\ndoc_id: infra-writable\nlast_reviewed: 2020-01-01\n---\n\n"
        "# Writable\n\nProse stays.\n\n"
        "<!-- INFRA-DATA:BEGIN writable (generated — do not edit) -->\n"
        "old content\n"
        "<!-- INFRA-DATA:END writable -->\n\nMore prose.\n",
        encoding="utf-8",
    )
    return doc


def test_write_dataset_writes_json_and_regenerates_doc(config: Config, tmp_path: Path) -> None:
    doc = _writable_doc(tmp_path)
    payload = json.dumps([{"answer": "1.2.3.4", "domain": "a.lab"}])
    result = infra_datasets.write_dataset(config, "writable", payload)

    assert result["ok"] is True
    assert result["records"] == 1
    assert result["doc_updated"].endswith("Writable.md")
    # cache file written
    cache = tmp_path / "02 Infrastructure" / "AdGuard" / "writable.json"
    assert json.loads(cache.read_text()) == [{"answer": "1.2.3.4", "domain": "a.lab"}]
    # doc region regenerated from the json; prose preserved; last_reviewed stamped
    text = doc.read_text()
    assert "| a.lab | 1.2.3.4 |" in text
    assert "old content" not in text
    assert "Prose stays." in text
    assert "last_reviewed: 2020-01-01" not in text


def test_write_dataset_rejects_bad_json(config: Config) -> None:
    result = infra_datasets.write_dataset(config, "writable", "{not json}")
    assert result["ok"] is False


def test_write_dataset_unknown_id(config: Config) -> None:
    result = infra_datasets.write_dataset(config, "nope", "[]")
    assert result["ok"] is False
    assert "unknown dataset" in result["error"]


def test_write_dataset_warns_on_missing_region(config: Config, tmp_path: Path) -> None:
    # Doc exists but has no INFRA-DATA region → json still written, doc flagged.
    (tmp_path / "02 Infrastructure" / "AdGuard" / "Writable.md").write_text(
        "---\ndoc_id: x\n---\n\n# Writable\n\nno markers\n", encoding="utf-8"
    )
    result = infra_datasets.write_dataset(config, "writable", json.dumps([{"domain": "d", "answer": "a"}]))
    assert result["ok"] is True
    assert "doc_warning" in result
