# Quickstart

This guide gets `severino-vault-mcp` running as a local stdio MCP server
against either the included sample vault or your own Obsidian-style operations
vault.

## Prerequisites

- Python 3.11+
- `uv`
- `ripgrep` (`rg`) for body search
- An MCP client such as Claude Code, Claude Desktop, Cline, or another MCP host

macOS toolchain:

```bash
brew install uv ripgrep
```

## 1. Clone And Test

```bash
git clone git@github.com:joeseverino/severino-vault-mcp.git
cd severino-vault-mcp
uv sync --extra dev
uv run pytest
uv run ruff check .
```

Expected result:

```text
pytest passes
ruff reports All checks passed
```

## 2. Run The Sample Vault

The sample vault is safe network/security operations demo data. It does not
require access to a private vault.

```bash
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

This starts the MCP server on stdio. It waits for an MCP client to talk to it,
so it will not print a web URL.

## 3. Wire Claude Code

From the repo directory:

```bash
claude mcp add \
  -e SVMC_VAULT_PATH="$PWD/examples/sample-vault" \
  severino-vault-mcp \
  -- uv run --no-editable --directory "$PWD" severino-vault-mcp
```

Then ask your MCP client to verify:

```text
Use the severino-vault-mcp MCP. Read vault://quick-index and tell me the first demo workflow.
```

Expected behavior:

- The client can see `vault://quick-index`.
- The client can read `vault://doc/rb-generate-internal-cert`.
- `find_runbook("generate internal certificate")` returns
  `rb-generate-internal-cert`.
- `vault://doc/infra-offline-ca` withholds the body because it is
  `secret_adjacent`.

Validate the sample vault from another terminal:

```bash
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp doctor
```

## 4. Wire Claude Desktop

Edit:

```text
~/Library/Application Support/Claude/claude_desktop_config.json
```

Example using the sample vault:

```json
{
  "mcpServers": {
    "severino-vault-mcp": {
      "command": "uv",
      "args": [
        "run",
        "--directory",
        "/absolute/path/to/severino-vault-mcp",
        "severino-vault-mcp"
      ],
      "env": {
        "SVMC_VAULT_PATH": "/absolute/path/to/severino-vault-mcp/examples/sample-vault"
      }
    }
  }
}
```

Restart Claude Desktop after editing the config.

## 5. Use Your Own Vault

Your vault needs markdown files with YAML frontmatter under these folders by
default:

```text
01 Projects/
02 Infrastructure/
03 Runbooks/
```

Minimum frontmatter:

```yaml
---
doc_id: rb-example
title: Example Runbook
doc_type: runbook
system: Example System
environment: other
status: active
sensitivity: internal
tags:
  - example
---
```

For a persistent setup, copy and edit the example config:

```bash
mkdir -p ~/.config/severino-vault-mcp
cp config.example.toml ~/.config/severino-vault-mcp/config.toml
```

Set:

```toml
[vault]
path = "/absolute/path/to/your/vault"
indexed_dirs = ["01 Projects", "02 Infrastructure", "03 Runbooks"]
```

For one-off runs, environment variables are enough:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" \
SVMC_INDEXED_DIRS="Projects:Infrastructure:Runbooks" \
uv run --no-editable severino-vault-mcp
```

Before connecting a messy vault to an MCP client, run:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" \
uv run --no-editable severino-vault-mcp doctor --propose
```

Fix missing or invalid frontmatter until `doctor` reports no errors.

## 6. Recommended Vault Docs

For best results, create:

- A Quick Index doc with `doc_id: report-playbook-mcp-index`.
- Runbooks with stable `rb-*` IDs.
- Infrastructure docs with stable `infra-*` IDs.
- Project indexes with stable `project-*` IDs.
- Deliberate `sensitivity` values: `public`, `internal`, `sensitive`, or
  `secret_adjacent`.

The Quick Index backs:

```text
vault://quick-index
```

Known docs can be read through:

```text
vault://doc/{doc_id}
```

## 7. Optional: Install As A uv Tool

For daily use:

```bash
uv tool install --from . severino-vault-mcp
```

Then configure your MCP client with:

```text
command: severino-vault-mcp
```

Upgrade after pulling new repo changes:

```bash
uv tool upgrade severino-vault-mcp
```

## 8. Optional: Enable Secret-Adjacent Local Unlock

By default, `secret_adjacent` docs return metadata only.

To allow one-request local unlocks on macOS, first store a salted unlock hash
in Keychain:

```bash
HASH="$(python3 -c 'import getpass,hashlib,os; p=getpass.getpass("MCP unlock phrase: "); s=os.urandom(16); print(f"sha256:{s.hex()}:{hashlib.sha256(s + p.encode()).hexdigest()}")')"
security add-generic-password -U \
  -s severino-vault-mcp \
  -a secret-adjacent-unlock \
  -w "$HASH"
```

Then add this env var to the MCP client config:

```json
{
  "env": {
    "SVMC_ALLOW_SECRET_ADJACENT_UNLOCK": "1"
  }
}
```

Never type the unlock phrase into AI chat. The prompt is local-only.

## 9. Common Adoption Checks

Run these locally before opening a PR or publishing your own fork:

```bash
uv run pytest
uv run ruff check .
```

Read:

- `README.md` for full project overview.
- `CONTRIBUTING.md` for development workflow.
- `STRUCTURE.md` for the repository map.
- `docs/demo.md` for a sample assistant transcript.
- `docs/migration-guide.md` for messy vault migration.
- `docs/testing-ci.md` for test and CI details.
- `docs/ai-safety-security.md` for safety model and threat assumptions.
