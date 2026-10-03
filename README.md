# severino-vault-mcp

[![CI](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml)
[![CodeQL](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/joeseverino/severino-vault-mcp/badge)](https://scorecard.dev/viewer/?uri=github.com/joeseverino/severino-vault-mcp)
![Go](https://img.shields.io/badge/go-1.27-blue)
![MCP](https://img.shields.io/badge/MCP-stdio%20server-green)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

One local MCP server for every Obsidian vault I keep: labs (homelab
infrastructure and runbooks), edu (coursework), and any private vault a
provider adds. It answers from the vault instead of model memory, withholds
`restricted` docs unless they're unlocked locally, and validates every write
against that vault's schema. It is one static Go binary that runs over stdio,
reads local files only, and has no HTTP listener.

## How it fits together

The binary indexes each vault, ranks sections for search, gates bodies by
sensitivity, validates writes against a schema profile, and keeps the task
ledger. Each shared tool registers once with a `vault` argument that routes the
call to that vault; search, the gate and validation never cross vaults.

| Vault | Config | Profile | Own tools |
|---|---|---|---|
| `labs` (default) | `SVMC_CONFIG`, else `~/.config/severino-vault-mcp/config.toml` | labs (built in) | none |
| `edu` | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml`; off when the file is missing | education (built in) | `education_dataset` |
| each provider | its `[[providers]]` entry's `config` | from the provider | from the provider |

A private vault brings its own domain through a **provider**: a separate stdio
MCP server that owns that vault's tools and schema profile. This repo knows
nothing about any provider. The operator declares them in the labs config, and
the host starts each one, reads the vault's name and profile from
`vault-provider://profile`, re-exports its tools, and appends its
instructions:

```toml
[[providers]]
command = "example"                         # the provider's stdio MCP server
args    = ["mcp-provider"]
config  = "~/.config/example/config.toml"   # the vault's [vault] path and indexed_dirs
env     = { EXAMPLE_HOME = "~/example" }    # optional, passed to the provider
```

`command` and `config` are required. The provider gets the process
environment minus `SVMC_*`, plus `SVMC_CONFIG=<config>` and its `env`. A
provider that fails to start, or names a vault that already exists, is logged
and skipped. There are no default providers.

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

Vault-specific: `education_dataset` (edu), plus each provider's tools.

Resources: `vault://{vault}/quick-index` and `vault://{vault}/doc/{doc_id}`.

## Run it

```bash
go test ./...
scripts/check.sh
SVMC_VAULT_PATH=examples/sample-vault go run ./cmd/severino-vault-mcp
```

Install for Claude Code:

```bash
go install ./cmd/severino-vault-mcp
claude mcp add severino-vault-mcp severino-vault-mcp
```

Validate a vault before wiring it:

```bash
SVMC_VAULT_PATH=/path/to/vault severino-vault-mcp doctor --propose
```

`ripgrep` is required for `find(by="text")`. `fd` walks the vault when it's on
`PATH` (hidden and ignored files skipped); otherwise a built-in walk does.

## CLI

With no subcommand (or `serve`) the binary serves MCP. Subcommands run one
governed call against the labs vault and print JSON: `doctor`, `find`, `read`,
`brief`, `task-*`, `promote-note`, `update-frontmatter`, `update-doc-link`,
`touch-reviewed`, `backfill-aliases`, `daily-write`, `schema`, `hq-manifest`,
`describe`, `unlock-hash` (the argon2id hash for the restricted unlock), and
`export education` (the dataset jseverino.com and
resume-engine read). `describe` emits the surface as a
[Cordon](https://github.com/joeseverino/cordon) contract, generated from the
same command table that parses arguments.

`schema --json` is the frozen enum contract HQ commits; `schema --contract` and
`schema --fingerprint` give the full versioned profile and its hash.
`--fingerprint` hashes the binary's own Go sources, so `tools doctor` can tell
a stale install from the source tree.

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
| `SVMC_RESTRICTED_UNLOCK_HASH_FILE` | `~/.config/severino-vault-mcp/restricted-unlock.phc` | argon2id unlock hash from `unlock-hash` (Keychain is preferred) |
| `SVMC_RESTRICTED_UNLOCK_AUDIT_LOG` | `~/.local/state/severino-vault-mcp/audit.log` | Unlock attempts; never bodies |

edu and providers read only their own TOML; labs overrides never leak into them.

## Sensitivity

| Sensitivity | `read_doc` returns |
|---|---|
| `public`, `internal` | The body. |
| `sensitive` | The body plus an advisory. |
| `restricted` | Metadata only. The body needs `include_restricted=True`, `SVMC_ALLOW_RESTRICTED_UNLOCK=1`, an argon2id hash from `unlock-hash`, and a successful local hidden-input prompt, for one read. |

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
