"""The vaults this server serves, each with its own config and schema profile.

labs is always present. edu is present when its config file exists. life is
present when the optional ``severino_life`` package is installed. Each vault
gets its own :class:`GovernanceContext`, so search, the sensitivity gate, and
schema validation never cross vaults.

Per-vault config paths: ``SVMC_CONFIG`` (labs), ``SVMC_EDU_CONFIG`` (edu),
``SVMC_LIFE_CONFIG`` (life). Each defaults to the standard install path.
"""

from __future__ import annotations

import os
import sys
from collections.abc import Callable, Mapping, MutableMapping
from dataclasses import dataclass, field
from pathlib import Path

from vault_engine.config import DEFAULT_CONFIG_PATH
from vault_engine.context import GovernanceContext
from vault_engine.schema import EDUCATION_PROFILE, LABS_PROFILE

EDU_CONFIG_DEFAULT = "~/.config/severino-edu-mcp/config.toml"

# Labs is the default vault for every tool.
DEFAULT_VAULT = "labs"


@dataclass
class Vaults:
    """The composed vault contexts plus the domain registrars that go with them."""

    contexts: dict[str, GovernanceContext] = field(default_factory=dict)
    # name -> register(mcp, ctx) for that vault's own tool group
    domains: dict[str, Callable] = field(default_factory=dict)
    instructions: list[str] = field(default_factory=list)


def _scoped_env(env: Mapping[str, str]) -> dict[str, str]:
    """Process env without SVMC_* overrides, so labs settings never leak into
    another vault's config."""
    return {k: v for k, v in env.items() if not k.startswith("SVMC_")}


def edu_config_path(env: Mapping[str, str] | None = None) -> Path:
    values = os.environ if env is None else env
    return Path(values.get("SVMC_EDU_CONFIG", EDU_CONFIG_DEFAULT)).expanduser()


def edu_context(env: Mapping[str, str] | None = None) -> GovernanceContext:
    values = os.environ if env is None else env
    return GovernanceContext.load(
        edu_config_path(values), env=_scoped_env(values), profile=EDUCATION_PROFILE
    )


def build(env: MutableMapping[str, str] | None = None) -> Vaults:
    """Compose every available vault.

    Life reads its config path from ``SVMC_CONFIG`` at call time, so the labs
    path is resolved first and ``SVMC_CONFIG`` is then pointed at Life's config
    (or removed) for the rest of the process.
    """
    values = os.environ if env is None else env
    vaults = Vaults()

    labs_path = values.get("SVMC_CONFIG", DEFAULT_CONFIG_PATH)
    vaults.contexts["labs"] = GovernanceContext.load(labs_path, env=values, profile=LABS_PROFILE)

    if edu_config_path(values).is_file():
        from . import education

        vaults.contexts["edu"] = edu_context(values)
        vaults.domains["edu"] = education.register
    else:
        print(f"severino-vault-mcp: no edu config at {edu_config_path(values)}; edu vault off",
              file=sys.stderr)

    try:
        from severino_life import mcp_domain, store
    except ImportError:
        print("severino-vault-mcp: severino-life not installed; life vault off", file=sys.stderr)
        return vaults

    life_path = values.get("SVMC_LIFE_CONFIG")
    if life_path:
        values["SVMC_CONFIG"] = life_path
    else:
        values.pop("SVMC_CONFIG", None)
    vaults.contexts["life"] = store.context()
    vaults.domains["life"] = mcp_domain.register_domain
    vaults.instructions.append(mcp_domain.INSTRUCTIONS)
    return vaults
