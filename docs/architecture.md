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
severino-vault-mcp
  vaults.build(): one GovernanceContext per vault (labs, edu, life)
  register_core(): 8 shared tools, each with a `vault` argument
  each vault's own group (education, life) on its context
      |
      v
local markdown vaults (+ life's registered Apple lists and calendars)
```

The server runs under the local user account and reads only files under each
vault's root and indexed folders.

## Vaults

| Vault | When present | Config | Profile | Own tools |
|---|---|---|---|---|
| `labs` | always (default) | `SVMC_CONFIG`, else `~/.config/severino-vault-mcp/config.toml`, plus `SVMC_*` overrides | labs | none |
| `edu` | its config file exists | `SVMC_EDU_CONFIG`, else `~/.config/severino-edu-mcp/config.toml` | education | `education_dataset` |
| `life` | `severino_life` imports | `SVMC_LIFE_CONFIG`, else `~/.config/severino-life-mcp/config.toml` | life | `reminders`, `calendar`, `agenda`, `life_view`, `renew`, `life_ops` |

`vaults.build()` loads labs first with the process environment. edu loads from
its own TOML with an environment scrubbed of `SVMC_*`, so a labs override can't
redirect it. severino-life resolves its config from `SVMC_CONFIG` at call time,
so after labs is loaded `build()` points `SVMC_CONFIG` at life's config (or
clears it) for the rest of the process.

## Engine vs. host

The governance core is [`severino-vault-engine`](https://github.com/joeseverino/vault-engine)
(import `vault_engine`): indexing, alias resolution and duplicate-ID exclusion,
section chunking and ranking, the sensitivity gate and local unlock, schema
profiles (`LABS_PROFILE`, `EDUCATION_PROFILE`), frontmatter parsing and
serialization, atomic writes, path validation, the task ledger, daily notes,
`doctor`, and `register_core`. Change generic behavior there.

This repo owns composition and the CLI:

- `vaults.py`: the vault contexts and their domain registrars.
- `server.py`: composition root and the instructions block.
- `education.py`: the edu dataset, shared by the tool and `export education`.
- `labs/hq_manifest.py`: the HQ docs manifest, until HQ owns it.
- `cli.py` / `__main__.py`: subcommands over the labs vault. Every result goes
  through one `_emit` (compact or `--pretty`, `ok` to exit code), and
  `describe` projects the parser to a Cordon contract.

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
- Paths validate against the named vault's root through `vault_engine.paths`.
- Frontmatter writes validate against the vault's profile, keep `doc_id`
  immutable, and replace atomically; a failed write leaves the original intact.
- Life's Apple-store writes only reach registered lists and calendars and
  preview unless `dry_run=false`.

If the server can't name the file shape, validate the fields, and report
exactly what changed, it doesn't expose the mutation as a tool.

## Verification

See [`testing-ci.md`](testing-ci.md).
