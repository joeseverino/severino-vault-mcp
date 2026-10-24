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
`restricted` docs unless Touch ID unlocks them, and validates every write
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
| `edu` | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml`; off when the file is missing | education (built in) | none |
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

<!-- tools:begin, generated from the registered tools by TestReadmeToolsTable -->
| Tool | Does |
|---|---|
| `find` | Find docs in a vault. Use this first for any question the vault may answer. |
| `log_entries` | Work an append-only log note: one top-level bullet per entry, closed once its line carries " → " and a resolution. |
| `move_doc` | Move or rename a registered doc; its doc_id and frontmatter stay as they are. |
| `move_folder` | Move or rename a folder with everything in it (notes, Reports/, assets), then point every link at the new path. |
| `queue` | Track work in a project's queue: one Obsidian task per item in 01 Projects/&lt;project>/Queue.md (cross-cutting work in 07 Backlog/Queue.md). Kinds: next, delegated (a worker holds it), decision (the user must answer), follow-up. Status: open, returned (delegated only), closed, dropped; a box ticked in Obsidian reads as closed. Each item carries the date it was filed (created); a worker's item may carry base (the branch it lands on), owns (the paths its worker owns) and transcript (the absolute path of the worker transcript holding its brief and report). |
| `read_doc` | Read a vault doc's markdown body, gated by its sensitivity. |
| `set_frontmatter` | Create or update a doc's frontmatter, validated against the vault's schema. |
| `task_board` | A vault's task board: open work, counts, recently shipped, and the projects tasks can go in. |
| `task_write` | Change a vault's tasks. |
| `write_doc` | Create a registered doc, or edit one section of its body, validated against the vault's schema. |
<!-- tools:end -->

A failure is `{"ok": false, "error": ...}` plus `next_steps`, the calls that
recover from it, each naming one of these tools; a `find` with no hits carries
them too. `find` and `read_doc` ask Claude Code to load them upfront
(`anthropic/alwaysLoad`), and `read_doc` and `task_board` set
`anthropic/maxResultSizeChars` so a long doc or a full lean board stays inline.

Vault-specific: each provider's tools. The edu dataset is the CLI's
`export education`.

How an agent should use them (find before prose, answer in the doc's words,
the sensitivity rules, writes) is the `vault` skill, `skills/vault/SKILL.md`;
the server's instructions point to it.

Resources: `vault://{vault}/quick-index` and `vault://{vault}/doc/{doc_id}`.

## Run it

```bash
go test -race ./...
scripts/check.sh
SVMC_VAULT_PATH=examples/sample-vault go run ./cmd/severino-vault-mcp
```

Install for Claude Code:

```bash
go install ./cmd/severino-vault-mcp
claude mcp add severino-vault-mcp severino-vault-mcp
```

Or install it as a Claude Code plugin, which declares the same server
(`.mcp.json`, the binary on `PATH`) and adds the `vault` skill. The repo is its
own marketplace:

```bash
go install ./cmd/severino-vault-mcp
/plugin install severino-vault-mcp --marketplace joeseverino/severino-vault-mcp
```

A server you already registered with the same command wins over the plugin's
copy, so its tool names stay `mcp__severino-vault-mcp__*`; the plugin then only
adds the skill. Without one, the plugin's tools are
`mcp__plugin_severino-vault-mcp_severino-vault-mcp__*`.

After upgrading the binary, restart Claude Code sessions, not just `/mcp`:
`find` and `read_doc` load upfront (`anthropic/alwaysLoad`) and keep the
schema they started with.

Validate a vault before wiring it:

```bash
SVMC_VAULT_PATH=/path/to/vault severino-vault-mcp doctor --propose
```

`ripgrep` is required for `find(by="text")`. `fd` walks the vault when it's on
`PATH` (hidden and ignored files skipped); otherwise a built-in walk does.

## CLI

With no subcommand (or `serve`) the binary serves MCP. Subcommands run one
governed call against the labs vault and print JSON: `doctor`, `find`, `read`,
`brief`, `task-*`, `promote-note`, `update-frontmatter`, `move-doc`,
`touch-reviewed`, `backfill-aliases`, `daily-write`, `schema`, `hq-manifest`,
`describe`, and `export education` (the dataset jseverino.com and
resume-engine read). `describe` emits the surface as a
[Cordon](https://github.com/joeseverino/cordon) contract, generated from the
same command table that parses arguments.

`schema --json` is the frozen enum contract HQ commits; `schema --contract` and
`schema --fingerprint` give the full versioned profile and its hash, and
`schema --json-schema [--vault labs|edu]` the frontmatter rules as a JSON
Schema for off-the-shelf validators and editors.
`--fingerprint` hashes the binary's own Go sources, so `tools doctor` can tell
a stale install from the source tree.

## Configuration

Copy `config.example.toml` to the vault's config path. Environment variables
override the labs config:

| Var | Default | Purpose |
|---|---|---|
| `SVMC_VAULT_PATH` | `~/Documents/vault` | Vault root |
| `SVMC_INDEXED_DIRS` | `01 Projects:02 Infrastructure:03 Runbooks:07 Backlog` | Folders the loader indexes |
| `SVMC_ARCHIVE_DIR` | `99 Archive` with the default indexed dirs, else unset | The archive: indexed apart, left out of `find` unless `include_archived` |
| `SVMC_ALIASES_PATH` | `<vault>/.svmc/aliases.toml` | Phrase to `doc_id` aliases |
| `SVMC_CACHE_SECONDS` | `30` | How long the index stays warm |
| `SVMC_RESTRICTED_UNLOCK_REF` | unset | `op://` reference to the 1Password unlock item; unset keeps restricted bodies withheld |
| `SVMC_RESTRICTED_UNLOCK_AUDIT_LOG` | `~/.local/state/severino-vault-mcp/audit.log` | Unlock attempts; never bodies |

edu and providers read only their own TOML; labs overrides never leak into them.

## Sensitivity

| Sensitivity | `read_doc` returns |
|---|---|
| `public`, `internal` | The body. |
| `sensitive` | The body plus an advisory. |
| `restricted` | Metadata only. The body needs `include_restricted=True`, a configured `unlock_ref`, and a 1Password read of it approved with Touch ID, for one read. A server with `OP_SERVICE_ACCOUNT_TOKEN` in its environment never unlocks. |

When in doubt, mark a doc `restricted`. The gate limits what reaches AI context
through this server; it is not a sandbox, and a client with direct file access
can still read the file. See [`docs/ai-safety-security.md`](docs/ai-safety-security.md).

## Docs

[`QUICKSTART.md`](QUICKSTART.md) · [`AGENTS.md`](AGENTS.md) ·
[`docs/architecture.md`](docs/architecture.md) ·
[`docs/ai-tool-contract.md`](docs/ai-tool-contract.md) ·
[`docs/migration-guide.md`](docs/migration-guide.md) ·
[`docs/testing-ci.md`](docs/testing-ci.md) ·
[`docs/release-checklist.md`](docs/release-checklist.md)

## License

MIT. See `LICENSE`.
