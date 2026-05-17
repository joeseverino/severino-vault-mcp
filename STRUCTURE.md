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
| `docs/testing-ci.md` | Local test commands, CI behavior, and what the tests cover. |
| `docs/ai-safety-security.md` | AI-facing safety model, sensitivity gates, unlock behavior, and audit posture. |

## Sample Vault

The sample vault is safe demo data. It mirrors the private vault's folder and
frontmatter contract so reviewers can run the MCP without access to private
Severino Labs notes.

| Path | Purpose |
|---|---|
| `examples/sample-vault/01 Projects/Homelab DNS.md` | Example project index with frontmatter and related doc references. |
| `examples/sample-vault/02 Infrastructure/AdGuard Home.md` | Example infrastructure note for DNS and homelab name resolution. |
| `examples/sample-vault/02 Infrastructure/Offline CA.md` | Example `secret_adjacent` doc used to prove body withholding behavior. |
| `examples/sample-vault/03 Runbooks/Add Nginx Proxy Host.md` | Example runbook for adding an HTTPS proxy host. |
| `examples/sample-vault/03 Runbooks/Generate Homelab Certificate.md` | Example runbook for generating a `.homelab` certificate. |
| `examples/sample-vault/03 Runbooks/Quick Index.md` | Example Quick Index backing `vault://quick-index`. |

## Python Package

| Path | Purpose |
|---|---|
| `src/severino_knowledge_router/__init__.py` | Package marker and exported version surface. |
| `src/severino_knowledge_router/__main__.py` | Console-script entry point wrapper. |
| `src/severino_knowledge_router/config.py` | Environment-driven configuration: vault path, cache, unlock settings, and audit paths. |
| `src/severino_knowledge_router/search.py` | Lightweight lexical ranking for `find_runbook`. |
| `src/severino_knowledge_router/secret_unlock.py` | Local one-request unlock gate for `secret_adjacent` doc bodies. |
| `src/severino_knowledge_router/sensitivity.py` | Sensitivity enum, body-release policy helper, and advisory text. |
| `src/severino_knowledge_router/server.py` | FastMCP server registration: resources, tools, read/write operations, and body search. |
| `src/severino_knowledge_router/vault.py` | Obsidian vault loader, frontmatter parser, indexed document model, and cache. |

## Tests

| Path | Purpose |
|---|---|
| `tests/__init__.py` | Test package marker. |
| `tests/test_search.py` | End-to-end unit tests for vault indexing, search, resources, sensitivity, local unlock, write tools, and sample-vault reproducibility. |

## Runtime Shape

The runtime dependency direction is intentionally small:

```text
MCP host
  -> severino_knowledge_router.server
       -> Config
       -> VaultLoader
       -> search.rank
       -> sensitivity policy
       -> secret_unlock gate
       -> FastMCP resources/tools
```

The private vault remains outside this repository. The repo only needs a vault
path, a stable frontmatter schema, and local filesystem access.
