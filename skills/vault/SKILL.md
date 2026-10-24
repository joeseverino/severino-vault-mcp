---
name: vault
description: How to use the severino-vault-mcp tools. Load it for any question the Obsidian vaults may answer (how do I, runbooks, infrastructure, projects, coursework, tasks) before answering from memory, before any write to a vault doc or task, and before tracking next, delegated, decision or follow-up work in a project's queue.
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
   Read the hit's section first (`read_doc(doc_id, sections=[section])`);
   for a long doc, `fields=["outline"]` lists its sections without text, and
   the whole body is the default when you need all of it.
2. No relevant hit: say so, then offer to write the doc, and create it with
   `write_doc` (body plus frontmatter fields in one validated call).
3. Broad questions: start from the `vault://labs/quick-index` resource (or
   `read_doc` on the Quick Index doc), then read the target doc.
4. `find(by=...)`: relevance (default) or text (body search: all words, any
   order). `system` and `project` narrow either mode to docs whose `system`
   field, or one of whose `related_projects` entries, contains the value.
5. Outputs are lean by default. `fields=[...]` (dotted paths, e.g. `tags`,
   `snippets.text`) asks `find`, `read_doc` or `task_board` for more; an
   unknown name is refused with the valid ones.

## Sensitivity

public, internal and sensitive bodies come back from `read_doc` (sensitive
adds an `advisory` to pass along). restricted bodies are withheld unless the
user explicitly needs one; pass `include_restricted=True`, which also needs the
user's Touch ID approval through 1Password, from the main session only.

## When a call fails

A failure is `{"ok": false, "error": ...}` with `next_steps`: the calls that
recover from it, each naming a tool. A `find` with no hits carries them too.
Take one of them before guessing.
After the server binary is upgraded, restart Claude Code sessions (`/mcp`
alone is not enough): `find` and `read_doc` load upfront and keep the schema
they started with.

## Writes

- `set_frontmatter` creates or updates a doc's frontmatter against that
  vault's schema. It takes every field the vault's profile declares (its
  description lists them), so no frontmatter is ever edited by hand. On a
  project's `index.md` that includes `project_path` and the work queue keys
  `repos` / `add_repos` / `remove_repos` and `queue_prefix`.
- `write_doc` creates a registered doc, or replaces or appends to one section
  of its body; `heading: ""` is the text before the first heading. Body edits to a registered doc go through it, never the file
  directly.
- `move_doc` moves or renames a registered doc (`to` is a .md path, or a
  folder ending in `/`); its doc_id and frontmatter stay. Never move a vault
  file with the shell. A file name another note already has is refused, since
  Obsidian links by unique name: rename first, in the same folder, to a name
  that says which doc it is (`Audit.md` becomes `Example Audit — 2026-09-07.md`).
  `with_folder=true` moves a report's folder with its evidence files. Fix the
  links the result's `wikilinks` lists with `write_doc`.
- A newer report of a series supersedes the old one: on the old one,
  `set_frontmatter` `superseded_by` to the new doc's doc_id and
  `status: deprecated`.
- `task_board` shows open work, best first (`limit`, `status`, `project`),
  and the projects a task can go in; `task_write` adds, moves, promotes or
  deletes tasks, and `items=[...]` makes several changes in one call, each
  reported on its own.
- `log_entries` works an append-only log note, one bullet per entry:
  `action="list"` returns the open entries (`contains` narrows them, e.g. to
  a repo), and `action="close"` with `match` and `resolution` appends
  ` → <resolution>` to the one open entry `match` names. Use it instead of
  reading or editing a log note whole.

## Work queue

Track work that outlives the turn with `queue`, never in scratch files,
memory or end-of-turn prose: what comes next (`kind="next"`), what a worker
holds (`delegated`, with its `agent` id), what the user must answer
(`decision`), and what to come back to (`follow-up`). Each project has one
queue note; name it with `cwd` (your working directory), `repo` or `project`.

- `queue(action="add", cwd=..., kind=..., text=...)` returns the item's id;
  never invent one. A question you put to the user is an explicit
  `kind="decision"` add, closed with their answer as the resolution.
- `queue(action="list", cwd=...)` shows open and returned items;
  `ready=true` the unblocked next work, `kind="decision"` the open questions,
  `all=true` every project's queue.
- `queue(action="set", id=..., claim=true, owner=...)` takes an item only if
  nobody holds it; `status="returned"` marks a delegated item handed back;
  `links={...}` records a pr, branch, commit or slug.
- `queue(action="close", id=..., resolution="repo@sha")` closes it
  (`status="dropped"` with the reason as the resolution for work not done);
  `action="promote"` gives an item that needs prose a body doc.

`log_entries` stays the tool for append-only log notes such as lessons.

After a write, remind the user to run their vault sync.

## Provider vaults

A provider vault's own tools are listed with the server's instructions; use
them as those instructions say.
