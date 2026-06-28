"""The Education server — proof the engine is reusable by a second consumer.

severino-edu-mcp composes the same register_core the Labs server does, but with an
Education context and no Labs tool groups. These assertions are hermetic: listing
registered tools never touches a vault.
"""

from __future__ import annotations

import asyncio


def _tool_names() -> set[str]:
    from severino_vault_mcp import edu_server

    return {tool.name for tool in asyncio.run(edu_server.mcp.list_tools())}


def test_edu_server_composes_the_generic_core() -> None:
    names = _tool_names()
    assert {
        "find_runbook", "read_doc", "search_body", "lookup_system",
        "recent_changes", "daily_progress", "task_board", "add_task",
        "set_task_status", "add_frontmatter", "update_frontmatter",
    } <= names


def test_edu_server_omits_every_labs_tool() -> None:
    names = _tool_names()
    for labs_tool in (
        "get_topology", "list_infra_datasets", "list_writeups",
        "get_technology_catalog", "list_contact_submissions",
        "check_jseverino_security_headers", "apply_jseverino_d1_schema",
    ):
        assert labs_tool not in names


def test_edu_server_carries_the_education_profile() -> None:
    from severino_vault_mcp import edu_server

    assert edu_server._CTX.profile.name == "education"
