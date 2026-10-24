# Quickstart

This guide gets `severino-vault-mcp` running as a local stdio MCP server
against either the included sample vault or your own Obsidian-style operations
vault.

## Prerequisites

- Go 1.27+ (the `toolchain` line in `go.mod` fetches it if yours is older)
- `ripgrep` (`rg`) for body search
- An MCP client such as Claude Code, Claude Desktop, Cline, or another MCP host

macOS toolchain:

```bash
brew install go ripgrep
```

## 1. Clone And Test

```bash
git clone git@github.com:joeseverino/severino-vault-mcp.git
cd severino-vault-mcp
go test ./...
```

Expected result: every package reports `ok`.

## 2. Run The Sample Vault

The sample vault is safe network/security operations demo data. It does not
require access to a private vault.

```bash
SVMC_VAULT_PATH=examples/sample-vault go run ./cmd/severino-vault-mcp
```

This starts the MCP server on stdio. It waits for an MCP client to talk to it,
so it will not print a web URL.

## 3. Wire Claude Code

From the repo directory, build the binary and register it:

```bash
go build -trimpath -o bin/severino-vault-mcp ./cmd/severino-vault-mcp
claude mcp add \
  -e SVMC_VAULT_PATH="$PWD/examples/sample-vault" \
  severino-vault-mcp \
  -- "$PWD/bin/severino-vault-mcp"
```

Then ask your MCP client to verify:

```text
Use the severino-vault-mcp MCP. Read vault://labs/quick-index and tell me the first demo workflow.
```

Expected behavior:

- The client can see `vault://labs/quick-index`.
- The client can read `vault://labs/doc/rb-generate-internal-cert`.
- `find("generate internal certificate")` returns
  `rb-generate-internal-cert`.
- `vault://labs/doc/infra-offline-ca` withholds the body because it is
  `restricted`.

Validate the sample vault from another terminal:

```bash
SVMC_VAULT_PATH=examples/sample-vault bin/severino-vault-mcp doctor
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
      "command": "/absolute/path/to/severino-vault-mcp/bin/severino-vault-mcp",
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
bin/severino-vault-mcp
```

Before connecting a messy vault to an MCP client, run:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" \
bin/severino-vault-mcp doctor --propose
```

Fix missing or invalid frontmatter until `doctor` reports no errors.

## 6. Recommended Vault Docs

For best results, create:

- A Quick Index doc with `doc_id: report-playbook-mcp-index`.
- Runbooks with stable `rb-*` IDs.
- Infrastructure docs with stable `infra-*` IDs.
- Project indexes with stable `project-*` IDs.
- Deliberate `sensitivity` values: `public`, `internal`, `sensitive`, or
  `restricted`.

The Quick Index backs:

```text
vault://labs/quick-index
```

Known docs can be read through:

```text
vault://labs/doc/{doc_id}
```

## 7. Optional: Install On PATH

For daily use:

```bash
go install -trimpath ./cmd/severino-vault-mcp
```

`-trimpath` strips the local checkout path from the binary, so builds are
reproducible across machines.

Then configure your MCP client with:

```text
command: severino-vault-mcp
```

Rerun `go install -trimpath ./cmd/severino-vault-mcp` after pulling new repo changes.

## 8. Optional: Unlock Restricted Docs With Touch ID

By default, `restricted` docs return metadata only.

A restricted body is released for one request when a 1Password read of a
dedicated unlock item succeeds. The read goes through the 1Password desktop
app, so it asks for Touch ID every time.

1. In the 1Password app, turn on Settings > Developer > Integrate with
   1Password CLI, and install the `op` CLI.
2. Create a dedicated item (any password field will do) in a vault that no
   service account can read. A service account that could read it would
   approve its own unlock.
3. Point the config at it:

   ```toml
   [restricted]
   unlock_ref = "op://Private-Vault/Unlock-Item/password"
   ```

   or set `SVMC_RESTRICTED_UNLOCK_REF` in the MCP client's `env`.

4. Check the read prompts for Touch ID:

   ```bash
   op read --no-newline "op://Private-Vault/Unlock-Item/password" >/dev/null && echo ok
   ```

Start the MCP server without `OP_SERVICE_ACCOUNT_TOKEN` in its environment:
while it is set, every unlock is refused. The value read is discarded at once
and never logged; the audit log records each attempt and its result.

## 9. Common Adoption Checks

Run these locally before opening a PR or publishing your own fork:

```bash
scripts/check.sh
```

Read:

- `README.md` for full project overview.
- `CONTRIBUTING.md` for development workflow.
- `STRUCTURE.md` for the repository map.
- `docs/demo.md` for a sample assistant transcript.
- `docs/architecture.md` for runtime shape and extension pattern.
- `docs/ai-tool-contract.md` for model-facing tool-selection rules.
- `docs/migration-guide.md` for messy vault migration.
- `docs/testing-ci.md` for test and CI details.
- `docs/ai-safety-security.md` for safety model and threat assumptions.
