# Architecture

`severino-vault-mcp` is a local stdio MCP server for turning an
Obsidian-style operations vault into structured AI context. It is designed for
operators who already keep procedures, infrastructure notes, and project
records in markdown and want an assistant to use those docs before answering.

The project is deliberately small: no HTTP listener, no hosted control plane,
no database requirement for the reusable vault surface, and no arbitrary shell
bridge. The MCP host starts the server as a local process, the server indexes
configured vault folders, and tools/resources return structured slices of that
vault.

## Design Goals

- Ground operational answers in real local documentation.
- Keep private vault content on the operator's machine.
- Make the reusable surface work for any similarly structured vault.
- Keep sensitive and restricted material out of chat by default.
- Expose writes only where the file shape is known and validation is possible.
- Make operator-specific workflows possible without turning the server into a
  generic automation agent.

## Runtime Shape

```text
MCP client / host
  starts local stdio process
      |
      v
severino-vault-mcp
  reads config.toml + SVMC_* overrides
  indexes configured vault folders
  registers FastMCP resources and tools
      |
      v
local markdown vault and optional fixed integrations
```

The server runs under the local user account. It can read files that account
can read, but only files under the configured vault root and indexed folders
are part of the generic vault index. There is no inbound network surface.

### Module ownership

- `server.py` registers MCP resources and tools. Tool bodies are thin: they
  delegate to the service modules below rather than holding logic.
- `frontmatter.py` owns the single constrained-YAML toolkit — parsing
  (`split_frontmatter`) and serialization (`serialize_frontmatter`,
  `yaml_escape`). Both the generic vault writers and the writeup
  line-replacement path quote scalars through this one `yaml_escape`, so
  escaping rules cannot fork between tools.
- `atomic_write.py` owns durable file replacement. `atomic_write_text` (one
  file) and `transactional_replace` (many files, locked, rollback) share one
  staged-tempfile + `fsync` + `os.replace` primitive, so there is a single
  implementation of "replace a file without truncating it on failure."
- `paths.py` owns vault path validation. `validate_indexed_path` (writes must
  land on a file under an indexed dir) and `path_within_root` (operator tools
  must stay inside the vault root) are defined once and shared.
- `vault.py` owns indexing, alias resolution, and duplicate-ID exclusion. At
  index time it attaches `sections` to every `Doc`.
- `sections.py` owns section chunking (P1 of `docs/federated-retrieval.md`):
  `parse_sections` splits a body into addressable H2 spans (H3+ folded in,
  over-cap sections sub-split at H3 then hard-wrapped) with doc-unique heading
  slugs; `resolve_section` and `section_summary` back the section-scoped reads.
  It returns structured spans only — `search.py` scores them, `read_doc`
  serializes one for the MCP, and the CLI can render the same spans for a human.
- `vault_write_service.py` owns generic frontmatter mutation
  (`add_frontmatter`, `update_frontmatter`) plus the index-skipping fast
  paths used by the drift guards: `touch_reviewed` and `update_mirror_block`
  (section-scoped ```json mirror replacement, CLI-only — never an MCP tool,
  so AI sessions can't write arbitrary JSON into doc bodies).
- `vault_query_service.py` owns the two shell-backed read tools
  (`recent_changes` over `git log`, `search_body` over ripgrep) and the
  shared `doc_to_hit` projection every search response uses.
- `writeup_service.py` owns writeup reads, validation, and transactions.
- `site_ops_service.py` owns the jseverino.com operator integrations —
  Cloudflare D1 readers, the confirmed schema apply, and the live
  security-header check — behind a `SiteOpsRuntime` config holder.
- `hq_manifest.py` owns HQ manifest synthesis using the shared frontmatter
  parser, so HQ and the MCP can never drift on how a doc is read.

Every service module is FastMCP-free. Standalone CLI commands call them
directly and never import FastMCP registration just to perform file, manifest,
or D1 work. All of them report failures with one envelope:
`{"ok": false, "error": "<message>"}` — the shape the `site` CLI and
`site manage` already parse.

## Data Contract

The reusable vault surface expects markdown files with YAML frontmatter under
operator-selected folders. The default folders are:

```text
01 Projects/
02 Infrastructure/
03 Runbooks/
```

A minimal indexed document looks like this:

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

The important fields are:

| Field | Purpose |
|---|---|
| `doc_id` | Stable identifier used by `read_doc`, `vault://doc/{doc_id}`, aliases, related refs, and assistant instructions. |
| `title` | Human-readable label returned in search and read responses. |
| `doc_type` | Classifies docs such as runbooks, infrastructure notes, project records, and decision records. |
| `system` | Names the system or service the doc operates. |
| `environment` | Groups docs by operational context. |
| `status` | Keeps stale or deprecated docs visible as state, not tribal knowledge. |
| `sensitivity` | Controls body release behavior. |
| `tags` | Supports discovery and filtering. |

### Reference-shape slim frontmatter

Concept-level reference docs (under `04 Reference/` in the operator's vault) can use a slim three-field frontmatter shape:

```yaml
---
type: reference
tags: [topic, area]
created: YYYY-MM-DD
---
```

When the loader encounters a doc with `type: reference` and no `doc_id`, it synthesizes one from the file path (`ref-<kebab-case-stem>`), defaults `doc_type: reference` and `sensitivity: public`, and indexes the doc. This keeps primers and explainers searchable via `search_body` and `find_runbook` without forcing them to adopt the heavyweight HQ shape.

`doctor` validates this contract and can propose starter frontmatter for messy
vaults:

```bash
SVMC_VAULT_PATH=/absolute/path/to/vault severino-vault-mcp doctor --propose
```

For first-time adoption, start with
[`QUICKSTART.md`](../QUICKSTART.md), then use
[`docs/migration-guide.md`](migration-guide.md) to migrate a real vault in
small slices.

`doc_id` is a uniqueness boundary, not a last-write-wins key. If the index
finds the same ID in multiple files, it excludes every conflicting document
from runtime lookup and search. Direct reads return an explicit ambiguous
response with all paths, and `doctor` reports the files to fix.

## Generic MCP Surface

The generic surface is the part intended for other operators to run as-is:

| Surface | Purpose |
|---|---|
| `vault://quick-index` | Returns the navigation hub with `doc_id: report-playbook-mcp-index`. |
| `vault://doc/{doc_id}` | Returns one document body when the sensitivity policy allows it. |
| `find_runbook` | Ranks runbooks for a natural-language operational question. |
| `get_runbook` | Combines runbook search and selected body return for smaller local models. |
| `lookup_system` | Finds infrastructure/system notes by name. |
| `read_doc` | Reads one doc by `doc_id` or local alias with sensitivity enforcement. |
| `inventory_for_project` | Returns docs related to a project slug. |
| `recent_changes` | Summarizes recent vault commits inside indexed folders. |
| `daily_progress` | Reads `00 Inbox/Daily Note/YYYY-MM-DD.md` for progress/log questions. |
| `search_body` | Searches non-restricted bodies with frontmatter skipped. |
| `add_frontmatter` | Adds validated frontmatter to one vault markdown file. |
| `update_frontmatter` | Updates validated frontmatter fields on one indexed doc. |

The intended assistant behavior is also part of the architecture: broad
questions should start at the Quick Index, specific runbook questions should
use `find_runbook` or `get_runbook`, and exact operational answers should come
from the target document rather than model memory. See
[`docs/demo.md`](demo.md) for a reproducible transcript.

Daily notes are not part of the durable runbook index. They are a separate
capture/log surface under configurable `daily_notes_dir` (default
`00 Inbox/Daily Note`) and are exposed through `daily_progress` for questions
like "what progress did I make on Friday?".

## Sensitivity Model

| Sensitivity | Body behavior |
|---|---|
| `public` | Body is released. |
| `internal` | Body is released. |
| `sensitive` | Body is released with advisory text. |
| `restricted` | Body is withheld by default. `read_doc(..., include_restricted=True)` can request one local unlock. |

`search_body` always excludes restricted bodies, even when compatibility flags
are provided. Restricted body release is per-document and only through
`read_doc` after local policy allows it.

The full release policy, local unlock flow, audit-log behavior, and write
boundaries are documented in
[`docs/ai-safety-security.md`](ai-safety-security.md).

## Write Model

The write model is intentionally schema-specific:

- No tool accepts an arbitrary file path and arbitrary replacement text.
- Vault writes validate paths against the configured vault root through the
  shared `paths.py` helpers — one implementation, not one per writer.
- Generic frontmatter writes validate enum fields and keep `doc_id` immutable.
- Generic frontmatter writes stage a sibling file, flush it, and replace the
  target atomically; failed replacement leaves the original unchanged.
- Writeup writes know the exact `05 Writeups/<slug>/index.md` shape and mutate
  only named scalar fields.
- Featured-list reordering is centralized in `reorder_featured` so assistants
  do not hand-edit slot values across multiple files.
- Multi-writeup changes are planned in memory, staged to sibling temporary
  files, checked for concurrent modification, and replaced under a lock.
  Replacement failures trigger rollback of files already changed.

This is the core pattern for safe MCP writes: if the server cannot name the
file shape, validate the fields, and report exactly what changed, it should not
expose that mutation as a tool.

## Operator Extension Surface

The jseverino.com tools demonstrate a second, operator-specific surface:

| Surface | Purpose |
|---|---|
| Contact and CSP D1 readers | Query fixed Cloudflare D1 tables through Wrangler. |
| `apply_jseverino_d1_schema` | Apply one known schema file to one configured database after `confirm=True`. |
| Security-header checker | Run a focused HEAD check against the configured site origin. |
| Writeup readers | Inspect portfolio writeups, published state, featured order, technology slugs, and tag usage. |
| Writeup validators | Reuse one writeup/catalog/vault snapshot for single, batch, and dashboard validation. |
| Writeup dashboard | Return summaries, featured order, and validation in one low-latency response for interactive clients. |
| Writeup writers | Update scalar frontmatter or apply a complete multi-writeup plan without manual YAML edits. |

These tools are not a generic shell bridge. They are narrow wrappers around
known local paths, known file schemas, and known service bindings. The D1 and
header tools live in `site_ops_service.py` behind a `SiteOpsRuntime`, mirroring
the writeup and vault-write services. That makes them useful portfolio
evidence: the project shows both a reusable MCP product surface and a concrete
production workflow built with the same safety rules.

Standalone console commands import these services directly rather than
importing `server.py`. FastMCP registration remains isolated to the MCP server
path, reducing startup cost for short-lived shell and TUI calls.

## Running It Yourself

To run the reusable surface:

1. Clone the repo and install dependencies with `uv sync --extra dev`.
2. Run `scripts/check.sh` or `uv run pytest`.
3. Point `SVMC_VAULT_PATH` at `examples/sample-vault` and connect an MCP
   client.
4. Copy `config.example.toml` to
   `~/.config/severino-vault-mcp/config.toml`.
5. Set your real vault path and indexed folders.
6. Run `doctor --propose` and add frontmatter until the indexed docs validate.
7. Add a Quick Index with `doc_id: report-playbook-mcp-index`.

To adapt the extension pattern for another operator workflow:

1. Start with read tools that return structured state from known paths.
2. Add validation tests with fake fixtures.
3. Add write tools only for narrow, well-known schemas.
4. Reject path traversal and ambiguous targets.
5. Document every mutation boundary in `docs/ai-safety-security.md`.
6. Keep operator-specific paths configurable through `SVMC_*` variables.

## Verification

The current suite has 80 tests. It covers vault indexing, config overrides,
doctor validation, runbook ranking, body release policy, restricted local
unlock behavior, audit logging, resources/resource templates, body search,
duplicate-ID runtime exclusion, shared multiline frontmatter parsing, atomic
write failure behavior, HQ manifest generation, sample-vault reproducibility,
writeup loading, taxonomy parsing, configured-path boundary checks, Quick Index
recommendation alignment, publish-readiness validation, one-snapshot dashboard
behavior, composite publish prep, transactional writeup plans, rollback, and
frontmatter/featured-order mutations.

See [`docs/testing-ci.md`](testing-ci.md) for local commands, CI behavior, and
coverage notes.
