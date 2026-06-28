"""Topology inventory: the single source of truth for *authored* infra state.

`<vault>/02 Infrastructure/Topology/topology.json` holds the node inventory,
networks, tailnet structure, containers, and PKI — the facts a human declares,
which have no upstream system to read from. This module is the one reader/derive
layer for it: `Topology.md`, the `brand figure` device diagram, the prose
Network Topology block in AGENTS.md, and (via `hq sync`) the HQ asset registry
all derive from this inventory.

Facts that *do* have an upstream owner — DNS rewrites (AdGuard), NPM proxy
hosts, the Tailscale ACL — are *reflected* stores: their SSOT is the live
system, mirrored into their own docs by the drift guards. This inventory only
**references** them (see `references`); it never re-authors them. Authored vs
reflected is the line that keeps a fact owned in exactly one place.

Stored as JSON (parsed via :mod:`jsonio`, no new dependency). Edit through
`severino-vault-mcp topology-write` (validate + regenerate); treat `Topology.md`
as a build artifact.
"""

from __future__ import annotations

import datetime
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING, Any

from .. import jsonio
from ..atomic_write import atomic_write_text

if TYPE_CHECKING:
    from ..config import Config

# The generated tables in Topology.md live between these markers, so the doc
# keeps a stable frontmatter + heading shell while `--check-doc` compares only
# the derived region (the same section-scoped contract as the drift guards).
TABLE_BEGIN = "<!-- TOPOLOGY:BEGIN (generated from topology.json — do not edit by hand) -->"
TABLE_END = "<!-- TOPOLOGY:END -->"

# ── The declared inventory contract ──────────────────────────────────────────
# One source of truth for the inventory shape, mirroring schema.py's role for
# frontmatter: validated on every read and emitted (`topology --emit schema`)
# so HQ's importer and any other consumer validate against the same definition.
REQUIRED_TOP_LEVEL = ("version", "hosts")
REQUIRED_HOST_FIELDS = ("id", "name", "role", "kind")
HOST_KINDS = ("physical", "vm", "vps", "laptop", "mobile")
# Kinds that belong on the device diagram (infrastructure, not admin clients).
DIAGRAM_KINDS = ("physical", "vm", "vps")

_ICON_BY_KIND = {
    "physical": "router",
    "vm": "server",
    "vps": "cloud",
    "laptop": "laptop",
    "mobile": "phone",
}

# Deterministic compass placement for the derived star diagram.
_COMPASS = ("nw", "e", "s", "w", "ne", "se", "sw", "n")


# ── Typed model ──────────────────────────────────────────────────────────────


@dataclass(frozen=True)
class Container:
    name: str
    ports: str
    note: str

    @classmethod
    def from_dict(cls, data: dict) -> Container:
        return cls(
            name=str(data.get("name", "")),
            ports=str(data.get("ports", "")),
            note=str(data.get("note", "")),
        )


@dataclass(frozen=True)
class Host:
    id: str
    name: str
    role: str
    kind: str
    os: str
    lan_ip: str | None
    ts_ip: str | None
    public_ip: str | None
    ssh_alias: str | None
    ssh_port: int | None
    tailnet: dict | None
    containers: tuple[Container, ...]
    hardening: tuple[str, ...]

    @classmethod
    def from_dict(cls, data: dict) -> Host:
        return cls(
            id=str(data.get("id", "")),
            name=str(data.get("name", "")),
            role=str(data.get("role", "")),
            kind=str(data.get("kind", "")),
            os=str(data.get("os", "")),
            lan_ip=data.get("lan_ip"),
            ts_ip=data.get("ts_ip"),
            public_ip=data.get("public_ip"),
            ssh_alias=data.get("ssh_alias"),
            ssh_port=data.get("ssh_port"),
            tailnet=data.get("tailnet"),
            containers=tuple(
                Container.from_dict(c) for c in (data.get("containers") or [])
            ),
            hardening=tuple(data.get("hardening") or []),
        )

    def icon(self) -> str:
        return _ICON_BY_KIND.get(self.kind, "server")

    def tailnet_label(self) -> str:
        """One-cell summary of tailnet membership for the hosts table."""
        if not self.tailnet:
            return "—"
        tags = self.tailnet.get("tags")
        if tags:
            return " ".join(f"`{t}`" for t in tags)
        group = self.tailnet.get("group")
        return f"`{group}`" if group else "—"


@dataclass(frozen=True)
class Topology:
    version: int
    meta: dict
    hosts: tuple[Host, ...]
    tailnet: dict
    networks: tuple[dict, ...]
    pki: tuple[dict, ...]
    invariants: tuple[str, ...]
    raw: dict = field(default_factory=dict, repr=False)

    @classmethod
    def from_dict(cls, data: dict) -> Topology:
        return cls(
            version=int(data.get("version", 1)),
            meta=dict(data.get("meta") or {}),
            hosts=tuple(Host.from_dict(h) for h in (data.get("hosts") or [])),
            tailnet=dict(data.get("tailnet") or {}),
            networks=tuple(data.get("networks") or []),
            pki=tuple(data.get("pki") or []),
            invariants=tuple(data.get("invariants") or []),
            raw=data,
        )


class TopologyError(ValueError):
    """Raised when the inventory file is missing, malformed, or off-contract."""


def inventory_schema() -> dict[str, list[str]]:
    """The declared contract as a JSON-serializable dict (the emit face)."""
    return {
        "required_top_level": list(REQUIRED_TOP_LEVEL),
        "required_host_fields": list(REQUIRED_HOST_FIELDS),
        "host_kinds": list(HOST_KINDS),
    }


def validate_inventory(data: object) -> list[str]:
    """Check a parsed inventory against the contract; empty list means valid."""
    if not isinstance(data, dict):
        return ["inventory must be a JSON object"]

    problems: list[str] = []
    for key in REQUIRED_TOP_LEVEL:
        if key not in data:
            problems.append(f"missing top-level key: {key}")

    hosts = data.get("hosts")
    if not isinstance(hosts, list):
        problems.append("hosts must be a list")
    else:
        seen: list[str] = []
        for i, host in enumerate(hosts):
            if not isinstance(host, dict):
                problems.append(f"hosts[{i}] must be an object")
                continue
            for required in REQUIRED_HOST_FIELDS:
                if not host.get(required):
                    problems.append(f"hosts[{i}] missing required field: {required}")
            kind = host.get("kind")
            if kind and kind not in HOST_KINDS:
                problems.append(f"hosts[{i}].kind {kind!r} not in {list(HOST_KINDS)}")
            host_id = host.get("id")
            if host_id:
                seen.append(host_id)
        dupes = sorted({h for h in seen if seen.count(h) > 1})
        if dupes:
            problems.append(f"duplicate host ids: {dupes}")

    for field_name in ("networks", "pki", "references", "invariants"):
        value = data.get(field_name)
        if value is not None and not isinstance(value, list):
            problems.append(f"{field_name} must be a list when present")
    if (tailnet := data.get("tailnet")) is not None and not isinstance(tailnet, dict):
        problems.append("tailnet must be an object when present")

    return problems


def load_topology(path: Path) -> Topology:
    """Parse + validate the inventory JSON into a typed :class:`Topology`.

    Raises :class:`TopologyError` on a missing file, a JSON parse failure, or a
    contract violation, so callers return one consistent
    ``{"ok": False, "error": ...}`` envelope and a malformed inventory can
    never reach a consumer.
    """
    if not path.is_file():
        raise TopologyError(f"topology inventory not found: {path}")
    try:
        data = jsonio.load_file(path, source="topology inventory")
    except jsonio.JsonError as exc:
        raise TopologyError(str(exc)) from exc
    problems = validate_inventory(data)
    if problems:
        raise TopologyError("topology inventory is off-contract: " + "; ".join(problems))
    return Topology.from_dict(data)


# ── Derivations ──────────────────────────────────────────────────────────────


def to_summary(topo: Topology) -> dict:
    """Structured payload for the MCP tool / CLI JSON face (AI grounding)."""
    return {
        "version": topo.version,
        "tailnet": topo.tailnet,
        "networks": list(topo.networks),
        "hosts": [
            {
                "id": h.id,
                "name": h.name,
                "role": h.role,
                "kind": h.kind,
                "os": h.os,
                "lan_ip": h.lan_ip,
                "ts_ip": h.ts_ip,
                "public_ip": h.public_ip,
                "ssh_alias": h.ssh_alias,
                "ssh_port": h.ssh_port,
                "tailnet": h.tailnet,
                "containers": [
                    {"name": c.name, "ports": c.ports, "note": c.note}
                    for c in h.containers
                ],
                "hardening": list(h.hardening),
            }
            for h in topo.hosts
        ],
        "pki": list(topo.pki),
        "invariants": list(topo.invariants),
    }


def _cell(value: Any) -> str:
    return "—" if value in (None, "") else str(value)


def _ssh_cell(host: Host) -> str:
    if host.ssh_alias and host.ssh_port:
        return f"`{host.ssh_alias}` :{host.ssh_port}"
    if host.ssh_alias:
        return f"`{host.ssh_alias}`"
    return "—"


def render_tables(topo: Topology, references: tuple[dict, ...] = ()) -> str:
    """Render the human-readable markdown (the Topology.md body region).

    ``references`` is the reflected-dataset pointer list, supplied by the caller
    from the infra-dataset registry — the canonical-sources section derives from
    that one catalog, not from a list hand-copied into topology.json.
    """
    out: list[str] = []

    # ── Hosts ──
    out += ["## Hosts", ""]
    out += ["| Host | Role | LAN IP | Tailscale IP | Public IP | SSH | Tailnet |"]
    out += ["|---|---|---|---|---|---|---|"]
    for h in topo.hosts:
        out += [
            f"| {h.name} | {h.role} | {_cell(h.lan_ip)} | {_cell(h.ts_ip)} "
            f"| {_cell(h.public_ip)} | {_ssh_cell(h)} | {h.tailnet_label()} |"
        ]
    out += [""]

    # ── Networks ──
    if topo.networks:
        out += ["## Networks", ""]
        out += ["| Network | CIDR | Gateway | Notes |", "|---|---|---|---|"]
        for n in topo.networks:
            out += [
                f"| {_cell(n.get('name'))} | `{_cell(n.get('cidr'))}` "
                f"| {_cell(n.get('gateway'))} | {_cell(n.get('note'))} |"
            ]
        out += [""]

    # ── Tailnet ──
    if topo.tailnet:
        t = topo.tailnet
        out += ["## Tailnet", ""]
        out += [f"- **Name:** `{_cell(t.get('name'))}`"]
        out += [
            f"- **DNS nameserver:** `{_cell(t.get('dns_nameserver'))}`"
            f" ({_cell(t.get('dns_nameserver_node'))}); MagicDNS"
            f" {'on' if t.get('magicdns') else 'off'}"
        ]
        lock = t.get("lock") or {}
        if lock.get("enabled"):
            signers = ", ".join(lock.get("signing_nodes") or [])
            out += [f"- **Tailnet lock:** enabled · signing nodes: {signers}"]
        if t.get("exit_nodes"):
            out += ["", "**Exit nodes**", "", "| Node | Upstream | Use |", "|---|---|---|"]
            for e in t["exit_nodes"]:
                out += [
                    f"| {_cell(e.get('node'))} | {_cell(e.get('upstream'))} "
                    f"| {_cell(e.get('use'))} |"
                ]
        if t.get("subnet_routes"):
            routes = "; ".join(
                f"`{r.get('cidr')}` via {r.get('node')}" for r in t["subnet_routes"]
            )
            out += ["", f"- **Subnet routes:** {routes}"]
        out += [""]

    # ── Containers ──
    out += ["## Containers", ""]
    for h in topo.hosts:
        if not h.containers:
            continue
        out += [f"### {h.name} (`{_cell(h.lan_ip or h.ts_ip)}`)", ""]
        out += ["| Container | Ports | Notes |", "|---|---|---|"]
        for c in h.containers:
            out += [f"| `{c.name}` | {c.ports} | {c.note} |"]
        out += [""]

    # ── PKI ──
    if topo.pki:
        out += ["## PKI / TLS", ""]
        out += ["| Issuer | Covers | Key location | Expires |", "|---|---|---|---|"]
        for p in topo.pki:
            out += [
                f"| {_cell(p.get('issuer'))} | {_cell(p.get('covers'))} "
                f"| {_cell(p.get('key_location'))} | {_cell(p.get('expires'))} |"
            ]
        out += [""]

    # ── Canonical sources (reflected stores — owned elsewhere) ──
    if references:
        out += ["## Canonical sources (not duplicated here)", ""]
        out += [
            "These facts are *reflected* state — their source of truth is the live",
            "system, mirrored by a drift guard into its own doc. The MCP reads them",
            "via `get_infra_dataset`; this inventory never re-authors them.",
            "",
            "| Concept | Source | Owner |",
            "|---|---|---|",
        ]
        for r in references:
            out += [
                f"| {_cell(r.get('concept'))} | [[{_cell(r.get('doc'))}]] "
                f"| {_cell(r.get('owner'))} |"
            ]
        out += [""]

    # ── Access & invariants ──
    hardening_lines = [(h.name, line) for h in topo.hosts for line in h.hardening]
    if hardening_lines or topo.invariants:
        out += ["## Access & invariants", ""]
        for inv in topo.invariants:
            out += [f"- {inv}"]
        for name, line in hardening_lines:
            out += [f"- **{name}** — {line}"]

    return "\n".join(out).rstrip() + "\n" if out else ""


def render_doc(
    topo: Topology, *, last_reviewed: str, references: tuple[dict, ...] = ()
) -> str:
    """Render the full, frontmatter'd `Topology.md` build artifact."""
    title = str(topo.meta.get("title", "Severino Labs Topology"))
    body = render_tables(topo, references).rstrip("\n")
    return (
        "---\n"
        "doc_id: infra-topology\n"
        f"title: {title}\n"
        "doc_type: architecture_note\n"
        "system: Network topology\n"
        "environment: homelab\n"
        "status: active\n"
        "sensitivity: sensitive\n"
        f"last_reviewed: {last_reviewed}\n"
        "related_projects: []\n"
        "related_assets: []\n"
        "tags: [topology, infrastructure, ssot]\n"
        "---\n"
        "\n"
        f"# {title}\n"
        "\n"
        "> Generated from [`topology.json`](./topology.json) by "
        "`severino-vault-mcp topology`. Do not edit below the marker — edit the "
        "inventory and regenerate. Parity is enforced by `topology --check-doc`.\n"
        "\n"
        f"{TABLE_BEGIN}\n"
        f"{body}\n"
        f"{TABLE_END}\n"
    )


def extract_generated_region(doc_text: str) -> str | None:
    """Return the text between the TOPOLOGY markers, or None if absent."""
    start = doc_text.find(TABLE_BEGIN)
    end = doc_text.find(TABLE_END)
    if start == -1 or end == -1 or end < start:
        return None
    return doc_text[start + len(TABLE_BEGIN) : end].strip("\n")


def check_doc(
    topo: Topology, doc_text: str, references: tuple[dict, ...] = ()
) -> list[str]:
    """Report drift between the inventory and a rendered Topology.md."""
    region = extract_generated_region(doc_text)
    if region is None:
        return ["Topology.md is missing the TOPOLOGY:BEGIN/END generated region"]
    if region.strip() != render_tables(topo, references).strip():
        return ["Topology.md generated region is stale — regenerate from topology.json"]
    return []


def render_figure(topo: Topology) -> dict:
    """Derive a `brand figure` device diagram from the infrastructure hosts.

    Only infrastructure kinds appear (admin laptops/phones are inventory, not
    the device topology). The host with the most containers anchors the star;
    the rest take compass spokes in declared order.
    """
    infra = [h for h in topo.hosts if h.kind in DIAGRAM_KINDS]
    if not infra:
        return {"template": "topology", "theme": "light", "layout": "star", "nodes": [], "links": []}

    center = max(infra, key=lambda h: len(h.containers))
    spokes = [h for h in infra if h.id != center.id]

    nodes = [
        {
            "id": center.id,
            "icon": center.icon(),
            "label": f"{center.name}\n{center.role.split('·')[0].strip()}",
            "role": "anchor",
            "pos": "center",
        }
    ]
    for host, pos in zip(spokes, _COMPASS, strict=False):
        nodes.append(
            {
                "id": host.id,
                "icon": host.icon(),
                "label": f"{host.name}\n{host.role.split('·')[0].strip()}",
                "pos": pos,
            }
        )

    links = [
        {"from": host.id, "to": center.id, "dir": "both"}
        for host in spokes
        if host.ts_ip or host.lan_ip
    ]
    return {
        "template": "topology",
        "theme": "light",
        "layout": "star",
        "nodes": nodes,
        "links": links,
    }


# ── Config-bound service face (shared by the MCP tool and the CLI) ────────────


def get_topology(config: Config) -> dict:
    """Summary envelope for the `get_topology` MCP tool and `topology` CLI."""
    try:
        topo = load_topology(config.topology_path)
    except TopologyError as exc:
        return {"ok": False, "error": str(exc)}
    return {"ok": True, **to_summary(topo)}


def write_topology(config: Config, payload_text: str | None = None) -> dict:
    """The validated write path for the authored topology inventory.

    The authored analog of a reflected dataset's `infra-write`: it validates the
    inventory against the contract and regenerates the derived artifacts
    (`Topology.md` + `topology.figure.json`) and the `last_reviewed` stamp in one
    operation — so an authored edit never needs a hand-run `--emit doc` and a
    bad edit can't propagate.

    With ``payload_text``, the new inventory is validated and written to
    ``topology.json`` first (the full-replace path). Without it, the existing
    file is validated and its derivations refreshed (the "I edited the JSON,
    now sync" path).
    """
    from . import infra_datasets

    topo_path = config.topology_path

    source_updated = False
    if payload_text is not None and payload_text.strip():
        try:
            data = jsonio.loads(payload_text, source="topology payload")
        except jsonio.JsonError as exc:
            return {"ok": False, "error": str(exc)}
        problems = validate_inventory(data)
        if problems:
            return {"ok": False, "error": "off-contract: " + "; ".join(problems)}
        try:
            atomic_write_text(topo_path, jsonio.canonical(data) + "\n")
        except OSError as exc:
            return {"ok": False, "error": f"topology.json write failed: {exc}"}
        source_updated = True

    try:
        topo = load_topology(topo_path)  # validates (off-contract → refuses)
    except TopologyError as exc:
        return {"ok": False, "error": str(exc)}

    references = tuple(infra_datasets.reflected_references(config))
    today = datetime.date.today().isoformat()
    doc_path = topo_path.parent / "Topology.md"
    figure_path = topo_path.parent / "topology.figure.json"
    try:
        atomic_write_text(doc_path, render_doc(topo, last_reviewed=today, references=references))
        atomic_write_text(figure_path, jsonio.canonical(render_figure(topo)) + "\n")
    except OSError as exc:
        return {"ok": False, "error": f"derive failed: {exc}"}

    return {
        "ok": True,
        "source_updated": source_updated,
        "doc": doc_path.name,
        "figure": figure_path.name,
        "reviewed": today,
        "hosts": len(topo.hosts),
    }
