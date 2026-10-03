# AGENTS.md: house rules for severino-vault-mcp

`CLAUDE.md` is a symlink to this file.

One local stdio MCP server (FastMCP) for every vault: labs, edu, life. The
governance core lives in [`severino-vault-engine`](https://github.com/joeseverino/vault-engine)
(import `vault_engine`); change generic behavior there, release, bump the pin
here. Don't re-implement it locally.

## Shape

```python
registry = vaults.build()                       # one GovernanceContext per vault
register_core(mcp, registry.contexts, default="labs")   # engine: 8 shared tools, `vault` arg
for name, register in registry.domains.items():         # each vault's own group
    register(mcp, registry.contexts[name])
```

- `vaults.py`: composes the vaults. labs always; edu when its config exists;
  life when `severino_life` imports. Per-vault config: `SVMC_CONFIG`,
  `SVMC_EDU_CONFIG`, `SVMC_LIFE_CONFIG`. edu gets an env scrubbed of `SVMC_*`
  so labs overrides can't leak. Life reads `SVMC_CONFIG` at call time, so
  `build()` points it at life's config after labs is loaded.
- `server.py`: composition root and the one instructions block.
- `education.py`: the edu dataset (`education_dataset` tool, `export education` CLI).
- `labs/hq_manifest.py`: the HQ docs manifest. HQ will own this; nothing else
  labs-specific lives here (site work belongs to the jseverino.com repo).
- `cli.py` / `__main__.py`: the CLI over the labs vault. Each subparser declares
  its effect with `cordon_emit.set_effect`; `describe` projects the parser to a
  Cordon contract.

The labs schema (`LABS_PROFILE`) is defined in the engine. `schema --json` is the
frozen shape HQ commits (`docs_index/schema.json`); after a profile change:
release the engine, bump the pin, `tools reinstall severino-vault-mcp`, then
`hq schema` and deploy HQ.

## Contracts

- Every service returns one dict; failures are `{"ok": false, "error": "..."}`.
  CLI subcommands exit 0/1 on `ok`.
- Consumers run the installed console script; `--fingerprint` (checked by
  `tools doctor`) catches a stale install.
- `export education` JSON is read by jseverino.com (`bin/content-sync`) and
  resume-engine (`lib/reconcile-coursework`). Changing its shape is a contract
  change for both.

## Safety

- `public`/`internal`/`sensitive` bodies are released; `restricted` is withheld
  unless the caller asks and the local unlock succeeds. Text search never reads
  restricted bodies.
- Writes are schema-specific: validate against the vault's `SchemaProfile`,
  keep `doc_id` immutable, serialize with the engine's `serialize_frontmatter`,
  replace atomically. Never hand-roll YAML or `open(path, "w")`.

## Verify

```bash
uv run --extra dev --extra life pytest -q
uv run --extra dev ruff check .
bash tests/golden/verify.sh
scripts/check.sh
```

## Test idioms

- Hermetic: fixtures build a fake vault on disk; `tests/conftest.py` points
  `SVMC_EDU_CONFIG` and `SVMC_LIFE_CONFIG` at missing files so no test reads
  this machine's real vaults.
- `_fresh_module(name)` re-imports `severino_vault_mcp.*` after
  `monkeypatch.setenv`; set env first. `_tool(server, name)` reaches the
  registered callable through the tool manager.
- `test_search.py`: shared tools on the labs vault. `test_server.py`: vault
  composition and a real stdio `list_tools`. `test_education.py`: the dataset.
  Engine behavior is tested in the engine repo.
