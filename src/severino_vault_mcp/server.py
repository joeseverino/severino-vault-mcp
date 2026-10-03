"""The MCP server: one process serving every configured vault.

Shared vault tools register once with a ``vault`` argument; each vault's own
tool group (education, life) registers against that vault's context.
"""

from __future__ import annotations

from mcp.server.fastmcp import FastMCP
from vault_engine.core_tools import register_core

from . import vaults as vault_registry

_SERVER_INSTRUCTIONS = """\
Joe's Obsidian vaults: labs (homelab infrastructure, runbooks, projects),
edu (Georgia Tech coursework and certifications), life (renewals, goals,
personal tasks). Every shared tool takes `vault` (default labs).

1. Operational questions ("how do I X", "what's the runbook for Y"): call
   `find` before writing any prose, then `read_doc` on the best hit, and
   answer in the doc's own words at the doc's length. Quote commands
   verbatim. Never substitute a generic tutorial for an existing doc.
2. No relevant hit: say so, then offer to write the doc (write the file,
   then `set_frontmatter` to register it).
3. Broad questions: start from the `vault://labs/quick-index` resource (or
   `read_doc` on the Quick Index doc), then read the target doc.
4. Progress or log questions ("what did I do Friday?"): `daily_progress`.
5. `find(by=...)`: relevance (default), system (the `system:` field),
   project (`related_projects`), text (full-text body search).

Sensitivity: public, internal and sensitive bodies come back from
`read_doc` (sensitive adds an `advisory` to pass along). restricted bodies
are withheld unless the user explicitly needs one; pass
`include_restricted=True`, which also needs a local unlock.

Writes: `set_frontmatter` creates or updates a doc's frontmatter against that
vault's schema; `task_write` adds, moves, promotes or deletes tasks. After a
write, remind the user to run their vault sync.

edu: `education_dataset` returns the publishable institutions and courses
the site and resume read.
"""


def build() -> FastMCP:
    registry = vault_registry.build()
    instructions = "\n".join([_SERVER_INSTRUCTIONS, *registry.instructions])
    server = FastMCP("severino-vault-mcp", instructions=instructions)
    register_core(server, registry.contexts, default=vault_registry.DEFAULT_VAULT)
    for name, register in registry.domains.items():
        register(server, registry.contexts[name])
    return server


mcp = build()


def run() -> None:
    """Start the MCP server over stdio."""
    mcp.run()


__all__ = ["build", "mcp", "run"]
