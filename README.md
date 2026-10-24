# severino-vault-mcp

[![CI](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/ci.yml)
[![CodeQL](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/joeseverino/severino-vault-mcp/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/joeseverino/severino-vault-mcp/badge)](https://scorecard.dev/viewer/?uri=github.com/joeseverino/severino-vault-mcp)
![Go](https://img.shields.io/badge/go-1.27-blue)
![MCP](https://img.shields.io/badge/MCP-stdio%20server-green)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A local MCP server that lets an AI assistant answer from your Obsidian vault
instead of from memory. It searches your notes by section, withholds
`restricted` docs unless you approve them, and validates every write against a
schema. It is one static Go binary that runs over stdio, reads local files only,
and has no HTTP listener.

## Try it

The repo ships a sample vault. Build the binary and point it there:

```bash
go install ./cmd/severino-vault-mcp
export SVMC_VAULT_PATH=examples/sample-vault
severino-vault-mcp find "internal certificate" --limit 2 --fields doc_id title sensitivity section section_summary
```

`find` ranks sections and returns the best one per doc, never a body (trimmed
here, output is compact JSON without `--pretty`):

```json
{
  "ok": true,
  "mode": "relevance",
  "hits": [
    {
      "doc_id": "rb-generate-internal-cert",
      "title": "Generate Internal Service Certificate",
      "sensitivity": "internal",
      "section": "goal",
      "section_summary": "Generate a signed TLS certificate for an internal service."
    },
    {
      "doc_id": "writeup-internal-https-with-a-private-ca",
      "title": "Internal HTTPS with a Private Certificate Authority",
      "sensitivity": "public",
      "section": "overview",
      "section_summary": "> Example writeup."
    }
  ],
  "match_count": 2
}
```

`read_doc` then returns just the section you need (`read` is its CLI form):

```bash
severino-vault-mcp read rb-generate-internal-cert --sections goal
```

```json
{
  "ok": true,
  "doc_id": "rb-generate-internal-cert",
  "title": "Generate Internal Service Certificate",
  "sensitivity": "internal",
  "body_released": true,
  "sections": [
    {
      "section": "goal",
      "heading_path": "Goal",
      "body": "## Goal\n\nGenerate a signed TLS certificate for an internal service.\n"
    }
  ]
}
```

The same two calls are what an assistant makes over MCP. With no subcommand the
binary serves MCP on stdio.

## Install

Requires Go 1.27 and `ripgrep`, which `find(by="text")` runs. The index
skips hidden trees and templates; a Git ignore file hides nothing inside a
configured folder.

Install the binary:

```bash
go install github.com/joeseverino/severino-vault-mcp/v3/cmd/severino-vault-mcp@latest
```

Or download the archive for your platform (darwin or linux, amd64 or arm64)
from the [latest release](https://github.com/joeseverino/severino-vault-mcp/releases/latest),
check it against `SHA256SUMS`, and put `severino-vault-mcp` on your `PATH`. To
work on the code, clone the repo and run `go install ./cmd/severino-vault-mcp`.

For Claude Code, register the binary as a server:

```bash
claude mcp add severino-vault-mcp severino-vault-mcp
```

Or install the repo as a Claude Code plugin, which declares the same server
(`.mcp.json`, the binary on `PATH`) and adds the `vault` skill. The repo is its
own marketplace, so with the binary installed, add it and install the plugin:

```bash
claude plugin marketplace add joeseverino/severino-vault-mcp
claude plugin install severino-vault-mcp@severino-vault-mcp
```

A server you already registered with the same command wins over the plugin's
copy, so its tool names stay `mcp__severino-vault-mcp__*`; the plugin then only
adds the skill. Without one, the plugin's tools are
`mcp__plugin_severino-vault-mcp_severino-vault-mcp__*`.

A running session moves to an upgraded binary on its own: at its next call
the server execs the new binary in place, on the same connection, keeps the
client's `subscriptions/listen` stream open, and asks the client to list the
tools again. Only a change to the arguments of `find` or `read_doc`, which
load upfront (`anthropic/alwaysLoad`), waits for a new session. When the new
binary can't take the session over, the old one keeps serving and the next
answer carries a `warning` saying so once; `/mcp`, then Reconnect, moves
Claude Code to the new binary.

Check a vault before wiring it:

```bash
SVMC_VAULT_PATH=/path/to/vault severino-vault-mcp doctor --propose
```

[`QUICKSTART.md`](QUICKSTART.md) walks through Claude Desktop, your own vault
and its docs. To run the tests: `go test -race ./...`, or `mise run check` for
the full CI gate.

## How it fits together

The binary indexes each vault, ranks sections for search, gates bodies by
sensitivity, validates writes against a schema profile, and keeps each
project's work queue. Each shared tool registers once with a `vault` argument that routes the
call to that vault; search, the gate and validation never cross vaults.

| Vault | Config | Profile |
|---|---|---|
| `labs` (default) | `SVMC_CONFIG`, else `~/.config/severino-vault-mcp/config.toml` | labs (built in) |
| `edu` | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml`; off when the file is missing | education (built in) |
| each provider | its `[[providers]]` entry's `config` | from the provider |

A **provider** is a separate stdio MCP server that owns a further vault's tools
and schema profile; the operator declares it in the labs config and the host
re-exports its tools. This repo knows no provider by name. The `[[providers]]`
format and what the host does with each entry are in
[`docs/architecture.md`](docs/architecture.md#providers).

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
| `write_doc` | Create a registered doc, or edit a section or a passage of its body, validated against the vault's schema. |
<!-- tools:end -->

A failure is `{"ok": false, "error": ...}` plus `next_steps`, the calls that
recover from it, each naming one of these tools; a `find` with no hits carries
them too. `find` and `read_doc` ask Claude Code to load them upfront
(`anthropic/alwaysLoad`). `read_doc` answers a long doc with its outline
and lead, so every default answer fits under Claude Code's 50,000-character
inline limit; only a whole body asked for with `fields=["body"]` can pass it.

How an agent should use them (find before prose, answer in the doc's words,
the sensitivity rules, writes) is the `vault` skill, `skills/vault/SKILL.md`;
the server's instructions point to it. Resources are
`vault://{vault}/quick-index` and `vault://{vault}/doc/{doc_id}`.

## CLI

Subcommands run one governed call against the labs vault and print JSON:
`doctor`, `find`, `read`, `brief`, `queue`,
`update-frontmatter`, `move-doc`, `touch-reviewed`, `backfill-aliases`,
`daily-write`, `schema`, `hq-manifest`, `describe`, and `export education`.
`schema --json-schema [--vault labs|edu]` prints the frontmatter rules as a JSON
Schema for off-the-shelf validators and editors; `schema --contract` and
`schema --fingerprint` give the full versioned profile and its hash.
Other programs that read this output are listed in
[`docs/integrations.md`](docs/integrations.md).

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
| `SVMC_RESTRICTED_UNLOCK_REF` | unset | `op://` reference to the 1Password unlock item; unset keeps restricted bodies withheld (macOS only, see Sensitivity) |
| `SVMC_RESTRICTED_UNLOCK_AUDIT_LOG` | `~/.local/state/severino-vault-mcp/audit.log` | Unlock attempts; never bodies |

edu and providers read only their own TOML; labs overrides never leak into them.

## Sensitivity

Each doc's `sensitivity` decides what `read_doc` returns. `public`, `internal`
and `sensitive` bodies are released (`sensitive` with an advisory); `restricted`
bodies are withheld unless a local approval succeeds.

The restricted unlock is macOS only: it needs Touch ID through the 1Password
desktop app and the `op` CLI. Elsewhere, restricted bodies stay withheld. The
gate limits what reaches AI context through this server and does not sandbox anything.
The full table and the unlock flow are in
[`docs/ai-safety-security.md`](docs/ai-safety-security.md).

## Docs

[`QUICKSTART.md`](QUICKSTART.md) · [`AGENTS.md`](AGENTS.md) ·
[`docs/architecture.md`](docs/architecture.md) ·
[`docs/integrations.md`](docs/integrations.md) ·
[`docs/ai-tool-contract.md`](docs/ai-tool-contract.md) ·
[`docs/ai-safety-security.md`](docs/ai-safety-security.md) ·
[`docs/migration-guide.md`](docs/migration-guide.md) ·
[`docs/testing-ci.md`](docs/testing-ci.md) ·
[`docs/dependencies.md`](docs/dependencies.md) ·
[`docs/release-checklist.md`](docs/release-checklist.md)

## License

MIT. See `LICENSE`.
