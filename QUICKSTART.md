# Quickstart

This guide gets `severino-knowledge-router` running as a local stdio MCP server
against either the included sample vault or your own Obsidian-style vault.

## Prerequisites

- Python 3.11+
- `uv`
- `ripgrep` (`rg`) for body search
- An MCP client such as Claude Code, Claude Desktop, Cline, or another MCP host

Install the local toolchain on macOS:

```bash
brew install uv ripgrep
```

## 1. Clone and Test

```bash
git clone git@github.com:joeseverino/severino-knowledge-router.git
cd severino-knowledge-router
uv sync --extra dev
uv run pytest
uv run ruff check .
```

Expected result:

```text
pytest passes
ruff reports All checks passed
```

## 2. Run Against the Sample Vault

The sample vault is safe demo data and does not require access to a private
Obsidian vault.

```bash
SKR_VAULT_PATH=examples/sample-vault uv run severino-knowledge-router
```

This starts the MCP server on stdio. It waits for an MCP client to talk to it,
so it will not print a web URL.

## 3. Wire Claude Code

From the repo directory:

```bash
claude mcp add \
  -e SKR_VAULT_PATH="$PWD/examples/sample-vault" \
  severino-knowledge-router \
  -- uv run --directory "$PWD" severino-knowledge-router
```

Then ask your MCP client to verify:

```text
Use the severino-knowledge-router MCP. Read vault://quick-index and tell me the first demo workflow.
```

Expected behavior:

- The client can see `vault://quick-index`.
- The client can read `vault://doc/rb-generate-homelab-cert`.
- `find_runbook("generate homelab certificate")` returns `rb-generate-homelab-cert`.
- `vault://doc/infra-offline-ca` withholds the body because it is `secret_adjacent`.

## 4. Wire Claude Desktop

Edit:

```text
~/Library/Application Support/Claude/claude_desktop_config.json
```

Example using the sample vault:

```json
{
  "mcpServers": {
    "severino-knowledge-router": {
      "command": "uv",
      "args": [
        "run",
        "--directory",
        "/absolute/path/to/severino-knowledge-router",
        "severino-knowledge-router"
      ],
      "env": {
        "SKR_VAULT_PATH": "/absolute/path/to/severino-knowledge-router/examples/sample-vault"
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

Point the MCP at your vault:

```bash
SKR_VAULT_PATH="/absolute/path/to/your/vault" uv run severino-knowledge-router
```

If your vault uses different folders:

```bash
SKR_VAULT_PATH="/absolute/path/to/your/vault" \
SKR_INDEXED_DIRS="Projects:Infrastructure:Runbooks" \
uv run severino-knowledge-router
```

## 6. Recommended Vault Docs

For best results, create:

- A Quick Index doc with `doc_id: report-playbook-mcp-index`.
- Runbooks with stable `rb-*` IDs.
- Infrastructure docs with stable `infra-*` IDs.
- Consistent `sensitivity` values: `public`, `internal`, `sensitive`, or `secret_adjacent`.

The Quick Index backs:

```text
vault://quick-index
```

Known docs can be read through:

```text
vault://doc/{doc_id}
```

## 7. Optional: Install as a uv Tool

For daily use:

```bash
uv tool install --from . severino-knowledge-router
```

Then configure your MCP client with:

```text
command: severino-knowledge-router
```

Upgrade after pulling new repo changes:

```bash
uv tool upgrade severino-knowledge-router
```

## 8. Optional: Enable Secret-Adjacent Local Unlock

By default, `secret_adjacent` docs return metadata only.

To allow one-request local unlocks on macOS, first store a salted unlock hash
in Keychain:

```bash
HASH="$(python3 -c 'import getpass,hashlib,os; p=getpass.getpass("MCP unlock phrase: "); s=os.urandom(16); print(f"sha256:{s.hex()}:{hashlib.sha256(s + p.encode()).hexdigest()}")')"
security add-generic-password -U \
  -s severino-knowledge-router \
  -a secret-adjacent-unlock \
  -w "$HASH"
```

Then add this env var to the MCP client config:

```json
{
  "env": {
    "SKR_ALLOW_SECRET_ADJACENT_UNLOCK": "1"
  }
}
```

Never type the unlock phrase into AI chat. The prompt is local-only.

## 9. Common Adoption Checks

Run these locally before opening a PR:

```bash
uv run pytest
uv run ruff check .
```

Check the repository map:

```bash
open STRUCTURE.md
```

Read the safety model:

```bash
open docs/ai-safety-security.md
```

## More Detail

- `README.md` — full project overview
- `STRUCTURE.md` — file-by-file repository map
- `docs/demo.md` — sample assistant transcript
- `docs/testing-ci.md` — test and CI details
- `docs/ai-safety-security.md` — safety model and threat assumptions
