# Architecture

`severino-vault-mcp` is one local stdio MCP server for Obsidian vaults. It indexes each vault's configured folders, answers from the docs instead
of model memory, withholds `restricted` bodies unless they're unlocked locally,
and validates writes against each vault's schema. No HTTP listener, no database,
no shell bridge.

## Runtime shape

```text
MCP client / host
  starts one local stdio process
      |
      v
severino-vault-mcp (one Go binary)
  vaults.Build(): one core.Vault per vault (labs, edu, each provider's)
  mcpserver.New(): the shared tools, each with a `vault` argument,
                   plus each provider's tools
      |                                   |
      v                                   v
local markdown vaults            provider processes over stdio
                                 (each: its own tools and profile)
```

The server runs under the local user account and reads only files under each
vault's root and indexed folders.

## Vaults

| Vault | When present | Config | Profile | Own tools |
|---|---|---|---|---|
| `labs` | always (default) | `SVMC_CONFIG`, else `~/.config/severino-vault-mcp/config.toml`, plus `SVMC_*` overrides | labs | none |
| `edu` | its config file exists | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml` | education | none |
| provider's | the provider connects | its `[[providers]]` entry's `config` | from the provider | from the provider |

`vaults.Build()` loads labs first with the process environment. edu loads from
its own TOML with an environment scrubbed of `SVMC_*`, so a labs override can't
redirect it.

## Providers

A provider is a stdio MCP server that owns one private vault's domain. The host
knows a provider only by its `[[providers]]` entry in the labs config:

```toml
[[providers]]
command = "example"                         # the provider's stdio MCP server
args    = ["mcp-provider"]
config  = "~/.config/example/config.toml"   # the vault's [vault] path and indexed_dirs
env     = { EXAMPLE_HOME = "~/example" }    # optional, passed to the provider
```

For each entry the host starts `command` with the process environment minus
`SVMC_*`, plus `SVMC_CONFIG=<config>` and `env`, then:

- reads `vault-provider://profile`, the vault's schema profile as a contract
  (`schema.FromContract`). Its `name` is the vault's name, so the `vault` enum
  is labs, edu (when configured), then each connected provider's vault;
- loads the vault's `[vault]` settings from `config`, so the core tools serve
  its docs and queues, and writes validate against the provider's profile;
- lists the provider's tools and re-exports them unchanged (a name that
  collides with a core tool is skipped and logged);
- appends the instructions from the provider's initialize result.

There are no compiled-in providers and no default commands. An entry missing
`command` or `config`, a provider that fails to start, and a vault name that
already exists are each logged and skipped; the rest of the server comes up.

## Code

The governance engine is `internal/`: indexing, alias resolution and
duplicate-ID exclusion, section chunking and ranking, the sensitivity gate and
the 1Password unlock, schema profiles, frontmatter parsing and serialization, atomic
writes, path validation, the work queue, daily notes and `doctor`. On top:

Each vault's index builds once the client finishes connecting, so the first
search finds it ready and the handshake never waits on it. A call trusts the
index for `SVMC_CACHE_SECONDS` (default 30); after that it
stats every note, the aliases file and the projects folder, and rebuilds
only when one of them changed.

- `internal/core`: the shared tools, transport-free.
- `internal/vaults`, `internal/provider`: composition.
- `internal/mcpserver`: the MCP adapter and the instructions block.
- `internal/education`: the edu dataset, shared by the tool and `export education`.
- `internal/hqmanifest`: the docs manifest HQ reads (see [`integrations.md`](integrations.md)).
- `internal/cli`: subcommands over the labs vault. Every result goes through one
  emitter (compact or `--pretty`, `ok` to exit code), and `describe` projects
  the command table to a contract.

Programs that read this server's output (HQ, the site, Cordon) are listed in
[`integrations.md`](integrations.md).

## Data contract

Each vault's docs are markdown with YAML frontmatter under its indexed
folders (labs default: `01 Projects`, `02 Infrastructure`, `03 Runbooks`,
`04 Reference`, `07 Backlog`). Read-only folders (`read_only_dirs`, labs
default `00 Inbox`, `05 Writeups`, `06 Pages`, applied only while
`indexed_dirs` is the default) are served by `find` and `read_doc`; writes
refuse them and `validate` skips them, since their notes carry their own
frontmatter. Minimal labs doc:

```yaml
---
doc_id: rb-example
title: Example Runbook
doc_type: runbook
system: Example System
environment: other
status: active
sensitivity: internal
---
```

| Field | Purpose |
|---|---|
| `doc_id` | Stable identifier for `read_doc`, `vault://{vault}/doc/{doc_id}`, aliases and related refs. Immutable. |
| `title` | Label in search and read responses. |
| `doc_type` | Validated against the vault's profile. |
| `system` | The system or service the doc covers (`find(system=...)` filters on it). |
| `environment`, `status`, `tags` | Context, lifecycle and discovery. |
| `sensitivity` | Body release behavior. |

A doc without a `doc_id` isn't indexed until `set_frontmatter` registers it. A
writeup or page
(`<folder>/<slug>/index.md`) without one is `writeup-<slug>` or `page-<slug>`,
the ids the HQ manifest uses. A doc without a `title` is named by its file.

`doc_id` is a uniqueness boundary: duplicates are excluded from lookup and
search, direct reads return every conflicting path, and `doctor` reports them.

## Tools

| Surface | Purpose |
|---|---|
| `vault://{vault}/quick-index` | The vault's navigation hub (`doc_id: report-playbook-mcp-index`). |
| `vault://{vault}/doc/{doc_id}` | One doc body, subject to sensitivity. |
| `find` | `by`: `relevance` (ranked sections plus Quick Index hints) or `text` (several words match docs holding any of them, ranked by BM25 relevance to all of them, a partial hit naming its `missing_words`; one token or a regex goes to ripgrep; zero hits fall back to relevance; restricted never searched). `system` and `project` filter either mode by a case-insensitive substring of the `system` field or a `related_projects` entry; a filtered search skips the Quick Index hints. |
| `read_doc` | One doc or one section, by `doc_id` or alias, with sensitivity enforced. |
| `set_frontmatter` | Create or update frontmatter, validated against the vault's profile. |
| `write_doc` | Create a registered doc, or edit one section of its body, validated against the vault's profile. |
| `move_doc` | Move or rename a registered doc, keeping its frontmatter; a file name another note has is refused. |
| `log_entries` | An append-only log note's entries: list the open ones, or close one with its resolution. |
| `queue` | A project's work queue: add, list, set or claim, and close one-line items in its `Queue.md`. |

Broad questions start at the Quick Index; specific ones use `find`, then
`read_doc`; answers come from the doc. See [`demo.md`](demo.md). The
procedure is the `vault` skill (`skills/vault/SKILL.md`), which the repo ships
as a Claude Code plugin with `.claude-plugin/plugin.json` and `.mcp.json`; the
server's instructions only point to it.

## Sensitivity

`public`, `internal` and `sensitive` bodies are released; `restricted` is
withheld unless a local approval succeeds. The table and the unlock flow are in
[`ai-safety-security.md`](ai-safety-security.md#sensitivity-levels).

## Write model

- No tool takes an arbitrary path plus arbitrary text.
- Paths are confined to the named vault's root: file operations go through
  an `os.Root` opened on it (`internal/fsx`).
- Frontmatter writes validate against the vault's profile, keep `doc_id`
  immutable, and replace atomically; a failed write leaves the original intact.
- Provider tools are the provider's to govern; the host forwards calls and
  never widens them.

If the server can't name the file shape, validate the fields, and report
exactly what changed, it doesn't expose the mutation as a tool.

## Verification

See [`testing-ci.md`](testing-ci.md).
