# Repository Structure

What each part of `severino-vault-mcp` is responsible for. The generic vault
core (config, index, search, sensitivity, task ledger, the 18 core tools) lives
in [`vault-engine`](https://github.com/joeseverino/vault-engine); this repo is
the Labs domain layer on top of it.

## Package: `src/severino_vault_mcp/`

| Path | Purpose |
|---|---|
| `server.py` | Composition root: one `GovernanceContext`, `register_core` for the engine tools, then the Labs tool groups. |
| `cli.py` | `build_parser()`, the argparse CLI surface. Each subparser declares its effect for the cordon contract. |
| `__main__.py` | Console-script entry point and CLI dispatch; `--fingerprint` hashes the package sources. |
| `tools/` | FastMCP registration groups, one `register(mcp, ctx)` per domain: `site_ops.py`, `writeups.py`. |
| `labs/` | FastMCP-free domain logic shared by the MCP and the CLI. |
| `labs/writeup_service.py`, `labs/writeups.py` | Writeup loading, validation, and staged transactions with rollback. |
| `labs/site_ops_service.py` | jseverino.com D1 readers (through the site repo's wrangler), the confirmed schema apply, and the security-header check. |
| `labs/hq_manifest.py` | Severino HQ manifest synthesis on the shared frontmatter parser. |
| `labs/tech_groups.py` | Parser for the technology catalog at `06 Pages/_technology-groups.md`. |
| `contracts/` | The bundled projection of the site-owned content contract (`site_content.v1.json`) and its loader. |

## Tests: `tests/`

| Path | Purpose |
|---|---|
| `test_search.py` | Vault indexing, search, resources, sensitivity, unlock, write tools, CLI describe. |
| `test_writeups.py` | Writeup loader, technology catalog, writeup tools and transactions. |
| `test_site_ops.py` | D1 readers, PII redaction, CSP reports, wrangler resolution. |
| `test_hq_manifest.py` | HQ manifest generation. |
| `test_cli_dispatch.py` | CLI wiring. |
| `test_daily_write.py`, `test_doctor.py` | Daily-note writer and doctor surfaces. |
| `test_schema_contract.py` | The Labs schema contract HQ validates against. |
| `golden/` | Frozen public surfaces (`schema --json`, `describe`, MCP tool names) and `verify.sh`, which diffs against them. |

## Everything else

| Path | Purpose |
|---|---|
| `docs/` | Architecture, safety model, AI tool contract, testing and CI, migration, release checklist, and release notes. |
| `examples/sample-vault/` | Safe demo vault that mirrors the frontmatter contract, so the MCP runs without a private vault. |
| `scripts/check.sh` | The CI gate, run locally (cordon's checks engine). |
| `scripts/eval_ranking.py` | Rank-quality eval for `find_runbook` against the vault's Quick Index. |
| `.github/workflows/` | `ci.yml` (cordon gate), `release.yml` (release-please), `codeql.yml`, `pip-audit.yml`, `scorecard.yml`. |
| `config.example.toml` | Local configuration template. |
| `README.md`, `QUICKSTART.md`, `CONTRIBUTING.md`, `AGENTS.md` | Overview, setup, contribution, and agent guidance. |
