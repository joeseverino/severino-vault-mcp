"""The host composes one server from every available vault."""

from __future__ import annotations

import asyncio
import builtins
import importlib
import sys
from pathlib import Path

import pytest

CORE = {
    "find", "read_doc", "set_frontmatter", "update_link",
    "task_board", "task_write", "recent_changes", "daily_progress",
}
LIFE = {"reminders", "calendar", "agenda", "life_view", "renew", "life_ops"}


def _server():
    for mod in list(sys.modules):
        if mod.startswith("severino_vault_mcp"):
            del sys.modules[mod]
    return importlib.import_module("severino_vault_mcp.server")


def _tools(server) -> dict:
    return {tool.name: tool for tool in asyncio.run(server.mcp.list_tools())}


def _edu_config(tmp_path: Path, monkeypatch) -> None:
    vault = tmp_path / "edu-vault"
    vault.mkdir()
    config = tmp_path / "edu.toml"
    config.write_text(f'[vault]\npath = "{vault}"\n', encoding="utf-8")
    monkeypatch.setenv("SVMC_EDU_CONFIG", str(config))


def test_labs_only_when_edu_and_life_are_absent(tmp_path: Path, monkeypatch) -> None:
    real_import = builtins.__import__

    def no_life(name, *args, **kwargs):
        if name.startswith("severino_life"):
            raise ImportError(name)
        return real_import(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", no_life)
    tools = _tools(_server())
    assert set(tools) == CORE
    assert tools["find"].inputSchema["properties"]["vault"]["const"] == "labs"


def test_edu_vault_and_dataset_register_when_its_config_exists(
    tmp_path: Path, monkeypatch
) -> None:
    _edu_config(tmp_path, monkeypatch)
    tools = _tools(_server())
    assert "education_dataset" in tools
    assert "edu" in tools["find"].inputSchema["properties"]["vault"]["enum"]


def test_life_tools_register_when_the_package_is_installed(
    tmp_path: Path, monkeypatch
) -> None:
    pytest.importorskip("severino_life")
    _edu_config(tmp_path, monkeypatch)
    tools = _tools(_server())
    assert set(tools) == CORE | LIFE | {"education_dataset"}
    assert tools["find"].inputSchema["properties"]["vault"]["enum"] == ["labs", "edu", "life"]
    assert tools["find"].inputSchema["properties"]["vault"]["default"] == "labs"


def test_edu_context_carries_the_education_profile(tmp_path: Path, monkeypatch) -> None:
    _edu_config(tmp_path, monkeypatch)
    from severino_vault_mcp import vaults

    assert vaults.edu_context().profile.name == "education"


def test_labs_overrides_never_reach_the_edu_vault(tmp_path: Path, monkeypatch) -> None:
    _edu_config(tmp_path, monkeypatch)
    monkeypatch.setenv("SVMC_VAULT_PATH", str(tmp_path / "labs-vault"))
    from severino_vault_mcp import vaults

    assert vaults.edu_context().config.vault_path == tmp_path / "edu-vault"


def test_the_real_server_answers_list_tools_over_stdio(tmp_path: Path, monkeypatch) -> None:
    import os

    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client

    _edu_config(tmp_path, monkeypatch)
    labs = tmp_path / "labs-vault"
    labs.mkdir()
    env = {**os.environ, "SVMC_VAULT_PATH": str(labs), "SVMC_CACHE_SECONDS": "0"}
    params = StdioServerParameters(command=sys.executable, args=["-m", "severino_vault_mcp"], env=env)

    async def list_names() -> set[str]:
        async with stdio_client(params) as (read, write), ClientSession(read, write) as session:
            await session.initialize()
            return {tool.name for tool in (await session.list_tools()).tools}

    names = asyncio.run(list_names())
    assert CORE | {"education_dataset"} <= names
