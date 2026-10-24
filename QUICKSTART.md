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

## Install The Binary

```bash
go install -trimpath github.com/joeseverino/severino-vault-mcp/v3/cmd/severino-vault-mcp@latest
```

Or download the archive for your platform (darwin or linux, amd64 or arm64)
from the [latest release](https://github.com/joeseverino/severino-vault-mcp/releases/latest),
check it against `SHA256SUMS`, and put `severino-vault-mcp` on your `PATH`. Each
archive carries a build provenance attestation:

```bash
gh attestation verify severino-vault-mcp_<tag>_<os>_<arch>.tar.gz --repo joeseverino/severino-vault-mcp
```

The steps below use a clone, for the sample vault and the tests.

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

## 7. Optional: Install From The Clone

For daily use, install from the clone instead of the released binary:

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

By default, `restricted` docs return metadata only. To release one body per
request on macOS, set `[restricted].unlock_ref` (or
`SVMC_RESTRICTED_UNLOCK_REF` in the MCP client's `env`) to the `op://`
reference of a dedicated 1Password item, and check that
`op read --no-newline "<that reference>" >/dev/null && echo ok` prompts for
Touch ID. The conditions, the setup rationale and the audit log are in
[`docs/ai-safety-security.md`](docs/ai-safety-security.md#secret-adjacent-release-flow).

## 9. Common Adoption Checks

Run these locally before opening a PR or publishing your own fork:

```bash
mise run check
```

Read:

- `README.md` for full project overview.
- `CONTRIBUTING.md` for development workflow.
- `AGENTS.md` for the repository map and how to verify a change.
- `docs/demo.md` for a sample assistant transcript.
- `docs/architecture.md` for runtime shape and extension pattern.
- `docs/ai-tool-contract.md` for model-facing tool-selection rules.
- `docs/migration-guide.md` for messy vault migration.
- `docs/testing-ci.md` for test and CI details.
- `docs/ai-safety-security.md` for safety model and threat assumptions.
