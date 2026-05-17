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


def _env_bool(name: str, default: bool = False) -> bool:
    raw = os.environ.get(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class Config:
    vault_path: Path
    indexed_dirs: tuple[str, ...]
    hq_url: str
    cache_seconds: int
    allow_secret_adjacent_unlock: bool
    secret_unlock_hash: str | None
    secret_unlock_hash_file: Path
    secret_unlock_keychain_service: str
    secret_unlock_keychain_account: str
    secret_unlock_audit_log: Path

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
            allow_secret_adjacent_unlock=_env_bool(
                "SKR_ALLOW_SECRET_ADJACENT_UNLOCK",
                False,
            ),
            secret_unlock_hash=os.environ.get("SKR_SECRET_ADJACENT_UNLOCK_HASH"),
            secret_unlock_hash_file=_env_path(
                "SKR_SECRET_ADJACENT_UNLOCK_HASH_FILE",
                "~/.config/severino-vault-mcp/secret-adjacent-unlock.sha256",
            ),
            secret_unlock_keychain_service=os.environ.get(
                "SKR_SECRET_ADJACENT_UNLOCK_KEYCHAIN_SERVICE",
                "severino-vault-mcp",
            ),
            secret_unlock_keychain_account=os.environ.get(
                "SKR_SECRET_ADJACENT_UNLOCK_KEYCHAIN_ACCOUNT",
                "secret-adjacent-unlock",
            ),
            secret_unlock_audit_log=_env_path(
                "SKR_SECRET_ADJACENT_UNLOCK_AUDIT_LOG",
                "~/.local/state/severino-vault-mcp/audit.log",
            ),
        )
