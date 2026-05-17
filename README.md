# severino-vault-mcp

[![CI](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml)
![Python](https://img.shields.io/badge/python-3.11%20%7C%203.12%20%7C%203.13-blue)
![MCP](https://img.shields.io/badge/MCP-stdio%20server-green)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Local-first MCP server for turning an Obsidian-style operations vault into
usable AI context without exposing credential-adjacent material by default.

It is built for security-minded operators, consultants, and small teams who
keep runbooks, infrastructure notes, decision records, and client/lab
procedures in markdown. The server runs over stdio, reads local files only,
and gives MCP clients a reliable way to answer operational questions from the
operator's actual documentation instead of generic model memory.

## Why It Is Useful

- Grounds AI assistants in real runbooks before they answer.
- Exposes stable resources such as `vault://quick-index` and
  `vault://doc/{doc_id}`.
- Searches vault metadata and markdown bodies without requiring a database.
- Enforces a sensitivity gate for credential-adjacent procedures.
- Provides narrow, validated frontmatter write tools for maintaining a vault.
- Uses a copyable TOML config and environment overrides for fast adoption.
- Has no HTTP listener, no hosted service, and no remote auth surface.

## Who This Is For

Best fit:

- Homelab and small-team operations documentation.
- MSP or client operations notes where procedures need to be repeatable.
- Cybersecurity lab runbooks and training environments.
- Incident response, recovery, and infrastructure maintenance notes.
- Internal "how do I operate this system?" documentation.

Not a great fit:

- Generic personal note-taking with no operational structure.
- Vaults where you do not want to add frontmatter.
- Multi-user hosted knowledge bases that need server-side auth.
- Replacing secret management, password vaults, or filesystem permissions.

## Quick Start

```bash
git clone git@github.com:joeseverino/severino-vault-mcp.git
cd severino-vault-mcp
uv sync --extra dev
uv run pytest
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

That starts a stdio MCP server pointed at the included sample vault. Wire it
to an MCP client, then ask the client to read `vault://quick-index`.

Before wiring a real vault, validate it:

```bash
SVMC_VAULT_PATH=/absolute/path/to/your/vault severino-vault-mcp doctor --propose
```

For a persistent local install:

```bash
uv tool install --from . severino-vault-mcp
mkdir -p ~/.config/severino-vault-mcp
cp config.example.toml ~/.config/severino-vault-mcp/config.toml
```

Edit `~/.config/severino-vault-mcp/config.toml` and set `vault.path` to your
vault root.

## MCP Surface

| Resource | Type | What it returns |
|---|---|---|
| `vault://quick-index` | resource | The Quick Index navigation hub (`report-playbook-mcp-index`) |
| `vault://doc/{doc_id}` | resource template | One stable vault doc rendered as markdown, with the same sensitivity policy as `read_doc` |

| Tool | Read or write | What it answers |
|---|---|---|
| `find_runbook(query, limit=5)` | read | "How do I add an HTTPS proxy host?" |
| `lookup_system(name)` | read | "Tell me about AdGuard Home" |
| `read_doc(doc_id)` | read | Returns markdown bodies for `public`, `internal`, and `sensitive` docs. `secret_adjacent` requires explicit request plus local unlock. |
| `inventory_for_project(slug)` | read | "What docs are part of client-edge-dns?" |
| `recent_changes(days=7)` | read | Recent vault commits within indexed folders |
| `add_frontmatter(...)` | write | Prepends a validated frontmatter block to a vault doc that does not have one. |
| `update_frontmatter(...)` | write | Updates frontmatter fields. `doc_id` is immutable. |

CLI helper:

```bash
severino-vault-mcp doctor --propose
```

Validates required frontmatter fields in the configured vault and prints
starter frontmatter for markdown files that are not yet indexed.

## Adopt It For Your Vault

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

Recommended docs:

- A Quick Index doc with `doc_id: report-playbook-mcp-index`.
- Runbooks with stable `rb-*` IDs.
- Infrastructure notes with stable `infra-*` IDs.
- Project indexes with stable `project-*` IDs.
- `sensitivity` values set deliberately: `public`, `internal`, `sensitive`,
  or `secret_adjacent`.

Real vaults are usually messy. Start with the validator:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" severino-vault-mcp doctor
```

Add `--propose` to print starter frontmatter for files that are missing it:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" severino-vault-mcp doctor --propose
```

Point the server at your vault with either TOML:

```toml
[vault]
path = "/absolute/path/to/your/vault"
indexed_dirs = ["01 Projects", "02 Infrastructure", "03 Runbooks"]
```

Or with environment variables:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" \
SVMC_INDEXED_DIRS="01 Projects:02 Infrastructure:03 Runbooks" \
severino-vault-mcp
```

## MCP Client Examples

Claude Code after `uv tool install`:

```bash
claude mcp add severino-vault-mcp severino-vault-mcp
```

Claude Code from a checkout:

```bash
claude mcp add severino-vault-mcp \
  -e SVMC_VAULT_PATH="$PWD/examples/sample-vault" \
  -- uv run --no-editable --directory "$PWD" severino-vault-mcp
```

Claude Desktop:

```json
{
  "mcpServers": {
    "severino-vault-mcp": {
      "command": "severino-vault-mcp",
      "env": {
        "SVMC_VAULT_PATH": "/absolute/path/to/your/vault"
      }
    }
  }
}
```

## Configuration

`config.example.toml` is the recommended starting point. Copy it to:

```text
~/.config/severino-vault-mcp/config.toml
```

Environment variables override the config file and are useful for demos, CI,
and one-off runs.

| Var | Default | Purpose |
|---|---|---|
| `SVMC_CONFIG` | `~/.config/severino-vault-mcp/config.toml` | TOML config path |
| `SVMC_VAULT_PATH` | `~/Documents/vault` | Vault root |
| `SVMC_INDEXED_DIRS` | `01 Projects:02 Infrastructure:03 Runbooks` | Colon-separated subdirs the loader recurses into |
| `SVMC_METADATA_URL` | unset | Optional downstream metadata-system URL |
| `SVMC_CACHE_SECONDS` | `30` | How long the in-memory vault index stays warm |
| `SVMC_ALLOW_SECRET_ADJACENT_UNLOCK` | `false` | Enables hidden local unlock prompts for `read_doc(..., include_secret_adjacent=True)` |
| `SVMC_SECRET_ADJACENT_UNLOCK_HASH` | unset | Salted unlock hash, mainly for tests or temporary local use |
| `SVMC_SECRET_ADJACENT_UNLOCK_HASH_FILE` | `~/.config/severino-vault-mcp/secret-adjacent-unlock.sha256` | Local file containing the salted unlock hash |
| `SVMC_SECRET_ADJACENT_UNLOCK_KEYCHAIN_SERVICE` | `severino-vault-mcp` | macOS Keychain service name for the salted unlock hash |
| `SVMC_SECRET_ADJACENT_UNLOCK_KEYCHAIN_ACCOUNT` | `secret-adjacent-unlock` | macOS Keychain account name for the salted unlock hash |
| `SVMC_SECRET_ADJACENT_UNLOCK_AUDIT_LOG` | `~/.local/state/severino-vault-mcp/audit.log` | Local audit log for unlock attempts; no body content is logged |

## Sensitivity Policy

| Sensitivity | `read_doc` returns |
|---|---|
| `public` | Full body. Safe to publish. |
| `internal` | Full body. Private operational context, but safe to enter an AI chat you control. |
| `sensitive` | Full body + advisory. Private but still safe to enter chat when handled deliberately. |
| `secret_adjacent` | Metadata only by default. May expose credentials, key paths, recovery flows, internal auth procedures, or escalation paths. Full body requires explicit request plus local unlock. |

Use `sensitive` only for material that is private but acceptable to place in
the assistant context. If a document could reveal credentials, private key
locations, recovery procedures, token rotation steps, break-glass access,
internal authentication flows, or escalation paths, mark it
`secret_adjacent`.

When in doubt, choose `secret_adjacent`. Mislabeling a secret-bearing procedure
as merely `sensitive` will cause the body to be returned to the MCP client.

## Threat Model

- The server runs locally over stdio under your user account.
- It does not expose an HTTP listener or remote API.
- It can read files your local account can read inside the configured indexed
  vault directories.
- It reduces accidental disclosure to AI chat context; it does not sandbox a
  malicious MCP host.
- A compromised MCP host can still ask for allowed tools. The local unlock
  prompt is the final boundary for `secret_adjacent` body release.
- Store actual credentials and private keys outside indexed markdown whenever
  possible.

To release one secret-adjacent body through the MCP, all conditions must pass:

- The caller requests `read_doc(..., include_secret_adjacent=True)`.
- `SVMC_ALLOW_SECRET_ADJACENT_UNLOCK=1` is set in the local MCP environment.
- A salted unlock hash is configured in macOS Keychain, a local hash file, or
  `SVMC_SECRET_ADJACENT_UNLOCK_HASH`.
- The local hidden-input prompt succeeds.

Do not type the unlock phrase into AI chat. The prompt is local-only, and the
unlock is valid for one `read_doc` request.

Recommended macOS setup stores the salted hash in Keychain, not the phrase:

```bash
HASH="$(python3 -c 'import getpass,hashlib,os; p=getpass.getpass("MCP unlock phrase: "); s=os.urandom(16); print(f"sha256:{s.hex()}:{hashlib.sha256(s + p.encode()).hexdigest()}")')"
security add-generic-password -U \
  -s severino-vault-mcp \
  -a secret-adjacent-unlock \
  -w "$HASH"
```

## Sample Vault

The included sample vault models a small network/security operations
environment:

- Client edge DNS and internal hostname resolution.
- AdGuard Home as a DNS/security filtering component.
- Nginx Proxy Manager for browser-facing internal services.
- Local PKI and an offline CA example that exercises `secret_adjacent`
  withholding.

It is intentionally safe demo data, but it follows the same frontmatter
contract as a real operations vault.

## Documentation

| Doc | Purpose |
|---|---|
| `QUICKSTART.md` | Command-first setup guide for sample-vault and real-vault adoption. |
| `CONTRIBUTING.md` | Local development, issue, PR, and release guidance. |
| `STRUCTURE.md` | File-by-file repository map. |
| `docs/demo.md` | Short transcript of the intended MCP assistant flow. |
| `docs/migration-guide.md` | Messy-vault onboarding, doctor usage, and bad-doc-to-fixed-doc examples. |
| `docs/testing-ci.md` | Local test commands, CI matrix, and test coverage notes. |
| `docs/release-checklist.md` | Public release checklist. |
| `docs/ai-safety-security.md` | AI safety model, sensitivity gate, local unlock, audit logging, and threat assumptions. |
| `config.example.toml` | Copyable local configuration template. |
| `.github/SECURITY.md` | GitHub vulnerability reporting policy. |

## Status

v2.0.0. Stable local stdio MCP for routing AI assistants to an
Obsidian-style operational vault, with resource discovery, reproducible sample
vault, CI, docs, config-file support, and secret-adjacent local unlock controls.
Downstream metadata-system integration is intentionally optional.

## License

MIT. See `LICENSE`.
