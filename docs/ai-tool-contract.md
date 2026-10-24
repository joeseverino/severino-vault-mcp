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
| Queue notes (`doc_type: queue`) | `find(query, doc_type="queue")` | `find` leaves them out otherwise; `queue` is the tool to work them. |
| Body search | `find(by="text")` | Snippets are for discovery; read the doc before final instructions. |
| New doc, or a section edit | `write_doc` | Body plus frontmatter in one validated call; never edit the file directly. |
| Missing or stale metadata | `set_frontmatter` | Report changed fields and the next sync step. |
| Move or rename a doc | `move_doc(doc_id, to)` | Path links follow; read `links` and `skipped_restricted`. Never move a vault file with the shell. |
| Move or rename a folder or project; retire one | `move_folder(from, to)` (`to` in `99 Archive/` with `archive=true` to retire) | Path links and `related_projects` follow; read `links`, `skipped_restricted` and `failed`. |
| Archived docs | `find(query, include_archived=true)` | `find` leaves `99 Archive` out otherwise. |
| A newer report of a series | `write_doc` for the new one | `set_frontmatter` on the old one: `superseded_by` (the new doc_id) and `status: deprecated`. |
| Tasks | `task_board` (`include_queue=true` adds the open queue items), then `task_write` | `task_write(action=...)`: add, status, promote, delete; `items=[...]` for several changes in one call. |
| Open entries in a log note | `log_entries(doc_id, contains=...)` | `log_entries(action="close", match=..., resolution=...)` closes one; never read or edit the log whole. |
| Work to track: next, delegated, a decision, a follow-up | `queue(action="add", cwd=..., kind=..., text=...)` | `queue(action="list", cwd=...)` to see it; `set`, `close` by `id` (or `agent`). Never keep it in scratch files. |
| Duplicate doc ID response | `doctor` (CLI) | Report every conflicting path; don't pick one. |

## Failures

Every `ok: false` result is an MCP tool error (`isError`) carrying
`{"ok": false, "error": "..."}` and `next_steps`: short
strings, each starting with a call to a registered tool, such as
`find(query="...")` for an unknown doc_id or
`read_doc(doc_id="...", fields=["outline"])` for a heading that isn't there.
`read_doc`'s not-found and withheld answers, and a `find` with no hits, carry
them too. An argument the schema refuses (an unknown name, a wrong type, a
value outside its enum, a missing required one) fails the same way. Take a
next step before guessing.

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
| A project's work queue | `severino-vault-mcp queue <list\|add\|set\|close\|reformat> [id] --cwd <dir> ...` (see [Work queue](#work-queue)) |
| Move or rename a doc | `severino-vault-mcp move-doc <to> --doc-id <id> [--with-folder]` |
| The frontmatter rules as JSON Schema | `severino-vault-mcp schema --json-schema [--vault labs\|edu]` (see [Frontmatter as JSON Schema](#frontmatter-as-json-schema)) |
| The edu dataset | `severino-vault-mcp export education` |

`describe` is generated from the CLI's command table and emits a
[Cordon v4](https://github.com/joeseverino/cordon) contract; prefer it over
restating commands from this doc.

## Work queue

Each project keeps one queue note, `01 Projects/<project>/Queue.md`
(`doc_id: queue-<project slug>`); work no project owns goes to
`07 Backlog/Queue.md` (`queue-backlog`). The first `add` creates the note
through `write_doc`'s create path, with only the fields the labs profile
requires of `doc_type: queue` (`doc_id`, `title`, `doc_type`, `system`,
`status`, `queue_format`, plus `last_reviewed`) and no H1: the file name and
breadcrumb name it. Items live under `## Open`; a closed or
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
The labs profile declares both (see [Frontmatter fields](#frontmatter-fields)): repos are
bare directory names (no `/`); a prefix is lowercase letters and digits,
starting with a letter, at most 12 characters, and `backlog` is reserved.
Each value belongs to one project index; a clash is refused naming the doc
that holds it. Both keys are refused on any doc outside
`01 Projects/*/index.md`. Two projects whose folder names slug to the same
default prefix are reported by the queue itself.

A call names its queue by `project` (the folder name, or `backlog`), `repo`,
or `cwd`. A `cwd` is tried folder by folder from itself up; a folder whose
`.git` file points into `<repo>/.git/worktrees/` also tries `<repo>`, so a
worker's worktree maps to the repository it came from.

A `repo` or `cwd` no project lists is never filed in the backlog: `add`
fails (`ok: false`, exit 1 from the CLI) and its `next_steps` name the call
that maps it, `set_frontmatter(relative_path="01 Projects/<project>/index.md",
add_repos=["<repo>"])`. The backlog queue takes work only through an
explicit `project="backlog"`. `list` from an unmapped repo reads the backlog
and says so: `fallback` is the backlog queue's path and `warning` the
sentence to show, with the mapping call.

Ids are `<prefix>-<n>`, allocated under the note's lock as one more than the
largest n written as an id field (`[id:: …]`, `(id:: …)` or `🆔 …`) anywhere
in any queue note, read from the text, so an id on a line that does not
parse, in another project's queue or in a restricted one is never issued
again. A write that would leave one id on two item lines of a note is
refused, and so is every write to a note that already has one, until a
person renumbers a line. The caller never supplies one on `add`; `set` and `close`
find the queue from the id's prefix, or take `agent` instead of
`id` to name the one open item carrying that agent id. An agent holds any
number of items: `list` with `agent` returns them all, and `set` or
`close` by `agent` alone, when it holds several, is refused with every id
it holds.

An item may carry an `alias`, an older id it also answers to (a task's
doc_id when an import moves the task into a queue), and a `body`, the
doc_id of the doc that holds its detail; `add` and `set` write both (`""`
removes one on `set`). A `body` is checked like a `superseded_by` doc
reference: it starts with a doc_id prefix and names a doc in the vault. An
alias that another item already carries, or that is another item's id, is
refused. Wherever an id names an item (`set` and `close`'s `id`, and each
`blocked_by` entry) its alias does too: the item with that id comes first,
then the one with that alias, and `blocked_by` stores the item's id. `list`
with `alias` returns the item that carries it, from every queue unless one
is named.

### Workers across repos

- A worker sent from one repo to work in another is filed in the queue of
  the repo it works in (`cwd` its worktree), with `origin` the repo the
  sending session ran in (a repository name, or a working directory
  resolved as `cwd` is). `list` with `origin` keeps only items from that
  repo; `list` with `include_origin=true` and a `cwd`, `repo` or `project`
  returns that queue plus the delegated items in any other queue whose
  `origin` is that repo (by `project`, any repo it lists), each with its
  `project`. Those items default to the fields `id, project, kind, status,
  owner, text, origin`.
- One worker whose work spans repos is one item, filed in the sending
  repo's queue, with `repos` listing them. Its resolution is `repo@sha` per
  repo, comma-separated. `close` with `partial=true` appends the landings
  it is given and leaves the item open until every repo in `repos` has one,
  then closes it; repeating a recorded landing is a no-op. A `close` without
  `partial` that leaves a repo unlanded is refused, naming it; a landing in
  a repo the item does not list is refused. `status=dropped` keeps the
  landings and adds the reason.
- Several workers for one piece of work: each worker's item goes in its own
  repo's queue with `origin`, and the sending session adds one parent item
  in its own queue whose `blocked_by` names the children. The parent shows
  in `ready` once they are all closed or dropped.

### Line grammar

Each item is an Obsidian task. What a reader needs reads as part of the
sentence: the body is a wikilink, and kind and owner are
[tags](https://publish.obsidian.md/tasks/Getting+Started/Tags), which a
Tasks query filters (`tags include #queue/decision`) and hides
(`hide tags`). The rest are
[Dataview inline fields](https://blacksmithgu.github.io/obsidian-dataview/annotation/add-metadata/),
so Obsidian, the [Tasks plugin](https://publish.obsidian.md/tasks/) (set its
task format to Dataview) and Dataview render and query the note as it is:

```
- [<mark>] <text> ([[<body note>|notes]]) #queue/<kind> #owner/<owner>  [agent:: <agent>]  [origin:: <repo>]  [repos:: <repo>,<repo>]  [pr:: ...]  [branch:: ...]  [commit:: ...]  [slug:: ...]  [base:: <branch>]  [owns:: <path>,<path>]  [transcript:: <absolute path>]  [alias:: <old id>]  [resolution:: ...]  [id:: <id>]  [dependsOn:: <id>,<id>]  [created:: <YYYY-MM-DD>]
```

- `<mark>` is the status, as the Tasks plugin types it:

  | Status | Mark | Tasks type |
  |---|---|---|
  | `open` | `[ ]` | TODO |
  | `returned` (delegated only) | `[/]` | IN_PROGRESS |
  | `closed` | `[x]` | DONE |
  | `dropped` | `[-]` | CANCELLED |

  A closed or dropped item sits under `## Closed`; `close` takes
  `status=dropped` for work not done. A `[>]` line reads as `open`, and the next write of the line (or
  `reformat`) gives it `[ ]`.
- `<kind>`: `next`, `delegated`, `decision`, `follow-up`, as the tag
  `#queue/<kind>`. `<owner>`, when set, is the tag `#owner/<owner>`: one
  word of letters, digits, `_` or `-`. The tags follow the text (and its body
  link) after one space each, where Tasks keeps tags when it rewrites a line;
  a tag Tasks or a person leaves among the fields at the end of the line
  reads the same.
- `<body note>`: the note holding the item's detail, by its file name
  without `.md`, or its vault path without `.md` where another doc shares
  that file name; it renders as a link reading "notes". The tool's `body` is
  that note's doc_id, resolved through the index both ways. A body whose
  note has no such name (the doc is not indexed) is written as
  `[body:: <doc_id>]` before `alias`; a link whose note is gone stays as
  written and the item reports no `body`.
- `<text>`: one line. A backslash (Markdown's own escape) goes only before
  what a parser would read as something other than text: a `\`; a `[` or
  `(` that a `::` follows before its own closing bracket (a Dataview field
  opener); the first `:` of a leading unbracketed `key::`; a Tasks date
  emoji (✅ ❌ 📅 ⏳ 🛫 ➕) followed by a date; the `#` of a tag in the
  queue's own namespaces (`#queue`, `#owner` and their children, in any
  case), so a word in the text never reads as the item's kind or owner nor
  matches a query for them; and the `(` of a trailing `([[note|notes]])`.
  Ordinary brackets, parentheses, links and other tags stay as typed.
- Fields appear only when set, each after two spaces (Tasks' own separator),
  in the order above. `agent`, `origin`, the links (`commit`
  comma-separated) and `alias` are one word without spaces,
  brackets, parentheses or a backslash; `repos` is a comma list of such
  words without `@`. `base` is the branch the item's work lands on, one
  such word; `owns` is a comma list of the paths its worker owns, each
  without a comma, brackets, a backslash or a line break. `transcript` is
  the absolute path of the worker transcript holding the item's brief and
  report, under the same character rule. `resolution` is one line, such as `repo@sha`, a PR or a
  doc_id; inside its `[resolution:: ...]` only `]` and `\` take a backslash,
  since only a `]` ends the field, so `merged (#213)` is written as typed
  (a resolution that carries escaped parentheses or `[` still reads back
  as typed). A closed item closed through
  `queue` carries one, and an open item carries one only when it has `repos`
  (the `repo@sha` landings so far).
- `id` and `dependsOn` are the Tasks plugin's own dependency fields (the
  tool's `blocked_by`), last because Tasks reads fields from the end of the
  line and stops at the first it does not know. `created` is Tasks' own
  created date, after them where Tasks reads it: `add` writes it on every
  new item as the day it was filed, and `list` returns it as `created`. A
  hand-typed `➕ YYYY-MM-DD` reads as the same field and is written back as
  `[created:: …]`. Tasks' `is blocked` and
  `is blocking` filters and `ready` agree: a dependency stops blocking once
  it is closed or dropped.

```
- [/] Build the store ([[Store Design|notes]]) #queue/delegated #owner/worker  [agent:: a1b2]  [branch:: pass/store]  [commit:: abc1234,def5678]  [id:: ex-4]
- [ ] Read map[string] keys \(see \[note:: x]) after \#queue/next lands #queue/next  [id:: ex-5]  [dependsOn:: ex-4]
- [x] Ship on Friday? #queue/decision #owner/main  [resolution:: yes]  [id:: ex-2]
```

Lines in the older grammar, with `[kind:: <kind>]`, `[owner:: <owner>]`
and `[body:: <doc_id>]` fields, still read (a tag in such a line is its
text's); every write gives the line it changes the current form, and
`reformat` rewrites a whole note. A Tasks query over the queues:

````
```tasks
not done
tags include #queue/decision
hide tags
```
````

Editing the note in Obsidian is fine; every write re-reads it under the
lock. A box ticked there is closed: the Tasks plugin's `[completion:: date]`
or `✅ date`, Dataview's completion field, and any other field the queue does
not own are kept as written (Tasks' own fields after `dependsOn`, the rest
before `id`). A ticked item has no resolution until `close` records one,
which also files it under `## Closed`; reopening it with `set` drops the
resolution and the done and cancelled dates.

### Format version

A queue note carries `queue_format: <n>` in its frontmatter, one integer the
labs profile declares: the line format it was written in. Every write
stamps the format the running binary writes (currently 3); a note without
the field predates it. So that no process, of any version, writes a note it
does not fully read, every write (`add`, `set`, `close`, `reformat`) is
refused, writing nothing, when the note:

- carries a `queue_format` above the one the binary knows, or one that is
  not a whole number;
- holds an item-shaped line (`- [<char>] …`, unindented) under `## Open` or
  `## Closed` that the binary cannot parse. The error quotes the line.

Both refusals carry the next step "this server may be older than the note's
format: reconnect it (/mcp) or upgrade the binary"; a line typed by hand is
fixed or removed in the note. `list` still reads such a note and counts the
lines it skipped in `unparsed_lines`; a `- [` line outside those headings is
counted there and does not block writes.

### Reformat

`reformat` rewrites every item line of one queue (`project`, `repo` or
`cwd`) or of every queue (`all=true`) in the current grammar, through the
same locked write, and is how a change to the grammar reaches existing
notes. It reads the older grammars it knows and stamps the current
`queue_format`. In the same write it drops what a queue note does not
need: `environment: other`, `sensitivity: internal`, `related_projects`
naming only the note's `system`, `tags` of only `queue`, any of those or
`related_assets` left empty, and an H1 at the top that repeats the title; a
value chosen by hand stays. It keeps each line where it is (a ticked box
under `## Open` stays there), writes a note only when a line, the stamp or
one of those changes, so a second run writes nothing, and returns `{ok,
count, changed: [{project, relative_path, lines, fields_dropped?,
h1_dropped?}]}`, with `unparsed_lines` for `- [` lines outside
`## Open` and `## Closed` (left as they are) and `skipped_restricted` for
queues it did not touch. A note it may not write (see
[Format version](#format-version)) stops the run with that refusal and the
`changed` list so far.
`drop_aliases=true` also removes every item's `alias` and reports
`aliases_dropped`; the alias field itself stays for the next import.

### CLI for hooks

`severino-vault-mcp queue <action> [id] [flags]` runs the same function as
the MCP tool against the labs vault and prints one line of compact JSON
(`--pretty` indents it). It exits 0 when `ok` is true, 1 on a refused call
(the JSON carries `error` and `next_steps`), 2 on a usage error (stderr).
Pass a value that starts with a dash as `--flag=value`. Items in the output
carry `id, project, kind, status, owner, text, links`, plus `blocked_by,
body, alias, agent, origin, repos, base, owns, transcript, created, resolution` when set; `--fields` narrows
them.

`--owner` is one word of letters, digits, `_` or `-`, since the line writes
it as the tag `#owner/<owner>` and a tag ends at any other character; a
value outside that is refused. A hook that records an agent type as the
owner passes the part after its last `:` (`worker` for
`severino-ops:worker`).

A brief that starts from a queued item (an `Item: <id>` line) launches its
worker with one atomic call, `queue set <id> --claim --owner <type> --agent
<agent id> --kind delegated --branch <branch>`: the claim, owner, agent, kind
and branch land in one locked edit, so a lost claim changes nothing and no
duplicate delegated item is created. A claim is refused when another agent
holds the item (the refusal carries `agent`), when another owner holds it
(`owner`), and when the item is closed or dropped; a claim that succeeds
leaves the item `open`, so a returned item its worker claims again is open
once more. Landing then closes it by agent: `queue close --agent <agent id>
--resolution <repo>@<sha>`.

A brief without an `Item:` line files its worker with `queue add --agent
<agent id> --text <goal>`. The add is idempotent for a relaunch: when that
agent already holds an open item with the same text in that queue, the call
returns it with `no_op: true` instead of filing a second.

`queue set <id> --release` hands an item back to the queue in one locked
edit: no agent or owner, kind `next` (from `delegated`; another kind stays),
status `open`. With `--agent`, the item must be held by that agent, so a
stale release never frees another worker's item. A done item is not
released, and `--release` takes no `--claim`, `--owner`, `--status` or
`--kind`.

A `set` or `close` that changes an item an agent holds (status `open` or
`returned`, with an agent) without naming that agent goes through, and its
result carries `warning`: `<id> was held by agent <agent>`.

| Action | Flags |
|---|---|
| `add` | `--cwd`, `--repo` or `--project` (`--project backlog` for the backlog); `--kind`, `--text`; optional `--owner`, `--agent`, `--origin <repo or dir>`, `--repos <repo>,<repo>`, `--base <branch>`, `--owns <path>,<path>`, `--transcript <absolute path>`, `--pr`, `--branch`, `--commit`, `--slug`, `--alias=<id>`, `--body=<doc_id>`, `--blocked-by <id or alias> ...` |
| `list` | `--cwd`, `--repo`, `--project` or `--all` (`--agent`, `--origin`, `--alias` or a link flag alone searches every queue); `--include-origin`; `--kind`, `--status <s,s>` or `all` (default `open,returned`), `--owner`, `--branch <name>` (or any link flag: items whose link is exactly that value), `--ready`, `--contains`, `--limit` |
| `set` | `<id or alias>` or `--agent`; `--status open\|returned`, `--owner` (with `--claim`: only if no other owner or agent holds it and it is not done), `--agent` (with an id: writes the agent onto the item; with `--release`, the holder it must have), `--release`, `--kind delegated` (the only kind change: from next or follow-up), `--text`, link flags, `--alias`, `--body` (`''` removes one), `--blocked-by`, `--repos`, `--base` (`''` removes it), `--owns` (`--owns=` clears it), `--transcript` (`''` removes it) |
| `close` | `<id or alias>` or `--agent`; `--resolution` (`repoA@sha,repoB@sha` for an item with repos); `--partial` to record landings; `--status dropped` for work not done; link flags |
| `reformat` | `--cwd`, `--repo`, `--project` or `--all`; `--drop-aliases` |

## Frontmatter fields

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
`fields`. A list left empty is not written: every writer leaves the key
out, which every reader takes as no value.

A doc_type may name its own required set in its document schema
(`schema --contract` carries them under `doc_type_required_fields`); it
replaces the profile's `required_fields` for that type, and a create
writes no default for a field the set leaves out. The labs profile's
`queue` type requires `doc_id`, `title`, `doc_type`, `system`, `status`
and `queue_format`; its `report` type requires the profile's set and
`kind`.

A field may name the doc_types that carry it (`doc_types` in its contract):
the labs `kind` (`audit`, `review`, `research`, `pass`, `session_log`,
`evaluation`) is a report's alone.

A doc_type may name the folder its docs live in (`doc_type_folders` in
`schema --contract`): a live doc of the type sits under a folder of that
name, at any depth, and a live doc under one is of the type. In the labs
profile a `report`, a dated record of one run (an audit, review, research
pass, session or evaluation) written once and superseded by the next in
its series, lives in a `Reports/` folder, and only reports do. The archive
keeps a doc where it was archived from, so the rule holds live docs only.

`set_frontmatter` refuses an update that would leave the block failing
`validate` in a way it did not before (a `kind` on a non-report, a
`doc_type` changed to one whose required set or folder the doc lacks); a
problem the doc already had does not block an edit elsewhere in the block.

A `related_projects` entry names a project folder, matched case-sensitively:
`01 Projects/<name>/` holding an `index.md`, `<archive>/Projects/<name>/`, or
`backlog`. This needs the vault's folders, so it is checked beside the
doc-reference rule rather than in the per-file one: `set_frontmatter` and
`write_doc` create refuse an entry they add that names none (the error
lists the closest folder names), an entry already in the block does not
block an edit elsewhere, and `doctor` and `validate` report every entry that
names none. A vault without a projects folder checks nothing.

## Frontmatter as JSON Schema

`severino-vault-mcp schema --json-schema [--vault labs|edu]` emits a vault's
frontmatter rules as a [JSON Schema](https://json-schema.org) (draft
2020-12), derived from the same declarations, so an off-the-shelf validator
or an editor checks a block without this server:

```bash
severino-vault-mcp schema --json-schema > labs.schema.json
check-jsonschema --schemafile labs.schema.json note-frontmatter.json
```

- Required fields, enums, ISO dates, doc_id prefixes as a `pattern`, list
  item patterns, integer and boolean kinds. A value the profile treats as
  absent (`null`, `""`, `[]`) passes for an optional field and fails a
  required one.
- A task (`doc_type: task`) is held to the task's required fields and
  lifecycle through `if`/`then`/`else`; a doc to the doc's.
- Numbers stay text in the frontmatter parser, so an integer field also takes
  digits as a string.
- What a schema can't check is in the field's `description` and stays with
  `validate`: a field's path scope, uniqueness across docs, and whether a doc
  reference names a doc that exists. The folder a doc_type lives in is said
  in the schema's `description`. A field only some doc_types carry is held
  absent on the others.

A test holds `schema.Profile.Check` and a JSON Schema validator to the same
verdict on every fixture doc; `tests/golden/labs.schema.json` pins the labs
schema. `schema --json` (the enum sets HQ commits) is a separate, frozen
shape.

## Section edits

`write_doc` modes `replace_section` and `append_section` (CLI `write-doc
--mode`) edit one section of a registered doc, and a section is what
`read_doc` returns for it: `heading` takes a section slug, a `heading_path`
as `find` and the outline give it (`Parent > Child`), or a heading's text,
resolved the way `read_doc`'s `sections` resolves them.

- `replace_section` replaces exactly the lines `read_doc` returns for that
  section, after its heading line, which stays. An H2 small enough to stay
  whole holds its H3s, so they are replaced with it; an H2 split at its H3s
  is only the text above the first. A heading that is no section of its own
  (an H3 inside a whole H2, an H4) runs to the next heading of any level. A
  heading hard-wrapped into parts is one section by its text or path; a
  part's slug names only that part.
- `append_section` adds the body at the end of the text under the heading,
  before its first subheading.
- `heading: ""` is the text before the first heading.

Refused, with `next_steps`: a heading text several headings share (the
error and `candidates` list each one's heading path, or its slug where two
paths are the same, and a step per candidate), and a body whose first
non-blank line is the section's own heading line, same level and text (send
the body without it).

## Moving a doc

`move_doc` (CLI `move-doc`) moves or renames a registered doc by `doc_id` or
`relative_path`. `to` is a vault-relative `.md` path, or a folder ending in
`/` that keeps the file name. The doc_id and frontmatter don't change.
Refused, each with `next_steps`:

- a destination that exists, or one outside the indexed folders or inside a
  read-only one (and a doc in a read-only folder);
- a file name another note anywhere in the vault already has, compared
  case-insensitively: Obsidian resolves `[[Name]]` by unique file name, so a
  second one would change what links point to. A rename in the same folder is
  how a shared name like `Audit.md` gets a unique one;
- a move that takes a path-scoped field out of its scope.

`with_folder=true` moves the doc's whole folder into `to` (a folder), for a
report folder with evidence files; the folder may hold no other doc. The move
holds both notes' locks and never replaces a file. The result is
`{ok, doc_id, from, to, links}`. Afterwards every `[[path/link]]`,
`![[embed]]` and `[text](path)` in the vault that named the doc by path names
its new path (alias and `#heading` kept, links by file name alone untouched),
through the same rewrite and note locks as `move_folder`; `links` lists each
rewritten note with a count, `failed` the notes it could not rewrite, and
restricted notes are not read but listed in `skipped_restricted`.

## Moving a folder

`move_folder(from, to)` (CLI `move-folder <from> <to>`) moves or renames a
folder inside an indexed folder or the archive, with everything in it: notes,
`Reports/`, assets. It holds the lock of every note in the folder, at its old
and its new path, and never replaces anything. Refused, each with
`next_steps`: a destination that exists or lies inside `from`, a top-level
folder itself, a read-only folder at either end, and a move that takes a
path-scoped field out of its scope.

After the move every note in the vault is read and each link that named a
file in the folder by path names its new path, through `fsx.Vault.Update`:
`[[01 Projects/x/index|…]]`, `![[01 Projects/x/diagram.png]]`, a partial path
like `[[x/index]]` that no longer matches, and `[text](<01 Projects/x/a.md>)`
or `[text](01%20Projects/x/a.md)`, written back in the form it had. A
relative markdown link whose base moved (a note inside the folder linking
out, or a note linking in) is recomputed relative to where the note is now.
Links by file name alone resolve wherever the file is and stay as they are,
and so do links in fenced code and inline code. Only bodies are rewritten;
frontmatter changes go through `set_frontmatter`'s path. `links` lists each
rewritten note with a count. A restricted note is rewritten only with
`include_restricted=true` and an approved unlock (Touch ID, once per call,
audited per note); otherwise it is listed in `skipped_restricted`. A write
that fails after the folder moved is listed in `failed`.

Renaming a project, `01 Projects/<old>` to `01 Projects/<new>`, also carries
its slug (the folder name):

- every `related_projects` entry naming `<old>` (case-insensitively) becomes
  `<new>`, through `set_frontmatter`'s path; a note outside the writable
  folders keeps its entry and is listed in `project.related_projects_skipped`;
- the queue note keeps its doc_id (doc_ids never change, so it stays
  `queue-<old>` and the queue tool reports that id), and its `title`,
  `system` and any `# <old> Queue` heading take the new name when they
  carried the old one;
- queue item ids never change. A project's new items take its
  `queue_prefix`, else the slug of its folder name; when `index.md` sets no
  `queue_prefix` and the old slug is a valid one, it is pinned there so new
  items continue the old prefix. A slug `queue_prefix` cannot hold (one with
  a hyphen) is not pinned, and new items take the new folder's slug;
  `project.queue_prefix` says which.

## The archive

`99 Archive` (`[vault].archive_dir`, `SVMC_ARCHIVE_DIR`; the default only
while the indexed dirs are the defaults) holds retired docs and projects. The
index reads it apart from the indexed and read-only folders: `find` leaves
its docs out unless `include_archived=true`, `read_doc` reads them by doc_id,
and boards, the brief and `validate` never see them. A live doc keeps a
doc_id an archived one shares. Nothing writes in the archive but a move and
the link rewrite a folder move makes: `move_doc` and `move_folder` take a doc
there only when it has
`status: archived` (a task: `done` or `wontfix`), and `archive=true` sets
`status: archived` on each doc first. The archive keeps a doc's frontmatter
as it was, so path-scoped fields are not checked there.

A doc in the archive is maintained like any other: `set_frontmatter` and
`write_doc` (sections, and create) accept it when the doc is, or the same
call makes it, `status: archived` (a task: `done` or `wontfix`), and refuse
anything else there. A write there checks `doc_id`, `title`, `doc_type` and
`status` and each field's value, not the live required set or path scopes,
so `set_frontmatter` registers an archive note that has no block with just
those four (`status: archived`).

## Superseded reports

`superseded_by` holds the doc_id of the doc that replaces this one. When a
newer report of a series is written, the old one gets `superseded_by` (the
new doc_id) and `status: deprecated`. `validate` and `doctor` refuse a
`superseded_by` that names no doc in the vault, and `set_frontmatter` won't
write one. `doctor` warns on a deprecated report (`doc_type: report`) with no
`superseded_by`, and on a `superseded_by` that names a doc that is not live
(deprecated or archived).

## Drift

`doctor` reads the body of every live doc (the indexed dirs; not the archive
or the read-only folders) for structure the vault has left behind and code
its repositories no longer have. A report, a deprecated doc and a closed log
entry record the past, so their text is checked for neither:

- a mention of retired structure is a warning, one per term per doc with the
  body lines it is on. The terms are the profile's `retired` list (`schema
  --contract`), each a regular expression with the help doctor prints; the
  labs profile lists `00 Reporting`, a `tasks/` path segment, `Backlog.base`
  and `[[task-` links.
- a path wikilink (`[[folder/note]]`, `![[folder/file.png]]`) that names no
  file in the vault, from the root, from the note's folder, or as the end of
  a file's path, is an error. A link by file name alone, and links in code,
  are not checked.
- code a living doc names that its repository lacks is a warning, one per
  doc listing each missing reference with its body lines. A living doc here
  is `status: active` or `draft` and not a report or a work queue. Its
  repositories are the `project_path` of the project folder it sits in and
  of each of its `related_projects`. Only inline code spans are read, never
  prose or fenced code, for three kinds of reference:
  - a relative path holding a `/` that names a file (its last part has an
    extension) or a folder below the top (`docs/old/`, or Go's
    `./internal/...`). It is there when it is in a repository, its
    `.gitignore` covers it (build output), it is a vault path from the doc's
    folder, its project's folder, `01 Projects` or the vault root, or it
    starts with a project's repository name and is in that repository.
    Absolute and home paths, URLs, host and module paths, globs,
    placeholders, a lone top folder (`images/`), a vault path written with
    its folder number (`03 Runbooks/x.md`) and anything after a `cd` are not
    references.
  - a Go or Python test name (`TestXxx`, `test_xxx`, and pytest's
    `file::test`), there when a `.go` or `.py` file in the repository holds
    it.
  - `mise run <task>` (each task after `:::` too, not a run with `-C`),
    there when the repository's mise config files define it under `[tasks]`
    (a name or an alias) or it is a file task in the default task folders or
    those `task_config.includes` names.

  A project whose `project_path` is not a folder on this machine gets one
  warning at its `index.md`, and its docs are not checked.

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
