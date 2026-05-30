# Repository Structure

This file maps every tracked file in `severino-vault-mcp` and explains
what each one is responsible for. Generated caches, local virtualenvs, and
`__pycache__` files are intentionally excluded.

## Root Files

| Path | Purpose |
|---|---|
| `.editorconfig` | Cross-editor formatting defaults. |
| `.gitignore` | Local/generated files excluded from Git. |
| `CHANGELOG.md` | Release history and feature notes. |
| `config.example.toml` | Copyable local configuration template for vault paths, cache, optional integrations, and unlock settings. |
| `CONTRIBUTING.md` | Local development, PR, testing, and security-report guidance. |
| `LICENSE` | MIT license. |
| `QUICKSTART.md` | Command-first setup guide for sample-vault and real-vault adoption. |
| `README.md` | Main project overview, install instructions, MCP surface, configuration, and sensitivity policy. |
| `STRUCTURE.md` | This repository map. |
| `pyproject.toml` | Package metadata, dependencies, console script, pytest config, and Ruff config. |
| `uv.lock` | Locked dependency graph for reproducible `uv` installs. |

## GitHub Metadata

| Path | Purpose |
|---|---|
| `.github/SECURITY.md` | GitHub security policy and vulnerability reporting guidance. |
| `.github/dependabot.yml` | Dependabot configuration for Python and GitHub Actions updates. |
| `.github/workflows/ci.yml` | CI workflow running Ruff and pytest on Python 3.11, 3.12, and 3.13. |

## Documentation

| Path | Purpose |
|---|---|
| `docs/demo.md` | Short transcript showing the intended MCP assistant flow with the sample vault. |
| `docs/architecture.md` | End-to-end architecture guide covering the runtime model, data contract, generic MCP surface, jseverino.com extension surface, write boundaries, and adoption guidance. |
| `docs/operator-workflows.md` | Portfolio-facing workflow-pack guide showing the concrete systems behind the jseverino.com tools and how to adapt the pattern. |
| `docs/ai-tool-contract.md` | Compact AI-facing tool-selection contract for fast, low-token MCP use. |
| `docs/assets/local-model-vps-ssh.png` | Screenshot showing a Mac-hosted local model using this MCP server to answer a VPS SSH runbook question. |
| `docs/assets/local-model-container-restart.png` | Screenshot showing a Mac-hosted local model using this MCP server to answer a homelab container restart question. |
| `docs/migration-guide.md` | Messy-vault migration guide with doctor usage and frontmatter examples. |
| `docs/release-checklist.md` | Public release checklist for repository hygiene, verification, packaging, and release notes. |
| `docs/testing-ci.md` | Local test commands, CI behavior, and what the tests cover. |
| `docs/ai-safety-security.md` | AI-facing safety model, sensitivity gates, unlock behavior, and audit posture. |

## Sample Vault

The sample vault is safe demo data. It mirrors the expected folder and
frontmatter contract so reviewers can run the MCP without access to a private
vault.

| Path | Purpose |
|---|---|
| `examples/sample-vault/01 Projects/Client Edge DNS.md` | Example project index with frontmatter and related doc references. |
| `examples/sample-vault/02 Infrastructure/AdGuard Home.md` | Example infrastructure note for DNS and client edge name resolution. |
| `examples/sample-vault/02 Infrastructure/Offline CA.md` | Example `restricted` doc used to prove body withholding behavior. |
| `examples/sample-vault/03 Runbooks/Add Nginx Proxy Host.md` | Example runbook for adding an HTTPS proxy host. |
| `examples/sample-vault/03 Runbooks/Generate Internal Service Certificate.md` | Example runbook for generating an `internal.example` certificate. |
| `examples/sample-vault/03 Runbooks/Quick Index.md` | Example Quick Index backing `vault://quick-index`. |

## Python Package

| Path | Purpose |
|---|---|
| `src/severino_vault_mcp/__init__.py` | Package marker and exported version surface. |
| `src/severino_vault_mcp/__main__.py` | Console-script entry point wrapper. |
| `src/severino_vault_mcp/config.py` | TOML and environment-driven configuration: vault path, cache, optional integrations, unlock settings, and audit paths. |
| `src/severino_vault_mcp/doctor.py` | Frontmatter validator and proposal helper for onboarding messy vaults. |
| `src/severino_vault_mcp/search.py` | Lightweight lexical ranking for `find_runbook`. |
| `src/severino_vault_mcp/secret_unlock.py` | Local one-request unlock gate for `restricted` doc bodies. |
| `src/severino_vault_mcp/sensitivity.py` | Sensitivity enum, body-release policy helper, and advisory text. |
| `src/severino_vault_mcp/server.py` | FastMCP server registration: resources, tools, read/write operations, and body search. |
| `src/severino_vault_mcp/tech_groups.py` | Parser for the jseverino.com technology-groups catalog at `06 Pages/_technology-groups.md`. |
| `src/severino_vault_mcp/vault.py` | Obsidian vault loader, frontmatter parser, indexed document model, and cache. |
| `src/severino_vault_mcp/writeups.py` | Writeup loader for `05 Writeups/<slug>/index.md`, using the portfolio frontmatter shape (no `doc_id`, but `published`/`featured`/`technologies`). |

## Tests

| Path | Purpose |
|---|---|
| `tests/__init__.py` | Test package marker. |
| `tests/test_search.py` | End-to-end unit tests for vault indexing, search, resources, sensitivity, local unlock, write tools, and sample-vault reproducibility. |
| `tests/test_writeups.py` | Tests for the writeup loader, technology catalog parser, and the four writeup-specific MCP tools. |

## Runtime Shape

The runtime dependency direction is intentionally small:

```text
MCP host
  -> severino_vault_mcp.server
       -> Config
       -> VaultLoader
       -> search.rank
       -> sensitivity policy
       -> secret_unlock gate
       -> FastMCP resources/tools
```

The private vault remains outside this repository. The repo only needs a vault
path, a stable frontmatter schema, and local filesystem access.
