---
name: vault
description: How to use the severino-vault-mcp tools. Load it for any question the Obsidian vaults may answer (how do I, runbooks, infrastructure, projects, coursework, tasks) before answering from memory, and before any write to a vault doc or task.
---

# Using the vault

The server serves every vault: labs (homelab infrastructure, runbooks,
projects), edu (coursework and certifications), and any vault a provider
adds. Every shared tool takes `vault` (default labs).

## Answering

1. Operational questions ("how do I X", "what's the runbook for Y"): call
   `find` before writing any prose, then `read_doc` on the best hit, and
   answer in the doc's own words at the doc's length. Quote commands
   verbatim. Never substitute a generic tutorial for an existing doc.
2. No relevant hit: say so, then offer to write the doc, and create it with
   `write_doc` (body plus frontmatter fields in one validated call).
3. Broad questions: start from the `vault://labs/quick-index` resource (or
   `read_doc` on the Quick Index doc), then read the target doc.
4. `find(by=...)`: relevance (default) or text (body search: all words, any
   order). `system` and `project` narrow either mode to docs whose `system`
   field, or one of whose `related_projects` entries, contains the value.

## Sensitivity

public, internal and sensitive bodies come back from `read_doc` (sensitive
adds an `advisory` to pass along). restricted bodies are withheld unless the
user explicitly needs one; pass `include_restricted=True`, which also needs the
user's Touch ID approval through 1Password, from the main session only.

## Writes

- `set_frontmatter` creates or updates a doc's frontmatter against that
  vault's schema.
- `write_doc` creates a registered doc, or replaces or appends to one section
  of its body. Body edits to a registered doc go through it, never the file
  directly.
- `task_board` shows open work and the projects a task can go in;
  `task_write` adds, moves, promotes or deletes tasks.

After a write, remind the user to run their vault sync.

## Provider vaults

A provider vault's own tools are listed with the server's instructions; use
them as those instructions say.
