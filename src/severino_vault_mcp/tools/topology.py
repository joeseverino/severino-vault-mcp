"""Topology tool — the structured infrastructure inventory as a tool group.

A single read-only tool over the vault's authored topology inventory (the
single source of truth for hosts, IPs, containers, and DNS rewrites).
Registered onto a server's FastMCP instance via :func:`register`, so a server
that doesn't carry a topology inventory simply never composes this group.
"""

from __future__ import annotations

from typing import Any

from vault_engine.context import ServerContext

from ..labs import topology as topology_mod


def register(mcp, ctx: ServerContext) -> None:
    """Register the topology tool group on ``mcp`` from a server context.

    Pulls the configured ``Config`` off ``ctx``; a server that omits this group
    never calls register.
    """
    config = ctx.config

    @mcp.tool()
    def get_topology() -> dict[str, Any]:
        """USE THIS for any host / IP / container / DNS-rewrite question — never
        re-derive the network from prose docs.

        Returns the structured inventory from
        `02 Infrastructure/Topology/topology.json` (the single source of truth):
        every host with its LAN / Tailscale / public IPs, SSH alias and port, and
        containers, plus the AdGuard DNS rewrites and the access constraints. The
        human-readable `Topology.md`, the network diagram, and HQ's asset registry
        are all generated from this same data, so this tool is always current with
        them by construction.
        """
        return topology_mod.get_topology(config)
