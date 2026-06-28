"""Tests for the topology inventory reader and its derivations."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from severino_vault_mcp.config import Config
from severino_vault_mcp.labs import topology as topo_mod

SAMPLE = {
    "version": 1,
    "meta": {"title": "Sample Topology"},
    "tailnet": {
        "name": "tail-sample.ts.net",
        "dns_nameserver": "100.64.0.2",
        "dns_nameserver_node": "core",
        "magicdns": True,
        "lock": {"enabled": True, "signing_nodes": ["mac"]},
        "exit_nodes": [{"node": "edge", "upstream": "DigitalOcean", "use": "datacenter IP"}],
        "subnet_routes": [{"node": "router", "cidr": "192.168.1.0/24"}],
    },
    "networks": [
        {"name": "Home LAN", "cidr": "192.168.1.0/24", "gateway": "192.168.1.1", "note": "lan"},
    ],
    "hosts": [
        {
            "id": "core",
            "name": "core-host",
            "role": "Docker host · everything",
            "kind": "vm",
            "os": "Ubuntu",
            "lan_ip": "192.168.1.2",
            "ts_ip": "100.64.0.2",
            "public_ip": None,
            "ssh_alias": "core",
            "ssh_port": 22,
            "tailnet": {"tags": ["tag:homelab", "tag:dns"], "accept_dns": False},
            "containers": [
                {"name": "nginx", "ports": "80, 443", "note": "proxy"},
                {"name": "app", "ports": "8000", "note": "the app"},
            ],
            "hardening": ["systemd-resolved disabled"],
        },
        {
            "id": "edge",
            "name": "edge-vps",
            "role": "Cloud outpost · exit node",
            "kind": "vps",
            "os": "Ubuntu",
            "lan_ip": None,
            "ts_ip": "100.64.0.3",
            "public_ip": "203.0.113.5",
            "ssh_alias": "edge",
            "ssh_port": 7722,
            "tailnet": {"tags": ["tag:server"], "exit_node": True},
            "containers": [],
            "hardening": [],
        },
        {
            "id": "mac",
            "name": "Mac",
            "role": "Admin device · signing node",
            "kind": "laptop",
            "os": "macOS",
            "lan_ip": "192.168.1.50",
            "ts_ip": "100.64.0.9",
            "public_ip": None,
            "ssh_alias": None,
            "ssh_port": None,
            "tailnet": {"group": "group:admins", "signing_node": True},
            "containers": [],
            "hardening": [],
        },
    ],
    "pki": [
        {"issuer": "Local CA", "covers": ".lab certs", "key_location": "offline", "expires": "2036"},
    ],
    "invariants": ["Nothing public except the site."],
}

# The reflected pointer list now comes from the infra-dataset registry, supplied
# to the renderer by the caller — never hand-listed in topology.json.
REFERENCES = (
    {"concept": "DNS rewrites", "doc": "DNS Rewrites — lab", "owner": "adguard guard"},
)


@pytest.fixture
def inventory(tmp_path: Path) -> Path:
    path = tmp_path / "topology.json"
    path.write_text(json.dumps(SAMPLE), encoding="utf-8")
    return path


def test_load_and_summary(inventory: Path) -> None:
    topo = topo_mod.load_topology(inventory)
    summary = topo_mod.to_summary(topo)
    assert summary["version"] == 1
    assert [h["id"] for h in summary["hosts"]] == ["core", "edge", "mac"]
    assert summary["hosts"][0]["containers"][0]["name"] == "nginx"
    assert summary["tailnet"]["name"] == "tail-sample.ts.net"
    assert summary["networks"][0]["cidr"] == "192.168.1.0/24"
    assert "references" not in summary  # owned by the registry, not topology.json


def test_missing_file_raises(tmp_path: Path) -> None:
    with pytest.raises(topo_mod.TopologyError):
        topo_mod.load_topology(tmp_path / "nope.json")


def test_render_tables_contains_facts(inventory: Path) -> None:
    topo = topo_mod.load_topology(inventory)
    tables = topo_mod.render_tables(topo, REFERENCES)
    assert "## Hosts" in tables
    assert "100.64.0.2" in tables
    assert "`nginx`" in tables
    assert "## Networks" in tables
    assert "## Tailnet" in tables
    assert "## Canonical sources (not duplicated here)" in tables
    assert "[[DNS Rewrites — lab]]" in tables  # referenced, not re-rendered
    assert "systemd-resolved disabled" in tables


def test_tables_do_not_contain_dns_rewrite_data(inventory: Path) -> None:
    # Reflected stores are referenced, never copied into the authored view.
    topo = topo_mod.load_topology(inventory)
    tables = topo_mod.render_tables(topo, REFERENCES)
    assert "Resolves To" not in tables  # no DNS-rewrite answer table here


def test_no_references_means_no_canonical_section(inventory: Path) -> None:
    topo = topo_mod.load_topology(inventory)
    assert "Canonical sources" not in topo_mod.render_tables(topo)


def test_doc_roundtrips_check_clean(inventory: Path) -> None:
    topo = topo_mod.load_topology(inventory)
    doc = topo_mod.render_doc(topo, last_reviewed="2026-06-22", references=REFERENCES)
    assert "doc_id: infra-topology" in doc
    assert topo_mod.check_doc(topo, doc, REFERENCES) == []


def test_check_doc_flags_stale_region(inventory: Path) -> None:
    topo = topo_mod.load_topology(inventory)
    doc = topo_mod.render_doc(topo, last_reviewed="2026-06-22", references=REFERENCES)
    tampered = doc.replace("100.64.0.2", "10.0.0.99")
    assert topo_mod.check_doc(topo, tampered, REFERENCES)


def test_figure_excludes_admin_clients_and_anchors_busiest(inventory: Path) -> None:
    topo = topo_mod.load_topology(inventory)
    figure = topo_mod.render_figure(topo)
    ids = [n["id"] for n in figure["nodes"]]
    assert "mac" not in ids  # laptop is inventory, not on the device diagram
    assert set(ids) == {"core", "edge"}
    anchors = [n for n in figure["nodes"] if n.get("role") == "anchor"]
    assert anchors[0]["id"] == "core"
    assert {"from": "edge", "to": "core", "dir": "both"} in figure["links"]


# ── Declared contract (validation on read) ───────────────────────────────────


def test_valid_inventory_has_no_problems() -> None:
    assert topo_mod.validate_inventory(SAMPLE) == []


def test_validate_flags_missing_required_host_field() -> None:
    bad = {"version": 1, "hosts": [{"id": "x", "name": "x", "role": "r"}]}
    problems = topo_mod.validate_inventory(bad)
    assert any("missing required field: kind" in p for p in problems)


def test_validate_flags_bad_kind_and_dupes() -> None:
    bad = {
        "version": 1,
        "hosts": [
            {"id": "a", "name": "a", "role": "r", "kind": "toaster"},
            {"id": "a", "name": "a2", "role": "r", "kind": "vm"},
        ],
    }
    problems = topo_mod.validate_inventory(bad)
    assert any("kind 'toaster'" in p for p in problems)
    assert any("duplicate host ids" in p for p in problems)


def test_validate_flags_missing_top_level() -> None:
    assert any(
        "missing top-level key: hosts" in p
        for p in topo_mod.validate_inventory({"version": 1})
    )


def test_load_rejects_off_contract(tmp_path: Path) -> None:
    path = tmp_path / "topology.json"
    path.write_text(json.dumps({"version": 1, "hosts": "nope"}), encoding="utf-8")
    with pytest.raises(topo_mod.TopologyError, match="off-contract"):
        topo_mod.load_topology(path)


def test_inventory_schema_shape() -> None:
    schema = topo_mod.inventory_schema()
    assert schema["required_host_fields"] == ["id", "name", "role", "kind"]
    assert "vm" in schema["host_kinds"]
    assert "laptop" in schema["host_kinds"]


def test_get_topology_envelope(tmp_path: Path, monkeypatch) -> None:
    path = tmp_path / "topology.json"
    path.write_text(json.dumps(SAMPLE), encoding="utf-8")
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SVMC_TOPOLOGY_PATH", str(path))
    config = Config.from_env()
    result = topo_mod.get_topology(config)
    assert result["ok"] is True
    assert result["hosts"][0]["id"] == "core"


def test_get_topology_missing_is_error(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SVMC_TOPOLOGY_PATH", str(tmp_path / "absent.json"))
    config = Config.from_env()
    result = topo_mod.get_topology(config)
    assert result["ok"] is False
    assert "not found" in result["error"]


# ── Validated write path (topology-write) ────────────────────────────────────


def _topology_config(tmp_path: Path, monkeypatch) -> Config:
    path = tmp_path / "topology.json"
    path.write_text(json.dumps(SAMPLE), encoding="utf-8")
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path))
    monkeypatch.setenv("SVMC_TOPOLOGY_PATH", str(path))
    return Config.from_env()


def test_write_topology_regenerates_doc_and_figure(tmp_path: Path, monkeypatch) -> None:
    config = _topology_config(tmp_path, monkeypatch)
    result = topo_mod.write_topology(config)
    assert result["ok"] is True
    assert result["source_updated"] is False
    assert result["hosts"] == 3
    doc = tmp_path / "Topology.md"
    figure = tmp_path / "topology.figure.json"
    assert doc.exists() and figure.exists()
    # the regenerated doc passes its own parity check
    topo = topo_mod.load_topology(tmp_path / "topology.json")
    assert topo_mod.check_doc(topo, doc.read_text(encoding="utf-8")) == []


def test_write_topology_replace_refuses_off_contract(tmp_path: Path, monkeypatch) -> None:
    config = _topology_config(tmp_path, monkeypatch)
    result = topo_mod.write_topology(config, json.dumps({"version": 1, "hosts": "nope"}))
    assert result["ok"] is False
    # the bad payload never reached topology.json
    assert isinstance(json.loads((tmp_path / "topology.json").read_text())["hosts"], list)


def test_write_topology_replace_writes_validated(tmp_path: Path, monkeypatch) -> None:
    config = _topology_config(tmp_path, monkeypatch)
    smaller = {**SAMPLE, "hosts": SAMPLE["hosts"][:1]}
    result = topo_mod.write_topology(config, json.dumps(smaller))
    assert result["ok"] is True
    assert result["source_updated"] is True
    assert result["hosts"] == 1
    assert len(json.loads((tmp_path / "topology.json").read_text())["hosts"]) == 1
