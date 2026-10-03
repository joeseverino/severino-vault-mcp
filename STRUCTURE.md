# Repository Structure

The governance core lives in [`vault-engine`](https://github.com/joeseverino/vault-engine);
this repo hosts it for every vault.

## Package: `src/severino_vault_mcp/`

| Path | Purpose |
|---|---|
| `server.py` | Composition root: builds the vaults, registers the shared tools once, then each vault's own group. |
| `vaults.py` | One `GovernanceContext` per vault (labs, edu, life) and the domain registrars that go with them. |
| `education.py` | The edu dataset: `education_dataset` tool and `export education`. |
| `labs/hq_manifest.py` | Severino HQ docs manifest (moves to HQ). |
| `cli.py` | `build_parser()`; each subparser declares its effect for the Cordon contract. |
| `__main__.py` | Entry point and CLI dispatch; `--fingerprint` hashes the package sources. |

## Tests: `tests/`

| Path | Purpose |
|---|---|
| `conftest.py` | Keeps every test off the machine's real edu and life configs. |
| `test_server.py` | Vault composition, the `vault` enum, and a real stdio `list_tools`. |
| `test_search.py` | Shared tools, resources, sensitivity, unlock and writes on a fake labs vault. |
| `test_education.py` | The education dataset and its CLI. |
| `test_hq_manifest.py` | HQ manifest generation. |
| `test_cli_dispatch.py`, `test_daily_write.py`, `test_doctor.py`, `test_schema_contract.py` | CLI wiring, daily-note writer, doctor, and the schema HQ validates against. |
| `golden/` | Frozen public surfaces (`schema --json`, `describe`, MCP tool names) and `verify.sh`. |

## Everything else

| Path | Purpose |
|---|---|
| `docs/` | Architecture, safety model, AI tool contract, testing, migration, release checklist, release notes. |
| `examples/sample-vault/` | Safe demo vault on the frontmatter contract. |
| `scripts/check.sh` | The CI gate, run locally. |
| `scripts/eval_ranking.py` | Rank-quality eval for `find` against the vault's Quick Index. |
| `.github/workflows/` | `ci.yml`, `release.yml`, `codeql.yml`, `pip-audit.yml`, `scorecard.yml`. |
| `config.example.toml` | Labs config template (edu and life use the same `[vault]` shape). |
