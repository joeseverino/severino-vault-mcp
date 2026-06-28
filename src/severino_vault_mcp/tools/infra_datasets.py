"""Infra-dataset tools — the structured infrastructure registry as a tool group.

Read-only access to the vault's infra-dataset registry: a catalog of every
authored or drift-reflected dataset, and a reader that returns one dataset's
data from its true owner. Registered onto a server's FastMCP instance via
:func:`register`, so a server that doesn't carry an infra-dataset registry
simply never composes this group.
"""

from __future__ import annotations

from typing import Any

from .. import infra_datasets
from ..context import ServerContext


def register(mcp, ctx: ServerContext) -> None:
    """Register the infra-dataset tool group on ``mcp`` from a server context.

    Pulls the configured ``Config`` off ``ctx``; a server that omits this group
    never calls register.
    """
    config = ctx.config

    @mcp.tool()
    def list_infra_datasets() -> dict[str, Any]:
        """List every structured infrastructure dataset the vault knows about.

        The catalog from the one infra-dataset registry: each entry's id, kind
        (`authored` = a human declares it, e.g. topology; `reflected` = a drift
        guard mirrors live system state, e.g. DNS rewrites), owner, and whether it
        is machine-readable. Call this to discover what `get_infra_dataset` can
        read, or to answer "where does fact X live / who owns it."
        """
        return infra_datasets.list_datasets(config)

    @mcp.tool()
    def get_infra_dataset(dataset_id: str, refresh: bool = False) -> dict[str, Any]:
        """Read one infrastructure dataset's data from its true owner.

        Reads through the registry so every infra fact comes from one place: the
        `topology` inventory (authored JSON), or a drift guard's mirror —
        `dns_rewrites` (AdGuard), `proxy_hosts` (NPM), `tailscale_acl` (Tailscale).
        USE THIS for any DNS-rewrite / proxy-host / ACL question instead of reading
        or grepping the docs by hand. Call `list_infra_datasets` first if unsure of
        the id.

        By default returns the git-tracked cache instantly with `fetched_at`
        freshness — so it answers even when the live system is down. The response
        carries `live: false` for a cache read. Sensitivity is gated like a doc
        body (`sensitive` returns with an advisory; `restricted` is withheld).

        Args:
            dataset_id: e.g. "dns_rewrites", "proxy_hosts", "tailscale_acl",
                "topology".
            refresh: Read live via the dataset's drift guard, updating freshness;
                falls back to the cache flagged `stale` if the system is
                unreachable. Default False (cache only — fast and offline).
        """
        return infra_datasets.read_dataset(config, dataset_id, refresh=refresh)
