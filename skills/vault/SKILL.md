---
name: vault
description: How to use the severino-vault-mcp tools. Load it for any question the Obsidian vaults may answer (how do I, runbooks, infrastructure, projects, coursework, tracked work) before answering from memory, before any write to a vault doc or task, and before tracking next, delegated, decision or follow-up work in a project's queue.
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
   Read the hit's section first (`read_doc(doc_id, sections=[section])`).
   A doc over 20,000 characters read with no sections answers with its lead
   and outline (each section's size) and `truncated: true`; pick sections
   from it by slug, heading path, heading text, unique slug prefix or 1-based
   position, or ask for `fields=["body"]` when you need all of it. Read a doc
   once per task and keep what you need from it.
2. No relevant hit: say so, then offer to write the doc, and create it with
   `write_doc` (body plus frontmatter fields in one validated call).
3. Broad questions: start from the `vault://labs/quick-index` resource (or
   `read_doc` on the Quick Index doc), then read the target doc.
4. `find(by=...)`: relevance (default) or text (body search: docs holding
   any of the words, ranked by relevance; a hit lacking some names them in
   `missing_words`). A query that is a doc_id, title or alias puts that doc
   first (`exact_match`); with a doc_id in hand, `read_doc` it directly.
   `system`, `project` and `doc_type` are filters of their own, never `by`
   values: `system` and `project` keep docs whose `system` field, or one of
   whose `related_projects` entries, contains the value. A value no doc
   carries comes back with the closest real ones as `next_steps`.
   Queue notes (`doc_type: queue`) stay out of `find` unless you pass
   `doc_type="queue"`; `queue(action="list")` reads their items.
5. Outputs are lean by default. `fields=[...]` (dotted paths, e.g. `tags`,
   `snippets.text`) asks `find`, `read_doc` or `queue` for more; an
   unknown name is refused with the valid ones and the one it likely meant.
   A `find` result stays within 12,000 characters: past that, snippets and
   then the lowest hits are cut, and `truncated` says so.

## Sensitivity

public, internal and sensitive bodies come back from `read_doc` (sensitive
adds an `advisory` to pass along). restricted bodies are withheld unless the
user explicitly needs one; pass `include_restricted=True`, which also needs the
user's Touch ID approval through 1Password, from the main session only.

## When a call fails

Every `ok: false` result is an MCP tool error (`isError`) with `error` and
`next_steps`: the calls that recover from it, each naming a tool. A `find` with no hits carries them too.
Take one of them before guessing.
An upgraded server binary takes over a running session at its next call. A
`warning` that the new binary could not take over means answers still come
from the old one until the session restarts.

## Writes

- `set_frontmatter` creates or updates a doc's frontmatter against that
  vault's schema. It takes every field the vault's profile declares (they are
  its arguments), so no frontmatter is ever edited by hand. On a
  project's `index.md` that includes `project_path` and the work queue keys
  `repos` / `add_repos` / `remove_repos` and `queue_prefix`. `unset` takes
  field names to remove from the block (a required field is refused).
- `write_doc` creates a registered doc, or replaces, appends to or deletes one section
  of its body. `heading` takes a `heading_path` or section slug from
  `read_doc`'s outline, or a unique heading text; `""` is the text before the
  first heading. A heading names its whole block, subsections included, the
  same span `read_doc` returns for it, so read it first; the body leaves out
  the heading line and keeps every subsection heading (`replace_section`
  refuses one that drops any), or targets the subsection itself.
  `append_section` adds at the end of the block; `delete_section` removes
  the block, subsections included, and takes no body.
  `replace_text` changes one sentence or bullet: `old_text` copied exactly
  from `read_doc` must occur once in the body (or in `heading`'s section when
  given), and becomes `new_text`; `""` removes it, and including the line
  break in `old_text` removes the whole line. A count other than one is
  refused with the count; lengthen `old_text` or add `heading`.
  Body edits to a registered doc go through it, never an Edit or Write on
  the file, however small the change.
- `move_doc` moves or renames a registered doc (`to` is a .md path, or a
  folder ending in `/`); its doc_id and frontmatter stay. Never move a vault
  file with the shell. A file name another note already has is refused, since
  Obsidian links by unique name: rename first, in the same folder, to a name
  that says which doc it is (`Audit.md` becomes `Example Audit - 2026-09-07.md`).
  `with_folder=true` moves a report's folder with its evidence files. Path
  links to the doc are rewritten; `links` lists the notes changed.
- `move_folder(from, to)` moves or renames a whole folder (a project, a
  report series) with everything in it, and rewrites every path link into it
  across the vault (`[[01 Projects/x/index|…]]`, embeds, `[text](<path>)`);
  `links` lists each note it rewrote. Renaming `01 Projects/<old>` to
  `01 Projects/<new>` also renames the slug in every `related_projects` and
  retitles the queue note; doc_ids and queue item ids stay. Restricted notes
  are listed in `skipped_restricted` unless `include_restricted=true`.
- `99 Archive` is history: only archived docs go there, and
  `archive=true` on `move_doc` or `move_folder` sets `status: archived` in the
  same call. `find` leaves archived docs out unless `include_archived=true`;
  `read_doc` still reads them by doc_id. `set_frontmatter` and `write_doc`
  edit an archived doc like any other while it stays archived, and
  `set_frontmatter` registers a bare archive note with `doc_id`, `title`,
  `doc_type` and `status: archived`.
- A report (`doc_type: report`, with `kind`: audit, review, research, pass,
  session_log or evaluation) is a dated record of one run, written once. It
  lives in a `Reports/` folder, and only reports do: a plan, decision record
  or scorecard that keeps changing is a living note elsewhere. A newer report
  of a series supersedes the old one: on the old one, `set_frontmatter`
  `superseded_by` to the new doc's doc_id and `status: deprecated`.
- `log_entries` works an append-only log note, one bullet per entry:
  `action="list"` returns the open entries (`contains` narrows them, e.g. to
  a repo), and `action="close"` with `match` and `resolution` appends
  ` → <resolution>` to the one open entry `match` names; `action="annotate"`
  with `match` and `resolution` replaces a closed entry's resolution, and
  with `reopen=true` removes it. Use it instead of reading or editing a log
  note whole, and never Edit a log entry's line.

## Work queue

Track work that outlives the turn with `queue`, never in scratch files,
memory or end-of-turn prose: what comes next (`kind="next"`), what a worker
holds (`delegated`, with its `agent` id), what the user must answer
(`decision`), and what to come back to (`follow-up`). Each project has one
queue note; name it with `cwd` (your working directory), `repo` or `project`.

- `queue(action="add", cwd=..., kind=..., text=...)` returns the item's id;
  never invent one. Every item records its `created` date; a worker's item
  may carry `base` (the branch it lands on) and `owns` (its paths). A question you put to the user is an explicit
  `kind="decision"` add, closed with their answer as the resolution.
- `queue(action="list", cwd=...)` shows open and returned items;
  `ready=true` the unblocked next work, `kind="decision"` the open questions,
  `all=true` every project's queue, `links={"branch": ...}` the items on
  exactly that branch, in whichever queue holds them.
- `queue(action="set", id=..., claim=true, owner=...)` takes an item only if
  no other owner or agent holds it and it is not done (a done item is
  never reopened; add a new one); add `agent=...`,
  `kind="delegated"` and `links={"branch":...}` to take a queued item over
  for a worker in the same call (a decision owned by `main` or nobody counts
  too; one locked edit, so a lost claim changes
  nothing). An agent may hold several items. `release=true` hands one back
  to the queue as unowned next work; `status="returned"` marks a delegated
  item handed back; `links={...}` records a pr, branch, commit or slug. A
  `warning` in the result means another agent held the item you changed.
- `queue(action="close", id=..., resolution="repo@sha")` closes it
  (`status="dropped"` with the reason as the resolution for work not done).
- A repo no project maps is refused on `add`; make the `set_frontmatter`
  call its `next_steps` name (`add_repos`) and retry. Only work no project
  owns goes to `project="backlog"`.
- A worker sent to another repo is added with `cwd` its worktree and
  `origin` your repo; `list` with `cwd` and `include_origin=true` shows
  your queue plus every worker you sent, wherever it is filed.
- One worker spanning repos: add it in your queue with `repos=[...]`; close
  each landing with `partial=true, resolution="repo@sha"`, and it closes
  when every repo has landed. Several workers: one item each in its repo,
  and a parent in yours with `blocked_by` their ids.
- An item's detail lives in a doc: `add` or `set` with `body` its doc_id.

`log_entries` stays the tool for append-only log notes such as lessons.

After a write, remind the user to run their vault sync.

## Provider vaults

A provider vault's own tools are listed with the server's instructions; use
them as those instructions say.
