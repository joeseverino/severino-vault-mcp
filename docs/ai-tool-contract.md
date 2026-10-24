# AI Tool Contract

The short operating contract for AI clients. More directive than the human
docs: the goal is correct tool selection with small responses. Architecture:
[`architecture.md`](architecture.md).

## Core rule

Use tools before prose. Don't answer an operational question from model memory
when a vault doc exists. Every shared tool takes `vault` (`labs` by default,
`edu` and each provider's vault when configured).

## Routing

| User intent | First tool/resource | Then |
|---|---|---|
| Broad process question | `vault://labs/quick-index` | Read the target `vault://labs/doc/{doc_id}`. |
| Specific runbook question | `find` | `read_doc` on the top hit with `sections=[<the hit's section>]`; the whole body when the section isn't enough. Quote commands exactly. |
| Known doc ID | `read_doc` or `vault://{vault}/doc/{doc_id}` | Respect the sensitivity policy. |
| System context | `find(query, system="...")` | Read the selected doc before summarizing. |
| Docs for a project | `find(query, project="...")` | Read before summarizing. |
| Body search | `find(by="text")` | Snippets are for discovery; read the doc before final instructions. |
| New doc, or a section edit | `write_doc` | Body plus frontmatter in one validated call; never edit the file directly. |
| Missing or stale metadata | `set_frontmatter` | Report changed fields and the next sync step. |
| Tasks | `task_board`, then `task_write` | `task_write(action=...)`: add, status, promote, delete; `items=[...]` for several changes in one call. |
| Open entries in a log note | `log_entries(doc_id, contains=...)` | `log_entries(action="close", match=..., resolution=...)` closes one; never read or edit the log whole. |
| Work to track: next, delegated, a decision, a follow-up | `queue(action="add", cwd=..., kind=..., text=...)` | `queue(action="list", cwd=...)` to see it; `set`, `close`, `promote` by `id` (or `agent`). Never keep it in scratch files. |
| Duplicate doc ID response | `doctor` (CLI) | Report every conflicting path; don't pick one. |

## Failures

Every failure is `{"ok": false, "error": "..."}` with `next_steps`: short
strings, each starting with a call to a registered tool, such as
`find(query="...")` for an unknown doc_id or
`read_doc(doc_id="...", fields=["outline"])` for a heading that isn't there.
`read_doc`'s not-found and withheld answers, and a `find` with no hits, carry
them too. An argument the schema refuses (an unknown name, a wrong type, a
value outside its enum, a missing required one) fails the same way, flagged
as a tool error. Take a next step before guessing.

## Sensitivity

| Sensitivity | AI behavior |
|---|---|
| `public` / `internal` | Use the body normally. |
| `sensitive` | Use the body and mention the advisory. |
| `restricted` | Don't ask for the body unless the user explicitly needs it. `read_doc(..., include_restricted=True)` only for one specific doc. |

`find(by="text")` never searches restricted bodies.

## Shell / CLI

The same retrieval as console subcommands, emitting the same `{ok, ...}` JSON:

| Need | Command |
|---|---|
| The command surface | `severino-vault-mcp describe` |
| Ranked section menu | `severino-vault-mcp find <query>` |
| Some sections or a whole body | `severino-vault-mcp read <doc_id> [--sections <slug> ...] [--fields outline]` |
| Open log entries, or close one | `severino-vault-mcp log-entries list <doc_id> [--contains <text>]`, `log-entries close <doc_id> --match <text> --resolution <text>` |
| A project's work queue | `severino-vault-mcp queue <list\|add\|set\|close\|promote> [id] --cwd <dir> ...` (see [Work queue](#work-queue)) |
| The edu dataset | `severino-vault-mcp export education` |

`describe` is generated from the CLI's command table and emits a
[Cordon v4](https://github.com/joeseverino/cordon) contract; prefer it over
restating commands from this doc.

## Work queue

Each project keeps one queue note, `01 Projects/<project>/Queue.md`
(`doc_id: queue-<project slug>`); work no project owns goes to
`07 Backlog/Queue.md` (`queue-backlog`). The first `add` creates the note
through `write_doc`'s create path. Items live under `## Open`; a closed or
dropped item moves under `## Closed`. An agent writes the note only through
`queue`; a person may also edit it in Obsidian (see the line grammar).

### Projects, repos and ids

A project's `index.md` frontmatter may carry two keys the tool reads:

```yaml
queue_prefix: ex                          # ids are ex-1, ex-2, ...; default: the folder name as a slug
repos: [example-tracker, example-tracker-ext]   # repository directory names
```

Set both through the server, never by editing the frontmatter by hand:
`set_frontmatter` on the project's `index.md` takes `repos` (replace),
`add_repos`, `remove_repos` and `queue_prefix`; the CLI is
`update-frontmatter <path> --set-repos <dir>... --queue-prefix <prefix>`.
The labs profile declares both (see "Frontmatter fields" below): repos are
bare directory names (no `/`); a prefix is lowercase letters and digits,
starting with a letter, at most 12 characters, and `backlog` is reserved.
Each value belongs to one project index; a clash is refused naming the doc
that holds it. Both keys are refused on any doc outside
`01 Projects/*/index.md`. Two projects whose folder names slug to the same
default prefix are reported by the queue itself.

### Frontmatter fields

Each vault's profile (`internal/schema`) declares the frontmatter fields its
docs carry: name, kind (string, enum, date, list, path, integer, boolean),
and any rule (enum values, doc_id prefixes, a pattern, reserved values, a
path scope such as `01 Projects/*/index.md`, uniqueness across docs,
immutability). That declaration is the only list: `set_frontmatter` and
`write_doc` take one argument per declared field (a list field as
`<field>`, `add_<field>`, `remove_<field>`; `touch_last_reviewed` sets that
date to today), the CLI's `update-frontmatter` and `write-doc` take one flag
per field (`--set-<field>` replaces a list), and `validate` checks every
declared field present in a block. Declaring a field in the profile is the
whole change that makes it settable; a field the chosen vault does not
declare is refused. `schema --contract` carries the declarations under
`fields`.

A call names its queue by `project` (the folder name, or `backlog`), `repo`,
or `cwd`. A `cwd` is tried folder by folder from itself up; a folder whose
`.git` file points into `<repo>/.git/worktrees/` also tries `<repo>`, so a
worker's worktree maps to the repository it came from. A `repo` or `cwd` no
project lists falls back to the backlog queue, and the result says so in
`fallback`.

Ids are `<prefix>-<n>`, allocated under the note's lock as one more than the
largest n in the note. The caller never supplies one on `add`; `set`, `close`
and `promote` find the queue from the id's prefix, or take `agent` instead of
`id` to name the one open item carrying that agent id.

### Line grammar

Each item is an Obsidian task whose fields are
[Dataview inline fields](https://blacksmithgu.github.io/obsidian-dataview/annotation/add-metadata/),
so Obsidian, the [Tasks plugin](https://publish.obsidian.md/tasks/) (set its
task format to Dataview) and Dataview render and query the note as it is:

```
- [<mark>] <text>  [kind:: <kind>]  [owner:: <owner>]  [agent:: <agent>]  [pr:: ...]  [branch:: ...]  [commit:: ...]  [slug:: ...]  [body:: <doc_id>]  [alias:: <old id>]  [resolution:: ...]  [id:: <id>]  [dependsOn:: <id>,<id>]
```

- `<mark>` is the status, as the Tasks plugin types it:

  | Status | Mark | Tasks type |
  |---|---|---|
  | `open` | `[ ]` | TODO |
  | `returned` (delegated only) | `[/]` | IN_PROGRESS |
  | `parked` | `[>]` | TODO by default; give `>` the ON_HOLD type in Tasks' settings to match |
  | `closed` | `[x]` | DONE |
  | `dropped` | `[-]` | CANCELLED |

  A closed or dropped item sits under `## Closed`; `close` takes
  `status=dropped` for work not done.
- `<kind>`: `next`, `delegated`, `decision`, `follow-up`.
- `<text>`: one line. A backslash escapes `\`, `[`, `]`, `(`, `)` and the
  Tasks date emoji (✅ ❌ 📅 ⏳ 🛫 ➕) in it, Markdown's own escape, so
  Obsidian shows it as typed and no text reads as a field.
- Fields appear only when set, each after two spaces (Tasks' own separator),
  in the order above. `owner`, `agent`, the links (`commit` comma-separated),
  `body` and `alias` are one word without spaces, brackets, parentheses or a
  backslash. `resolution` is one line, such as `repo@sha`, a PR or a doc_id,
  with the text's bracket escapes; a closed item closed through `queue`
  carries one, and an open item never does.
- `id` and `dependsOn` are the Tasks plugin's own dependency fields (the
  tool's `blocked_by`), last because Tasks reads fields from the end of the
  line and stops at the first it does not know. Tasks' `is blocked` and
  `is blocking` filters and `ready` agree: a dependency stops blocking once
  it is closed or dropped.

```
- [/] Build the store  [kind:: delegated]  [owner:: worker]  [agent:: a1b2]  [branch:: pass/store]  [commit:: abc1234,def5678]  [id:: ex-4]
- [ ] Read map\[string\] keys  [kind:: next]  [id:: ex-5]  [dependsOn:: ex-4]
- [x] Ship on Friday?  [kind:: decision]  [owner:: main]  [resolution:: yes]  [id:: ex-2]
```

Editing the note in Obsidian is fine; every write re-reads it under the
lock. A box ticked there is closed: the Tasks plugin's `[completion:: date]`
or `✅ date`, Dataview's completion field, and any other field the queue does
not own are kept as written (Tasks' own fields after `dependsOn`, the rest
before `id`). A ticked item has no resolution until `close` records one,
which also files it under `## Closed`; reopening it with `set` drops the
resolution and the done and cancelled dates. A line under either heading
that starts `- [` and doesn't parse is left alone and counted in `list`'s
`unparsed_lines`.

### CLI for hooks

`severino-vault-mcp queue <action> [id] [flags]` runs the same function as
the MCP tool against the labs vault and prints one line of compact JSON
(`--pretty` indents it). It exits 0 when `ok` is true, 1 on a refused call
(the JSON carries `error` and `next_steps`), 2 on a usage error (stderr).
Pass a value that starts with a dash as `--flag=value`. Items in the output
carry `id, project, kind, status, owner, text, links`, plus `blocked_by,
body, alias, agent, resolution` when set; `--fields` narrows them.

| Action | Flags |
|---|---|
| `add` | `--cwd`, `--repo` or `--project`; `--kind`, `--text`; optional `--owner`, `--agent`, `--pr`, `--branch`, `--commit`, `--slug`, `--blocked-by <id> ...` |
| `list` | `--cwd`, `--repo`, `--project` or `--all` (`--agent` alone searches every queue); `--kind`, `--status <s,s>` or `all` (default `open,returned`), `--owner`, `--ready`, `--contains`, `--limit` |
| `set` | `<id>` or `--agent`; `--status open\|returned\|parked`, `--owner` (with `--claim`: only if unowned), `--text`, link flags (`''` removes one), `--blocked-by` |
| `close` | `<id>` or `--agent`; `--resolution`; `--status dropped` for work not done; link flags |
| `promote` | `<id>` or `--agent`; optional `--content` |

## Writing a note from outside the server

The server rewrites a note by reading it, editing, and renaming a new file
over it. A process that writes the same note directly, such as an
append-only hook adding a line, takes the note's lock for the duration of its
append, so the server's rewrite never drops that line:

- Lock file: `<vault root>/.locks/<path of the note relative to the vault
  root, clean (no `./`, `..` or doubled `/`), every "/" replaced by
  "%">.lock`. `Projects/Example/queue.md` locks
  `.locks/Projects%Example%queue.md.lock`. Create `.locks/` and the file if
  absent; never delete them.
- Lock: an exclusive `flock(2)` on that file, held from before the note is
  opened until after it is closed. Never lock the note itself: a rewrite
  replaces its inode, so a lock on the old one guards nothing.
- Write with `O_APPEND`, and keep the hold short: the server waits up to ten
  seconds for the lock, then fails the write with a retry as its next step.

```go
lock, _ := os.OpenFile(filepath.Join(vault, ".locks", strings.ReplaceAll(rel, "/", "%")+".lock"), os.O_RDWR|os.O_CREATE, 0o644)
defer lock.Close() // closing releases the lock
syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
note, _ := os.OpenFile(filepath.Join(vault, rel), os.O_WRONLY|os.O_APPEND, 0)
note.WriteString(line); note.Close()
```

On Linux, `flock <lock file> sh -c 'printf ... >> <note>'` does the same
from a shell. A writer that skips the lock is still caught when its append
lands before the server's final re-read: the server re-applies its edit over
the new bytes. Only the lock closes the last window.

## Response discipline

- A short runbook gets a short answer.
- Quote commands exactly.
- If no matching doc exists, say so before offering general guidance.
- For writes, report only the changed fields and any follow-up.
