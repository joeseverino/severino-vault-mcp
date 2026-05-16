"""Configuration via environment variables.

All paths can be overridden — defaults match Joe's Mac layout. The package
is single-user by design (it runs locally as a stdio MCP server), so envs
are the only configuration surface.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path


def _env_path(name: str, default: str) -> Path:
    raw = os.environ.get(name, default)
    return Path(os.path.expanduser(raw))


def _env_list(name: str, default: list[str]) -> list[str]:
    raw = os.environ.get(name)
    if raw is None:
        return default
    return [part for part in raw.split(":") if part]


@dataclass(frozen=True)
class Config:
    vault_path: Path
    indexed_dirs: tuple[str, ...]
    hq_url: str
    cache_seconds: int

    @classmethod
    def from_env(cls) -> Config:
        return cls(
            vault_path=_env_path(
                "SKR_VAULT_PATH",
                "~/Documents/Code/Severino Labs",
            ),
            indexed_dirs=tuple(_env_list(
                "SKR_INDEXED_DIRS",
                ["01 Projects", "02 Infrastructure", "03 Runbooks"],
            )),
            hq_url=os.environ.get(
                "SKR_HQ_URL",
                "https://hq.jseverino.com",
            ),
            cache_seconds=int(os.environ.get("SKR_CACHE_SECONDS", "30")),
        )
