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

`doctor` validates this contract and can propose starter frontmatter for messy
vaults:

```bash
SVMC_VAULT_PATH=/absolute/path/to/vault severino-vault-mcp doctor --propose
```

For first-time adoption, start with
[`QUICKSTART.md`](../QUICKSTART.md), then use
[`docs/migration-guide.md`](migration-guide.md) to migrate a real vault in
small slices.

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
| `search_body` | Searches non-restricted bodies with frontmatter skipped. |
| `add_frontmatter` | Adds validated frontmatter to one vault markdown file. |
| `update_frontmatter` | Updates validated frontmatter fields on one indexed doc. |

The intended assistant behavior is also part of the architecture: broad
questions should start at the Quick Index, specific runbook questions should
use `find_runbook` or `get_runbook`, and exact operational answers should come
from the target document rather than model memory. See
[`docs/demo.md`](demo.md) for a reproducible transcript.

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
- Vault writes validate paths against the configured vault root.
- Generic frontmatter writes validate enum fields and keep `doc_id` immutable.
- Writeup writes know the exact `05 Writeups/<slug>/index.md` shape and mutate
  only named scalar fields.
- Featured-list reordering is centralized in `reorder_featured` so assistants
  do not hand-edit slot values across multiple files.

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
| Writeup validators | Check frontmatter completeness, image references, taxonomy coverage, and related-vault refs. |
| Writeup writers | Update scalar frontmatter and maintain featured ordering without manual YAML edits. |

These tools are not a generic shell bridge. They are narrow wrappers around
known local paths, known file schemas, and known service bindings. That makes
them useful portfolio evidence: the project shows both a reusable MCP product
surface and a concrete production workflow built with the same safety rules.

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

The current suite has 69 tests. It covers vault indexing, config overrides,
doctor validation, runbook ranking, body release policy, restricted local
unlock behavior, audit logging, resources/resource templates, body search,
frontmatter writes, sample-vault reproducibility, writeup loading, taxonomy
parsing, configured-path boundary checks, Quick Index recommendation alignment,
publish-readiness validation, composite publish prep, and writeup
frontmatter/featured-order mutations.

See [`docs/testing-ci.md`](testing-ci.md) for local commands, CI behavior, and
coverage notes.
