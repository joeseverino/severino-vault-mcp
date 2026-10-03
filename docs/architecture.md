# Architecture

`severino-vault-mcp` is one local stdio MCP server for every Obsidian vault I
keep. It indexes each vault's configured folders, answers from the docs instead
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
  mcpserver.New(): 8 shared tools, each with a `vault` argument,
                   plus education_dataset and each provider's tools
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
| `edu` | its config file exists | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml` | education | `education_dataset` |
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
  its docs and tasks, and writes validate against the provider's profile;
- lists the provider's tools and re-exports them unchanged (a name that
  collides with a core tool is skipped and logged);
- appends the instructions from the provider's initialize result.

There are no compiled-in providers and no default commands. An entry missing
`command` or `config`, a provider that fails to start, and a vault name that
already exists are each logged and skipped; the rest of the server comes up.

## Code

The governance engine is `internal/`: indexing, alias resolution and
duplicate-ID exclusion, section chunking and ranking, the sensitivity gate and
local unlock, schema profiles, frontmatter parsing and serialization, atomic
writes, path validation, the task ledger, daily notes and `doctor`. On top:

- `internal/core`: the shared tools, transport-free.
- `internal/vaults`, `internal/provider`: composition.
- `internal/mcpserver`: the MCP adapter and the instructions block.
- `internal/education`: the edu dataset, shared by the tool and `export education`.
- `internal/hqmanifest`: the HQ docs manifest, until HQ owns it.
- `internal/cli`: subcommands over the labs vault. Every result goes through one
  emitter (compact or `--pretty`, `ok` to exit code), and `describe` projects
  the command table to a Cordon contract.

Site work (writeups, the technology catalog, D1, CSP, contact, headers) is
owned by the jseverino.com repo's `site` CLI, not this server.

## Data contract

Each vault's docs are markdown with YAML frontmatter under its indexed
folders (labs default: `01 Projects`, `02 Infrastructure`, `03 Runbooks`,
`07 Backlog`). Minimal labs doc:

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
| `system` | The system or service the doc covers (`find(by="system")`). |
| `environment`, `status`, `tags` | Context, lifecycle and discovery. |
| `sensitivity` | Body release behavior. |

Reference docs with `type: reference` and no `doc_id` get a synthesized
`ref-<stem>` ID, `doc_type: reference` and `sensitivity: public`.

`doc_id` is a uniqueness boundary: duplicates are excluded from lookup and
search, direct reads return every conflicting path, and `doctor` reports them.

## Tools

| Surface | Purpose |
|---|---|
| `vault://{vault}/quick-index` | The vault's navigation hub (`doc_id: report-playbook-mcp-index`). |
| `vault://{vault}/doc/{doc_id}` | One doc body, subject to sensitivity. |
| `find` | `by`: `relevance` (ranked sections plus Quick Index hints), `system`, `project`, `text` (ripgrep; restricted never searched). |
| `read_doc` | One doc or one section, by `doc_id` or alias, with sensitivity enforced. |
| `set_frontmatter` | Create or update frontmatter, validated against the vault's profile. |
| `update_link` | Replace one exact Markdown link. |
| `task_board`, `task_write` | The task ledger. |
| `recent_changes`, `daily_progress` | Recent commits; daily notes. |

Broad questions start at the Quick Index; specific ones use `find`, then
`read_doc`; answers come from the doc. See [`demo.md`](demo.md).

## Sensitivity

| Sensitivity | Body behavior |
|---|---|
| `public`, `internal` | Released. |
| `sensitive` | Released with an advisory. |
| `restricted` | Withheld. `read_doc(..., include_restricted=True)` can request one local unlock. |

Details: [`ai-safety-security.md`](ai-safety-security.md).

## Write model

- No tool takes an arbitrary path plus arbitrary text.
- Paths validate against the named vault's root (`internal/fsx`).
- Frontmatter writes validate against the vault's profile, keep `doc_id`
  immutable, and replace atomically; a failed write leaves the original intact.
- Provider tools are the provider's to govern; the host forwards calls and
  never widens them.

If the server can't name the file shape, validate the fields, and report
exactly what changed, it doesn't expose the mutation as a tool.

## Verification

See [`testing-ci.md`](testing-ci.md).
