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
| Specific runbook question | `find` | `read_doc` on the top hit with `sections=[<the hit's section>]`; more sections, or `fields=["body"]`, when the section isn't enough. Quote commands exactly. |
| Known doc ID | `read_doc` or `vault://{vault}/doc/{doc_id}` | Respect the sensitivity policy. |
| System context | `find(query, system="...")` | Read the selected doc before summarizing. |
| Docs for a project | `find(query, project="...")` | Read before summarizing. |
| Queue notes (`doc_type: queue`) | `find(query, doc_type="queue")` | `find` leaves them out otherwise; `queue` is the tool to work them. |
| Body search | `find(by="text")` | Docs holding any of the words, ranked by relevance; `missing_words` on a partial hit, `ranking` says how. Snippets are for discovery; read the doc before final instructions. |
| A doc named by its doc_id, title or alias | `find(query=<the name>)` | That doc ranks first in either mode, with `exact_match` and a `read_doc` step. |
| New doc, or a section edit | `write_doc` | Body plus frontmatter in one validated call; never edit the file directly. |
| A sentence or bullet in a doc | `write_doc(mode="replace_text", old_text=..., new_text=...)` | `old_text` must occur once; `heading` narrows where it is looked for. |
| Missing or stale metadata | `set_frontmatter` | Report changed fields and the next sync step. |
| Move or rename a doc | `move_doc(doc_id, to)` | Path links follow; read `links` and `skipped_restricted`. Never move a vault file with the shell. |
| Move or rename a folder or project; retire one | `move_folder(from, to)` (`to` in `99 Archive/` with `archive=true` to retire) | Path links and `related_projects` follow; read `links`, `skipped_restricted` and `failed`. |
| Archived docs | `find(query, include_archived=true)` | `find` leaves `99 Archive` out otherwise. |
| A newer report of a series | `write_doc` for the new one | `set_frontmatter` on the old one: `superseded_by` (the new doc_id) and `status: deprecated`. |
| Open entries in a log note | `log_entries(doc_id, contains=...)` | `log_entries(action="close", match=..., resolution=...)` closes one; `action="annotate"` rewrites a closed one's resolution, or reopens it with `reopen=true`; never read or edit the log whole. |
| Work to track: next, delegated, a decision, a follow-up | `queue(action="add", cwd=..., kind=..., text=...)` | `queue(action="list", cwd=...)` to see it; `set`, `close` by `id` (or `agent`). Never keep it in scratch files. |
| Duplicate doc ID response | `doctor` (CLI) | Report every conflicting path; don't pick one. |

## Failures

Every `ok: false` result is an MCP tool error (`isError`) carrying
`{"ok": false, "error": "..."}` and `next_steps`: short
strings, each starting with a call to a registered tool, such as
`find(query="...")` for an unknown doc_id or
`read_doc(doc_id="...", fields=["outline"])` for a heading that isn't there.
`read_doc`'s not-found and withheld answers, and a `find` with no hits, carry
them too; when a `system`, `project` or `doc_type` filter matches no doc, the
first steps are the same `find` with the closest values the vault holds. A
`find` result is held to 12,000 characters (`core.FindBudget`): past it,
snippets shrink to their matched lines and then go, then hits go from the
bottom, and `truncated` (`budget_chars`, `snippets_trimmed`, `hits_dropped`)
with `next_steps` says what was cut and the calls that get it. An argument the schema refuses (an unknown name, a wrong type, a
value outside its enum, a missing required one) fails the same way, its step
the corrected call: a filter given as `by` becomes that argument, a refused
`fields` name becomes the field it most plausibly meant. Take a
next step before guessing.

A vault this process cannot read (its path is missing, not a directory, or
denied, or an indexed folder under it is denied) fails every tool that loads
the index, and `doctor`, `validate` and the CLI commands over the labs vault,
with `{"ok": false, "error": "vault 'x' is unreadable ...: <OS error naming
the path>"}`. Its `next_steps` say what to check (the configured vault path
exists, this process may read it) instead of calling a tool. Never read it as
"no such doc": an empty vault that can be read stays `ok`, a configured
indexed folder that does not exist is simply absent.

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
| A project's work queue | `severino-vault-mcp queue <list\|add\|set\|close> [id] --cwd <dir> ...` (see [Work queue](#work-queue)) |
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
with only the fields the labs profile requires of `doc_type: queue` and no
H1. Open and returned items live under `## Open`, closed and dropped ones
under `## Closed`. An agent writes the note only through `queue`; a person
may edit it in Obsidian, since every write re-reads it under the
[note lock](#writing-a-note-from-outside-the-server).

### Projects, repos and ids

A project's `index.md` may carry the two keys the queue reads, set through
`set_frontmatter` (`repos`, `add_repos`, `remove_repos`, `queue_prefix`) or
`update-frontmatter`; their rules are in
[Frontmatter fields](#frontmatter-fields):

```yaml
queue_prefix: ex                                # ids are ex-1, ex-2, ...; default: the folder name as a slug
repos: [example-tracker, example-tracker-ext]   # repository directory names
```

A call names its queue by `project` (the folder name, or `backlog`), `repo`,
or `cwd`. A `cwd` is tried folder by folder from itself up; a folder whose
`.git` file points into `<repo>/.git/worktrees/` also tries `<repo>`. A repo
two projects list is refused naming both. A write from a `repo` or `cwd` no
project lists is refused, and its `next_steps` name
`set_frontmatter(relative_path="01 Projects/<project>/index.md", add_repos=["<repo>"])`;
the backlog takes writes only as `project="backlog"`. A `list` there reads
the backlog, with `fallback` its path and `warning` the sentence to show.

Ids are `<prefix>-<n>`, issued under the note's lock as one more than the
largest `n` written as an id field (`[id:: …]`, `(id:: …)` or `🆔 …`) in any
queue note, read from the text, so no id is issued twice. `set` and `close`
name an item by `id`, or by `agent` when that agent holds one open item;
an agent holding several is refused with their ids. One id per call: an id
that is not `<prefix>-<n>` (several joined by spaces, say) is refused with the
form, and an id whose prefix no queue uses is refused with the prefixes in
use, naming the project's own when the id starts with the project's name. A
link key (`branch`, `pr`, `commit`, `slug`) given at the top level of the MCP
call is refused with the call that puts it inside `links`.

### Line grammar

Each item is one Obsidian task: kind and owner are
[tags](https://publish.obsidian.md/tasks/Getting+Started/Tags), the body is
a wikilink, and the rest are
[Dataview inline fields](https://blacksmithgu.github.io/obsidian-dataview/annotation/add-metadata/),
so the [Tasks plugin](https://publish.obsidian.md/tasks/) (Dataview task
format) and Dataview read the note as it is. `internal/queue` holds the
grammar and its tests; `Parse(Format(item)) == item`.

```
- [<mark>] <text> ([[<body note>|notes]]) #queue/<kind> #owner/<owner>  [agent:: <agent>]  [origin:: <repo>]  [repos:: <repo>,<repo>]  [pr:: ...]  [branch:: ...]  [commit:: ...]  [slug:: ...]  [base:: <branch>]  [owns:: <path>,<path>]  [transcript:: <absolute path>]  [resolution:: ...]  [id:: <id>]  [dependsOn:: <id>,<id>]  [created:: <YYYY-MM-DD>]
```

| Status | Mark | Tasks type |
|---|---|---|
| `open` | `[ ]` | TODO |
| `returned` (delegated only) | `[/]` | IN_PROGRESS |
| `closed` | `[x]` | DONE |
| `dropped` | `[-]` | CANCELLED |

- `<kind>` is `next`, `delegated`, `decision` or `follow-up`. `<owner>`,
  when set, is one word of letters, digits, `_` or `-`.
- `<body note>` is the doc holding the item's detail, by its file name
  without `.md` (its vault path where another doc shares the name); the
  tool's `body` is that doc's doc_id. A doc with no such name is written as
  `[body:: <doc_id>]`.
- `<text>` is one line, written with a backslash before anything a parser
  would read as a field, a date, a `#queue` or `#owner` tag, or a trailing
  body link.
- Fields appear only when set, in this order, two spaces apart. `agent`,
  `origin`, `base` and the links are one word without spaces, brackets,
  parentheses or a backslash; `repos` is a comma list of such words without
  `@`; `owns` and `transcript` are paths without a comma, brackets, a
  backslash or a line break, `transcript` absolute. `resolution` is one line
  (only `]` and `\` take a backslash); a done item carries one once `close`
  records it, and an open item only as the `repo@sha` landings of its
  `repos`.
- `id`, `dependsOn` (the tool's `blocked_by`) and `created` are Tasks' own
  fields, last because Tasks reads fields from the end of the line. `add`
  writes `created` as the day it was filed; a typed `➕ YYYY-MM-DD` reads as
  it. A dependency stops blocking once it is closed or dropped, for Tasks'
  `is blocked` and for `ready` alike.
- A field the queue does not own (Tasks' `[completion:: …]` or `✅ …` on a
  box ticked in Obsidian, say) is kept as written. A ticked item is closed
  without a resolution until `close` records one and files it under
  `## Closed`. A closed or dropped item is not reopened: `set` with a status
  or a claim refuses it and names `add` for the work as a new item.

```
- [/] Build the store ([[Store Design|notes]]) #queue/delegated #owner/worker  [agent:: a1b2]  [branch:: pass/store]  [commit:: abc1234,def5678]  [id:: ex-4]
- [ ] Read map[string] keys \(see \[note:: x]) after \#queue/next lands #queue/next  [id:: ex-5]  [dependsOn:: ex-4]
- [x] Ship on Friday? #queue/decision #owner/main  [resolution:: yes]  [id:: ex-2]
```

A write is refused, writing nothing, when the note holds an item-shaped
line (`- [<char>] …`, unindented) under `## Open` or `## Closed` that does
not parse (the error quotes it), or one id on two item lines. `list` still
reads such a note and counts the lines it skipped in `unparsed_lines`.

### Workers across repos

- A worker sent from one repo to work in another is filed in the queue of
  the repo it works in (`cwd` its worktree), with `origin` the sending
  repo (a name, or a working directory resolved as `cwd` is).
  `include_origin=true` on a `list` by `cwd`, `repo` or `project` adds the
  delegated items in any other queue whose `origin` is that repo (by
  `project`, any repo it lists), with the default fields `id, project,
  kind, status, owner, text, origin`.
- One worker whose work spans repos is one item in the sending repo's
  queue, with `repos` listing them; its resolution is `repo@sha` per repo,
  comma-separated. `close` with `partial=true` adds the landings it is
  given and closes the item once every repo has one; a `close` without it
  that leaves a repo unlanded, or a landing in a repo the item does not
  list, is refused. `status=dropped` keeps the landings and adds the
  reason.
- Several workers for one piece of work: one item each in its own repo's
  queue, and a parent in the sending queue whose `blocked_by` names them.

### CLI for hooks

`severino-vault-mcp queue <list|add|set|close> [id] [flags]` runs the MCP
tool's function against the labs vault and prints one line of compact JSON
(`--pretty` indents it). It exits 0 when `ok` is true, 1 on a refused call
(see [Failures](#failures)), 2 on a usage error (stderr). Pass a value that
starts with a dash as `--flag=value`. Items carry `id, project, kind,
status, owner, text, links`, plus `blocked_by, body, agent, origin, repos,
base, owns, transcript, created, resolution` when set; `--fields` narrows
them. `describe` lists every flag.

- A hook that records an agent type as the owner passes the part after its
  last `:` (`worker` for `plugin:worker`).
- A brief that starts from a queued item (an `Item: <id>` line) launches
  its worker with `queue set <id> --claim --owner <type> --agent <agent id>
  --kind delegated --branch <branch>`, one locked edit. The claim is
  refused when another agent holds the item (the refusal carries `agent`),
  another owner does (`owner`), or the item is done; it leaves the item
  `open`. With `--kind delegated` it also takes a `decision` item that
  `main` owns or nobody owns, keeping its id and text; a decision owned by
  anyone else is refused with that `owner`. Landing closes it with `queue close --agent <agent id>
  --resolution <repo>@<sha>`.
- A brief without one files its worker with `queue add --agent <agent id>
  --text <goal>`; when that agent already holds an open item with that text
  in that queue, the call returns it with `no_op: true`.
- `queue set <id> --release` empties agent and owner, turns `delegated`
  into `next` and sets `open`; with `--agent`, only if that agent holds the
  item. It takes no `--claim`, `--owner`, `--status` or `--kind`.
- A `set` or `close` that changes an item another agent holds goes
  through, with `warning`: `<id> was held by agent <agent>`.

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
declare is refused. `unset` (`--unset` on the CLI) takes field names to
remove from an existing block with any list items, including a leftover key
the profile no longer declares; a field the doc's type requires is refused,
and the result passes the same introduced-problems check as any update. `schema --contract` carries the declarations under
`fields`. A list left empty is not written: every writer leaves the key
out, which every reader takes as no value.

A doc_type may name its own required set in its document schema
(`schema --contract` carries them under `doc_type_required_fields`); it
replaces the profile's `required_fields` for that type, and a create
writes no default for a field the set leaves out. The labs profile's
`queue` type requires `doc_id`, `title`, `doc_type`, `system` and
`status`; its `report` type requires the profile's set and
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
- In a profile that declares tasks (a provider's; labs and edu track work in
  the queue), a task (`doc_type: task`) is held to the task's required fields
  and lifecycle through `if`/`then`/`else`; a doc to the doc's.
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

## Reading a doc

`read_doc` with no `sections` and no `fields` returns a body of up to
`core.BoundedBodyChars` (20,000) characters whole. Past that it returns
`truncated: true`, the `lead` (the text before the first section heading, cut
at a line near 2,000 characters), the `outline`, and `next_steps` naming a
sections read and `fields=["body"]`, which returns the whole body at any size.
The CLI's `read` prints the whole body.

The `outline` (also `fields=["outline"]`) lists each section in order:
`section` (its slug), `heading_path`, and `chars`, the size of what reading
that entry returns.

`sections` takes, for each entry, one of:

- a section slug, a part's slug (`-part-N`), a heading path (`Parent >
  Child`) or a heading's text, case aside;
- the slug of the text before the first heading (usually `overview`);
- a 1-based position in the outline (`"3"`), when no heading has that text;
- a prefix of section slugs that names one section, or one section and the
  sections inside it (then the outer one).

One resolver, `sections.Address`, reads every form. A name that matches no
section returns `section_error` and `available_sections` (the outline); one
that matches several returns `section_error` and `candidates`, the names that
each read one of them. Neither releases text.

## Section edits

`write_doc` modes `replace_section`, `append_section` and `delete_section` (CLI
`write-doc --mode`) edit one section of a registered doc, and a section is what
`read_doc` returns for it: `heading` takes a section slug, a `heading_path`
as `find` and the outline give it (`Parent > Child`), or a heading's text,
resolved as `read_doc` resolves those forms. An outline position or a slug
prefix names a section for a read only.

- A heading, by its text, heading path or section slug, names its whole
  block: the heading and everything up to the next heading of the same or a
  higher level, its subheadings included. The token cap that splits a long
  H2 into outline entries for `find` is no boundary for a read or an edit.
  A part's slug (an outline entry ending in `-part-N`) names only that part.
- `replace_section` replaces the block after its heading line, which stays.
  It refuses a body that would drop a subheading the block holds now (same
  level and text, as many times as the block has it); the error and
  `subsections` name each one's heading path, and `next_steps` read the whole
  block or replace one subsection on its own.
- `append_section` adds the body at the end of the block, after its last
  subsection.
- `delete_section` removes the whole block, heading line included, and takes
  no body (the CLI reads no stdin). It refuses `""` (the preamble is no
  section; empty it with `replace_section`), a part's slug, and a block whose
  removal leaves the doc without a heading. Same lock, same `last_reviewed`
  rule and result shape as `replace_section`.
- `heading: ""` is the text before the first heading.

Also refused, with `next_steps`: a heading text several headings share (the
error and `candidates` list each one's heading path, or its slug where two
paths are the same, and a step per candidate), and a body whose first
non-blank line is the section's own heading line, same level and text (send
the body without it).

## Text edits

`write_doc` mode `replace_text` (CLI `write-doc --mode replace_text
--old-text ... --new-text ...`, no stdin) changes one passage of a
registered doc's body: a sentence, a bullet, a line.

- `old_text` is matched byte for byte, whitespace and line breaks included,
  and must occur exactly once; overlapping matches count separately. With
  `heading` (resolved as the section modes resolve it, `""` for the text
  before the first heading) only that block, heading line included, is
  searched.
- `new_text` replaces it; `""` removes it. Include the trailing line break
  in `old_text` to remove a whole line. `new_text` equal to `old_text` is
  refused.
- Refused with `count` and `next_steps`: no match (the error says whether
  the text occurs elsewhere in the body, or only in the frontmatter, which
  this mode never searches or edits; `set_frontmatter` changes that), and
  several matches (`sections` lists the heading paths they fall under when
  those differ, with a step per heading).
- Same lock, `last_reviewed` rule and result shape as `replace_section`,
  with `mode: "replace_text"` and `heading` when given.

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
`status: archived` (in a profile with tasks, a task: `done` or `wontfix`), and `archive=true` sets
`status: archived` on each doc first. The archive keeps a doc's frontmatter
as it was, so path-scoped fields are not checked there.

A doc in the archive is maintained like any other: `set_frontmatter` and
`write_doc` (sections, and create) accept it when the doc is, or the same
call makes it, `status: archived` (a provider's task: `done` or `wontfix`), and refuse
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
or the read-only folders) for links and code the vault no longer has. A
report, a deprecated doc and a closed log entry record the past, so their
text is checked for neither:

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

- Lock file: `<state dir>/locks/<id>/<name>.lock`, outside the vault so a
  synced vault never uploads it.
  - `<state dir>` is `$XDG_STATE_HOME/severino-vault-mcp` when
    `XDG_STATE_HOME` is set to an absolute path, else
    `$HOME/.local/state/severino-vault-mcp`.
  - `<id>` is the first 16 lowercase hex characters of the SHA-256 of the
    vault root's absolute path with symlinks resolved, no trailing `/`, as
    UTF-8 bytes.
  - `<name>` is the note's path relative to the vault root, clean (no `./`,
    `..` or doubled `/`), with every "/" replaced by "%".
  - Example: vault root `/srv/example-vault`, note `Projects/Example/queue.md`,
    `XDG_STATE_HOME` unset, `HOME=/home/joe` gives
    `/home/joe/.local/state/severino-vault-mcp/locks/52f595614f5a30d2/Projects%Example%queue.md.lock`
    (`printf %s /srv/example-vault | shasum -a 256 | cut -c1-16`).
  - Create the directories (mode 0700) and the file if absent; never delete
    them.
- Lock: an exclusive `flock(2)` on that file, held from before the note is
  opened until after it is closed. Never lock the note itself: a rewrite
  replaces its inode, so a lock on the old one guards nothing.
- Write with `O_APPEND`, and keep the hold short: the server waits up to ten
  seconds for the lock, then fails the write with a retry as its next step.

```go
sum := sha256.Sum256([]byte(vaultRealPath))
dir := filepath.Join(stateDir, "locks", hex.EncodeToString(sum[:])[:16])
os.MkdirAll(dir, 0o700)
lock, _ := os.OpenFile(filepath.Join(dir, strings.ReplaceAll(rel, "/", "%")+".lock"), os.O_RDWR|os.O_CREATE, 0o600)
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
