"""Print the server's registered MCP tool names, one per line, sorted.

The authoritative tool-surface contract: introspect the assembled FastMCP
instance at runtime instead of grepping a source file, so tools can move
between modules during the engine extraction without false drift. What must
stay byte-stable is the externally-visible set Claude Code sees, which is
exactly what `mcp.list_tools()` reports.
"""

import asyncio

from severino_vault_mcp.server import mcp

names = sorted(tool.name for tool in asyncio.run(mcp.list_tools()))
print("\n".join(names))
