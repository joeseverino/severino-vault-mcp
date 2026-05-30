# Operator Workflows

This document shows the concrete systems behind the jseverino.com surface while
keeping the pattern portable. The goal is not to make every user's environment
look like this one. The goal is to show how a local MCP can expose real
operator workflows as narrow, testable tools.

For the reusable vault architecture, read [`architecture.md`](architecture.md).
For the exact safety boundary, read
[`ai-safety-security.md`](ai-safety-security.md).

## Workflow Pack Pattern

The jseverino.com tools are best understood as a workflow pack:

```text
generic vault MCP
  + fixed local paths
  + fixed service bindings
  + schema-aware readers
  + narrow mutation tools
  + tests with fake fixtures
```

That is the reusable pattern. The implementation here deliberately targets the
author's personal cybersecurity portfolio and operations vault because the
tools should be fastest where they are used every day. The same pattern works
for client publishing, lab inventory, incident-response notes, internal
runbooks, or any other local workflow with predictable files and state.

## Systems In Use

| System | Role in this MCP |
|---|---|
| Obsidian-style markdown vault | Private source of truth for runbooks, infrastructure notes, project records, pages, writeups, and technology taxonomy. |
| FastMCP / Model Context Protocol | Local stdio interface between AI clients and the vault tools. |
| Python 3.11+ | MCP server implementation. |
| `uv` | Reproducible local development, packaging, and tool install flow. |
| `ripgrep` | Fast structured body search with frontmatter skipped. |
| `fd` | Optional fast vault walking. |
| macOS Keychain | Recommended storage location for the salted restricted-doc unlock hash. |
| Claude Code / Claude Desktop / local MCP clients | Example MCP hosts for using the server. |
| Local models | Supported usage path for keeping private vault context on the operator's Mac. |
| Git | Recent-change reporting and normal vault/repo history review. |
| Cloudflare D1 | Fixed database for jseverino.com contact submissions and CSP reports. |
| Wrangler | Local CLI used for fixed D1 read/schema operations. |
| jseverino.com Astro site repo | Public site build source that consumes sanitized content. |
| Cloudflare Pages | Production hosting surface checked by the security-header helper. |

## Generic Vault Workflows

These tools are portable and should work for any similar vault:

| Workflow | Tools |
|---|---|
| Find an operational procedure | `vault://quick-index`, `find_runbook`, `get_runbook`, `read_doc` |
| Read one known doc | `vault://doc/{doc_id}`, `read_doc` |
| Find system context | `lookup_system`, `search_body` |
| List project-related docs | `inventory_for_project` |
| Review recent vault changes | `recent_changes` |
| Add or maintain metadata | `add_frontmatter`, `update_frontmatter`, `doctor --propose` |
| Keep restricted procedures out of chat | `sensitivity: restricted`, `read_doc(..., include_restricted=True)` with local unlock |

## jseverino.com Workflows

These tools are intentionally visible because they show the MCP operating
against a real production workflow.

| Workflow | Tools |
|---|---|
| Review contact submissions | `list_contact_submissions` |
| Review CSP reports | `list_csp_reports`, `count_csp_reports` |
| Apply the fixed D1 schema | `apply_jseverino_d1_schema(confirm=True)` |
| Check live security headers | `check_jseverino_security_headers` |
| Get the featured home-cloud order | `list_featured_writeup_order` |
| Inventory portfolio writeups | `list_writeups` |
| Parse technology taxonomy | `get_technology_catalog` |
| Check whether a tag is earned | `find_writeups_using_tag` |
| Validate one writeup before publish | `validate_writeup` |
| Get one publish-prep response | `prepare_writeup_publish` |
| Update one writeup's scalar frontmatter | `update_writeup_frontmatter` |
| Maintain featured ordering | `reorder_featured` |

## Why The Tools Are Narrow

The operator-specific tools could have been replaced by a generic shell tool,
but that would be worse for both security and model reliability. Narrow tools
are faster for AI clients because they return structured state directly:

- The model does not need to grep dozens of files.
- The model does not need to parse Markdown tables by hand.
- The model does not need to infer featured order from raw YAML.
- The model receives explicit blockers, nits, and changed fields.
- The mutation boundary is visible in tests and docs.

This is the same reason `prepare_writeup_publish` exists: one compact response
beats a multi-step chain when the model only needs a publish decision.

## Porting The Pattern

To build a workflow pack for another operator:

1. Name the workflow in terms a human uses, such as "publish a writeup" or
   "review CSP reports."
2. Identify the source of truth: markdown folder, catalog file, fixed database,
   or live endpoint.
3. Write read tools first and return compact structured objects.
4. Add write tools only when the file shape and valid fields are known.
5. Validate paths against the configured root.
6. Add fake-fixture tests for every reader and writer.
7. Document the workflow in this style, then add short AI-facing rules in
   [`ai-tool-contract.md`](ai-tool-contract.md).

## Configuration

The jseverino.com paths default to locations under the configured vault root
or to fixed local project paths:

```bash
SVMC_JSEVERINO_D1_DATABASE=jseverino-contact
SVMC_JSEVERINO_SITE_REPO=~/Documents/Code/Projects/jseverino.com
SVMC_JSEVERINO_SITE_ORIGIN=https://jseverino.com
SVMC_JSEVERINO_WRITEUPS_DIR=<vault>/05 Writeups
SVMC_JSEVERINO_TECH_GROUPS=<vault>/06 Pages/_technology-groups.md
```

The writeup directory and technology catalog must resolve inside the configured
vault root. This keeps portfolio frontmatter mutations inside the same local
trust boundary as the generic vault tools.
