"""Print the server's registered MCP tool names, one per line, sorted.

Introspects the assembled server, composed deterministically: labs plus an
empty edu vault, with life blocked (its tools are covered by test_server.py).
"""

import asyncio
import os
import sys
import tempfile
from pathlib import Path

sys.modules["severino_life"] = None  # type: ignore[assignment]
with tempfile.TemporaryDirectory() as tmp:
    config = Path(tmp) / "edu.toml"
    config.write_text(f'[vault]\npath = "{tmp}"\n', encoding="utf-8")
    os.environ["SVMC_EDU_CONFIG"] = str(config)
    from severino_vault_mcp.server import mcp

    names = sorted(tool.name for tool in asyncio.run(mcp.list_tools()))
print("\n".join(names))
