"""Server runtime context — the dependency bundle each tool group receives.

One :class:`ServerContext` carries the configured :class:`Config` plus the
lazily-built runtimes (vault loader, writeup runtime, site-ops runtime). Tool
groups are registered as ``register(mcp, ctx)`` and pull what they need off the
context, so a server composes only the groups it wants and never builds a runtime
a group it omitted would use.

Staging note: while the engine extraction is in flight this single context holds
both the engine-generic loader and the Labs-domain runtimes. At the physical
``vault-engine`` / servers split it divides into a generic vault context and a
Labs context that extends it; the ``register(mcp, ctx)`` seam does not change.
"""

from __future__ import annotations

from .config import Config
from .labs import site_ops_service, writeup_service
from .schema import LABS_PROFILE, SchemaProfile
from .vault import VaultLoader


class ServerContext:
    """Lazily-built runtimes a server's tool groups share, keyed off one Config.

    ``profile`` is the vault's frontmatter contract — the schema-validated write
    tools check against it, so a Labs server and an Education server differ only
    by the profile their context carries.
    """

    def __init__(self, config: Config, profile: SchemaProfile = LABS_PROFILE) -> None:
        self.config = config
        self.profile = profile
        self._loader: VaultLoader | None = None
        self._writeup_runtime: writeup_service.WriteupRuntime | None = None
        self._site_ops: site_ops_service.SiteOpsRuntime | None = None

    @property
    def loader(self) -> VaultLoader:
        if self._loader is None:
            self._loader = VaultLoader(self.config)
        return self._loader

    @property
    def writeup_runtime(self) -> writeup_service.WriteupRuntime:
        if self._writeup_runtime is None:
            self._writeup_runtime = writeup_service.WriteupRuntime.from_config(
                self.config, loader=self.loader
            )
        return self._writeup_runtime

    @property
    def site_ops(self) -> site_ops_service.SiteOpsRuntime:
        if self._site_ops is None:
            self._site_ops = site_ops_service.SiteOpsRuntime.from_env()
        return self._site_ops
