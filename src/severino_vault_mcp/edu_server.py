"""severino-edu-mcp — the Education vault MCP server.

The engine's second consumer, and the proof it is reusable: this server composes
the same generic core (:func:`core_tools.register_core`) that the Labs server
does, but against an Education :class:`ServerContext` carrying ``EDUCATION_PROFILE``
and no Labs-domain tool groups. It is launched with its own config
(``SVMC_CONFIG=education.toml``) pointing at the Georgia Tech / education vault.

Course-specific tool groups (a course board, scaffolding) layer on later via the
same ``register(mcp, ctx)`` seam the Labs groups use.
"""

from __future__ import annotations

from mcp.server.fastmcp import FastMCP

from .config import Config
from .context import ServerContext
from .core_tools import register_core
from .schema import EDUCATION_PROFILE

_CTX = ServerContext(Config.from_env(), profile=EDUCATION_PROFILE)


_SERVER_INSTRUCTIONS = """\
This MCP serves an Obsidian-style education vault: Georgia Tech OMS Cybersecurity
coursework and active certifications. Courses, course notes, and assignments are
frontmatter-tagged docs; the same search / read / task tools as the operator's
other vault apply here, governed by the education schema profile.

OPERATING RULES:

1. For "where are my notes on X", "what's due", or "what did I cover in <course>",
   call `find_runbook` / `search_body` to locate the doc, then `read_doc` the top
   hit and answer from it. Do not invent course content from model memory.

2. Course work is tracked as tasks (`doc_type: task`): use `task_board` for what's
   open, `add_task` to capture an assignment or todo, `set_task_status` to close
   it. Assignments and todos share the universal task lifecycle.

3. Writes (`add_frontmatter` / `update_frontmatter`) validate against the
   education profile — doc-types `course | course_note | assignment | resource`,
   id prefixes `course- / cnote- / asg- / res-`. A Labs doc-type is rejected here,
   and vice versa, by design.

4. Daily progress questions use `daily_progress`.

The configured vault root and indexed directories come from the server's config
(SVMC_CONFIG / SVMC_* overrides) — point it at the education vault.
"""

mcp = FastMCP("severino-edu-mcp", instructions=_SERVER_INSTRUCTIONS)


register_core(mcp, _CTX)

# Education-specific tool groups (course board, scaffolding) register here later,
# via the same register(mcp, _CTX) seam the Labs groups use. None yet — the core
# already makes the vault searchable, readable, and task-tracked.


# ----- entry point ------------------------------------------------------------

def run() -> None:
    """Start the Education MCP server over stdio (the `severino-edu-mcp` script)."""
    mcp.run()


__all__ = ["mcp", "run"]
