# severino-vault-mcp

[![CI](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml)
[![CodeQL](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/codeql.yml)
[![pip-audit](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/pip-audit.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/pip-audit.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/joeseverino/severino-vault-mcp/badge)](https://scorecard.dev/viewer/?uri=github.com/joeseverino/severino-vault-mcp)
![Python](https://img.shields.io/badge/python-3.11%20%7C%203.12%20%7C%203.13-blue)
![MCP](https://img.shields.io/badge/MCP-stdio%20server-green)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

One local MCP server for every Obsidian vault I keep: labs (homelab
infrastructure and runbooks), edu (coursework), and life (renewals, goals,
personal tasks). It answers from the vault instead of model memory, withholds
`restricted` docs unless they're unlocked locally, and validates every write
against that vault's schema. It runs over stdio, reads local files only, and
has no HTTP listener.

## How it fits together

The governance core (indexing, ranked section search, the sensitivity gate,
schema profiles, atomic writes, the task ledger) is the
[`severino-vault-engine`](https://github.com/joeseverino/vault-engine) library.
This repo is the host: it builds one governed context per vault and registers
each shared tool once, with a `vault` argument that routes the call to that
vault's context. Search, the gate and validation never cross vaults.

| Vault | Config | Profile | Own tools |
|---|---|---|---|
| `labs` (default) | `SVMC_CONFIG`, else `~/.config/severino-vault-mcp/config.toml` | labs | none |
| `edu` | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml`; off when the file is missing | education | `education_dataset` |
| `life` | `SVMC_LIFE_CONFIG`, else `~/.config/severino-life-mcp/config.toml`; off when `severino-life` isn't installed | life | from [`severino-life`](https://github.com/joeseverino/severino-life) |

Site work (writeups, D1, CSP, contact, headers) lives in the jseverino.com
repo's `site` CLI. The HQ docs manifest stays here until HQ owns it.

## Tools

Shared, each taking `vault`:

| Tool | Does |
|---|---|
| `find` | Search one vault. `by`: `relevance` (ranked sections, default), `system`, `project` (`related_projects`), `text` (ripgrep over bodies; restricted docs never searched). |
| `read_doc` | A doc's body, or one section of it. Restricted bodies need `include_restricted=True` plus a local unlock. |
| `set_frontmatter` | Create a frontmatter block, or update one in place. `doc_id` never changes. |
| `update_link` | Replace one exact Markdown link in a doc. |
| `task_board` | Open tasks, with the projects tasks can be filed in. |
| `task_write` | `action`: `add`, `status`, `promote` (inbox note to task), `delete`. |
| `recent_changes` | Recent vault commits in the indexed folders. |
| `daily_progress` | A daily note, for "what did I do Friday?". |

Vault-specific: `education_dataset` (edu); `reminders`, `calendar`,
`agenda`, `life_view`, `renew`, `life_ops` (life).

Resources: `vault://{vault}/quick-index` and `vault://{vault}/doc/{doc_id}`.

## Run it

```bash
uv sync --extra dev
scripts/check.sh
SVMC_VAULT_PATH=examples/sample-vault uv run severino-vault-mcp
```

Install for Claude Code, with life:

```bash
uv tool install . --with ~/Code/Assets/severino-life
claude mcp add severino-vault-mcp severino-vault-mcp
```

Validate a vault before wiring it:

```bash
SVMC_VAULT_PATH=/path/to/vault severino-vault-mcp doctor --propose
```

`ripgrep` is required for `find(by="text")`; `fd` speeds up indexing when present.

## CLI

With no subcommand the binary serves MCP. Subcommands run one governed call
against the labs vault and print JSON: `doctor`, `find`, `read`, `brief`,
`task-*`, `update-frontmatter`, `update-doc-link`, `touch-reviewed`,
`backfill-aliases`, `daily-write`, `schema`, `hq-manifest`, `describe`, and
`export education` (the dataset jseverino.com and resume-engine read).
`describe` emits the surface as a [Cordon](https://github.com/joeseverino/cordon)
contract.

`schema --json` is the frozen enum contract HQ commits; `schema --contract` and
`schema --fingerprint` give the full versioned profile and its hash.

## Configuration

Copy `config.example.toml` to the vault's config path. Environment variables
override the labs config:

| Var | Default | Purpose |
|---|---|---|
| `SVMC_VAULT_PATH` | `~/Documents/vault` | Vault root |
| `SVMC_INDEXED_DIRS` | `01 Projects:02 Infrastructure:03 Runbooks:07 Backlog` | Folders the loader indexes |
| `SVMC_ALIASES_PATH` | `<vault>/.svmc/aliases.toml` | Phrase to `doc_id` aliases |
| `SVMC_CACHE_SECONDS` | `30` | How long the index stays warm |
| `SVMC_ALLOW_RESTRICTED_UNLOCK` | `false` | Allows the local unlock prompt for restricted reads |
| `SVMC_RESTRICTED_UNLOCK_HASH_FILE` | `~/.config/severino-vault-mcp/restricted-unlock.sha256` | Salted unlock hash (Keychain is preferred) |
| `SVMC_RESTRICTED_UNLOCK_AUDIT_LOG` | `~/.local/state/severino-vault-mcp/audit.log` | Unlock attempts; never bodies |

edu and life read only their own TOML; labs overrides never leak into them.

## Sensitivity

| Sensitivity | `read_doc` returns |
|---|---|
| `public`, `internal` | The body. |
| `sensitive` | The body plus an advisory. |
| `restricted` | Metadata only. The body needs `include_restricted=True`, `SVMC_ALLOW_RESTRICTED_UNLOCK=1`, a configured salted hash, and a successful local hidden-input prompt, for one read. |

When in doubt, mark a doc `restricted`. The gate limits what reaches AI context
through this server; it is not a sandbox, and a client with direct file access
can still read the file. See [`docs/ai-safety-security.md`](docs/ai-safety-security.md).

## Docs

[`QUICKSTART.md`](QUICKSTART.md) · [`STRUCTURE.md`](STRUCTURE.md) ·
[`docs/architecture.md`](docs/architecture.md) ·
[`docs/ai-tool-contract.md`](docs/ai-tool-contract.md) ·
[`docs/migration-guide.md`](docs/migration-guide.md) ·
[`docs/testing-ci.md`](docs/testing-ci.md) ·
[`docs/release-checklist.md`](docs/release-checklist.md)

## License

MIT. See `LICENSE`.
