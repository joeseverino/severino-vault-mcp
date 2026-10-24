package cli

import (
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// Arg is one argument of a command. Flags start with "--".
type Arg struct {
	Name       string
	Help       string
	TakesValue bool     // flags only
	Multi      bool     // flag: zero or more values (nargs="*"); positional: one or more, the rest of the line (nargs="+")
	Optional   bool     // positionals only: may be omitted (nargs="?")
	Choices    []string // positionals only
	Metavar    string
	Dest       string // parsed-value key; defaults to the name without dashes, "-" as "_"
	Int        bool   // value must parse as an integer
	Default    string
}

func (a Arg) positional() bool { return len(a.Name) < 2 || a.Name[:2] != "--" }

func (a Arg) dest() string {
	if a.Dest != "" {
		return a.Dest
	}
	name := a.Name
	if !a.positional() {
		name = name[2:]
	}
	out := []byte(name)
	for i, c := range out {
		if c == '-' {
			out[i] = '_'
		}
	}
	return string(out)
}

// Command is one subcommand.
type Command struct {
	Name    string
	Summary string
	Effect  string
	Args    []Arg
	Run     func(*Ctx, Parsed) int
}

const (
	prettyHelp = "Pretty-print JSON with indentation (default: compact)."
	toolName   = "severino-vault-mcp"
	toolDesc   = "Local stdio MCP server for Joe's Obsidian vaults. With no subcommand it serves MCP; " +
		"subcommands run one governed call against the labs vault (or the named dataset) and print JSON."
	fingerprintHelp = "Print a hash of the binary's Go sources and exit. tools compares the installed " +
		"binary with the source tree to detect a stale install."
)

func pretty(help string) Arg { return Arg{Name: "--pretty", Help: help} }

// The frontmatter arguments of update-frontmatter and write-doc's create,
// from the labs profile; a list's whole-list flag is --set-<field>.
var (
	updateArgs = write.Args(schema.Labs.Settable(), "set_", false)
	createArgs = write.Args(slices.DeleteFunc(schema.Labs.Settable(), func(f schema.Field) bool { return f.Name == "doc_id" }), "", true)
)

// frontmatterFlags are the flags of frontmatter arguments.
func frontmatterFlags(args []write.Arg) []Arg {
	out := make([]Arg, 0, len(args))
	for _, a := range args {
		flag := Arg{Name: "--" + strings.ReplaceAll(a.Key, "_", "-"), Help: a.Help()}
		switch {
		case a.Op == write.OpTouch:
		case a.Op != write.OpSet:
			flag.TakesValue, flag.Multi = true, true
		default:
			flag.TakesValue, flag.Int = true, a.Field.Kind == schema.KindInteger
		}
		out = append(out, flag)
	}
	return out
}

// changesFrom reads the frontmatter flags a command line gives.
func changesFrom(p Parsed, args []write.Arg) write.Changes {
	var c write.Changes
	for _, a := range args {
		if !p.Has(a.Key) {
			continue
		}
		var value any
		switch {
		case a.Op == write.OpTouch:
			value = p.Bool(a.Key)
		case a.Op != write.OpSet:
			value = p.List(a.Key)
		case a.Field.Kind == schema.KindInteger:
			value = p.Int(a.Key)
		case a.Field.Kind == schema.KindBoolean:
			value = p.Str(a.Key) == "true"
		default:
			value = p.Str(a.Key)
		}
		c.Apply(a, value)
	}
	return c
}

// fieldsArg is the field mask the MCP tools take as `fields`; given, it also
// trims the result to the MCP's lean envelope.
func fieldsArg(help string) Arg {
	return Arg{Name: "--fields", Help: help, TakesValue: true, Multi: true}
}

// commands is the command surface, in --help and describe order.
var commands []Command

func init() {
	commands = []Command{
		{Name: "doctor", Summary: "Validate configured vault frontmatter without starting the MCP server.", Effect: "read",
			Args: []Arg{{Name: "--propose", Help: "Print starter frontmatter for markdown files that are missing it."}},
			Run:  runDoctor},
		{Name: "touch-reviewed", Summary: "Set last_reviewed to today on a vault doc and print JSON. Exits 0 if ok, 1 otherwise.", Effect: "vault_write",
			Args: []Arg{{Name: "relative_path", Help: "Vault-relative path, e.g. '03 Runbooks/Generate Internal Service Certificate.md'."}, pretty(prettyHelp)},
			Run:  runTouchReviewed},
		{Name: "backfill-aliases", Effect: "vault_write",
			Summary: "Set each folder-note's (`<folder>/index.md`) Obsidian `aliases` to its `title`, so `[[Title]]` resolves " +
				"and autocompletes for notes whose filename is the non-unique `index`. Derived from `title`, so idempotent — " +
				"safe to re-run to repair drift. Writeups are left alone.",
			Args: []Arg{pretty(prettyHelp)},
			Run:  runBackfillAliases},
		{Name: "find", Effect: "read",
			Summary: "Section-scoped vault search: ranked hits, each with its best-matching section (heading, slug, one-line summary), never a body.",
			Args: []Arg{
				{Name: "query", Help: "Natural-language query, e.g. 'renew the TLS cert'."},
				{Name: "--limit", Help: "Maximum hits to return (default 5, capped at 25).", TakesValue: true, Int: true, Default: "5"},
				{Name: "--include-archived", Help: "Also search the archive folder's docs, left out by default."},
				fieldsArg("Hit fields to print, as dotted paths (e.g. doc_id title snippets.text); omit for every field."),
				pretty(prettyHelp),
			},
			Run: runFind},
		{Name: "read", Effect: "read",
			Summary: "Read one vault doc by doc_id and print JSON. With --sections, return just those sections (the token-minimal path); " +
				"without it, the whole body. Honors the sensitivity gate — restricted bodies are withheld (no interactive unlock on the CLI path).",
			Args: []Arg{
				{Name: "doc_id", Help: "Stable doc_id, e.g. 'rb-add-nginx-proxy-host'."},
				{Name: "--sections", Help: "Section slugs or heading paths from a `find` hit or the outline. Omit for the whole body.", TakesValue: true, Multi: true},
				fieldsArg("Doc fields to print (e.g. outline for the section list without text); omit for every field."),
				pretty(prettyHelp),
			},
			Run: runRead},
		{Name: "task-list", Effect: "read",
			Summary: "The backlog board: every `doc_type: task` doc, derived from the index (project tasks under 01 Projects/<project>/tasks/ + " +
				"the 07 Backlog/ cross-cutting bucket), filtered and ranked. Default shows live work (open + active); the thin `backlog` CLI " +
				"and the Obsidian cockpit render this — the MCP is the one task brain.",
			Args: []Arg{
				{Name: "--status", Help: "Only this status.", TakesValue: true},
				{Name: "--project", Help: "Only this project (folder or related_projects).", TakesValue: true},
				{Name: "--stale-only", Help: "Only stale (open/active, untouched past the window)."},
				{Name: "--all", Help: "Include parked/done/wontfix.", Dest: "include_all"},
				{Name: "--stale-days", Help: "Stale window in days (default 14).", TakesValue: true, Int: true, Default: "14"},
				{Name: "--include-queue", Help: "Add the open and returned work queue items (of --project, or every queue) under queue."},
				{Name: "--limit", Help: "Most tasks to print, best first (default 0: all).", TakesValue: true, Int: true, Default: "0"},
				fieldsArg("Task fields to print (e.g. doc_id title status); omit for the whole board."),
				pretty(prettyHelp),
			},
			Run: runTaskList},
		{Name: "promote-note", Effect: "read",
			Summary: "Promote a captured note (e.g. an 00 Inbox/ capture) into a task, preserving its body and deleting the source. " +
				"The capture → task half of the inbox loop; used by the Obsidian promote command.",
			Args: []Arg{
				{Name: "source", Help: "Vault-relative path to the note."},
				{Name: "--title", Help: "Task title.", TakesValue: true},
				{Name: "--project", Help: "Owning project (colocates the task).", TakesValue: true},
				{Name: "--effort", TakesValue: true, Default: "S"},
				{Name: "--priority", TakesValue: true, Default: "med"},
				pretty(prettyHelp),
			},
			Run: runPromoteNote},
		{Name: "update-frontmatter", Effect: "vault_write",
			Summary: "Set fields in a vault doc's frontmatter: the MCP set_frontmatter tool, one flag per field the labs profile " +
				"declares, each validated against it (doc_id never changes once set). Used by the Obsidian relation editor so author-time edits can't dangle.",
			Args: append(append([]Arg{{Name: "relative_path", Help: "Vault-relative path to the doc."}},
				frontmatterFlags(updateArgs)...), pretty(prettyHelp)),
			Run: runUpdateFrontmatter},
		{Name: "write-doc", Effect: "vault_write",
			Summary: "Create a registered doc, or replace or append to one section of its body, with the markdown body on stdin " +
				"(no frontmatter). The MCP write_doc tool: create validates the new frontmatter with the rules validate checks; " +
				"section modes keep the frontmatter except last_reviewed, which becomes today. Restricted docs are refused.",
			Args: append(append([]Arg{
				{Name: "--mode", Help: "create (default), replace_section or append_section.", TakesValue: true, Default: "create"},
				{Name: "--relative-path", Help: "Vault-relative path; required to create.", TakesValue: true},
				{Name: "--doc-id", Help: "The new doc's id on create; the doc to edit otherwise.", TakesValue: true},
				{Name: "--heading", Help: "The section to edit (section modes): a heading path or section slug as find and the read_doc outline give them, or a unique heading text; \"\" for the text before the first heading.", TakesValue: true},
			}, frontmatterFlags(createArgs)...), pretty(prettyHelp)),
			Run: runWriteDoc},
		{Name: "move-doc", Effect: "vault_write",
			Summary: "Move or rename a registered doc, keeping its doc_id and frontmatter: the MCP move_doc tool. Refuses a destination " +
				"that exists, one outside the indexed folders or in a read-only one, and a file name another note already has; " +
				"lists the notes whose [[links]] named the old location.",
			Args: []Arg{
				{Name: "to", Help: "Destination from the vault root: a .md path, or a folder ending in / to keep the file name."},
				{Name: "--doc-id", Help: "The doc to move.", TakesValue: true},
				{Name: "--relative-path", Help: "The doc's current vault-relative path (instead of --doc-id).", TakesValue: true},
				{Name: "--with-folder", Help: "Move the doc's whole folder, files beside it included, into the destination folder."},
				{Name: "--archive", Help: "Into the archive: set status: archived first instead of refusing a doc without it."},
				pretty(prettyHelp),
			},
			Run: runMoveDoc},
		{Name: "move-folder", Effect: "vault_write",
			Summary: "Move or rename a folder with everything in it, then point every path link in the vault ([[path]], ![[embed]], " +
				"[text](path)) at the new path: the MCP move_folder tool. A project renamed under 01 Projects also updates related_projects " +
				"and retitles its queue note; into 99 Archive every doc must be archived. Restricted notes are listed, never read.",
			Args: []Arg{
				{Name: "from", Help: "The folder to move, from the vault root."},
				{Name: "to", Help: "Its new path from the vault root."},
				{Name: "--archive", Help: "Into the archive: set status: archived on every doc in the folder first instead of refusing."},
				pretty(prettyHelp),
			},
			Run: runMoveFolder},
		{Name: "log-entries", Effect: "vault_write",
			Summary: "List an append-only log note's entries (one top-level bullet each), or close one by appending ' → <resolution>' " +
				"to its line. The MCP log_entries tool: close needs --match to name exactly one open entry, and writes like a section edit.",
			Args: []Arg{
				{Name: "action", Help: "list or close.", Choices: []string{"list", "close"}},
				{Name: "doc_id", Help: "The log note's doc_id."},
				{Name: "--all", Help: "list: include closed entries.", Dest: "include_closed"},
				{Name: "--contains", Help: "list: only entries whose text contains this (case-insensitive).", TakesValue: true},
				{Name: "--limit", Help: "list: most entries to print, in file order (default 0: all).", TakesValue: true, Int: true, Default: "0"},
				{Name: "--match", Help: "close: text that exactly one open entry contains.", TakesValue: true},
				{Name: "--resolution", Help: "close: one line saying how the entry was resolved.", TakesValue: true},
				fieldsArg("Entry fields to print (line, text, closed, resolution); omit for every field."),
				pretty(prettyHelp),
			},
			Run: runLogEntries},
		{Name: "queue", Effect: "vault_write",
			Summary: "Add, list, set, claim or close work items in a project's queue (01 Projects/<project>/Queue.md, " +
				"or 07 Backlog/Queue.md), or reformat a queue's lines in the current grammar, " +
				"and print compact JSON; the MCP queue tool. Hooks call it: name the queue with --cwd, and " +
				"pass a value that starts with a dash as --flag=value. Exits 0 if ok, 1 on a refused call, 2 on a usage error.",
			Args: []Arg{
				{Name: "action", Help: "list, add, set, close or reformat.", Choices: core.QueueActions},
				{Name: "id", Optional: true, Help: "set, close: the item's id, e.g. example-3, or its alias."},
				{Name: "--project", Help: "The queue's project folder under 01 Projects, or backlog.", TakesValue: true},
				{Name: "--repo", Help: "A repository directory name, resolved through each project's index.md repos.", TakesValue: true},
				{Name: "--cwd", Help: "A working directory, resolved to its repository and then its project.", TakesValue: true},
				{Name: "--all", Help: "list, reformat: every queue."},
				{Name: "--agent", Help: "add: the worker's agent id. list: that agent's items. set, close: the item by agent; set with an id writes the agent onto that item.", TakesValue: true},
				{Name: "--origin", Help: "add: the repo (or working directory) of the session that sent the worker. list: items from it.", TakesValue: true},
				{Name: "--include-origin", Help: "list: also the delegated items in any queue whose origin is this repo."},
				{Name: "--kind", Help: "add: next, delegated, decision or follow-up. list: only this kind. set: delegated, for a next or follow-up item (the only change).", TakesValue: true},
				{Name: "--text", Help: "add: one line saying what the work is. set: replace it.", TakesValue: true},
				{Name: "--owner", Help: "add, set: who holds the item, one word ('' or - for nobody). list: that owner's items.", TakesValue: true},
				{Name: "--pr", Help: "Link: a PR number or URL ('' removes it on set).", TakesValue: true},
				{Name: "--branch", Help: "Link: a branch. list: the items with exactly this branch.", TakesValue: true},
				{Name: "--commit", Help: "Link: commits, comma-separated.", TakesValue: true},
				{Name: "--slug", Help: "Link: a content slug.", TakesValue: true},
				{Name: "--alias", Help: "add, set: an old id the item also answers to, such as a task doc_id ('' removes it on set). list: the item with it.", TakesValue: true},
				{Name: "--body", Help: "add, set: the doc_id of the doc holding the item's detail; it must exist ('' removes it on set).", TakesValue: true},
				{Name: "--blocked-by", Help: "add, set: item ids (or aliases) that must close first.", TakesValue: true, Multi: true},
				{Name: "--repos", Help: "add, set: the repos one worker's item spans (comma-separated or several values).", TakesValue: true, Multi: true},
				{Name: "--status", Help: "set: open, returned or parked. close: closed (default) or dropped. list: comma-separated statuses or all (default open,returned).", TakesValue: true},
				{Name: "--ready", Help: "list: open next and follow-up items whose blockers are all closed or dropped."},
				{Name: "--contains", Help: "list: items whose text contains this (case-insensitive).", TakesValue: true},
				{Name: "--limit", Help: "list: most items to print (default 0: all).", TakesValue: true, Int: true, Default: "0"},
				{Name: "--claim", Help: "set: take the item for --owner only if nobody holds it."},
				{Name: "--resolution", Help: "close: one line, e.g. repo@sha, a PR, a doc id, or why it was dropped.", TakesValue: true},
				{Name: "--drop-aliases", Help: "reformat: also remove every item's alias."},
				{Name: "--partial", Help: "close: record the --resolution repo@sha landings; the item closes when every repo in its repos has one."},
				fieldsArg("list: item fields to print (id, project, kind, status, owner, text, links, blocked_by, body, alias, agent, origin, repos, resolution); omit for every field."),
				pretty(prettyHelp),
			},
			Run: runQueue},
		{Name: "task-reconcile", Effect: "read",
			Summary: "Re-home tasks into tasks/ (live) or tasks/done/ (closed) per their status — the idempotent tidy sweep for statuses " +
				"edited by hand (a Base/Properties edit) that didn't move through task-move.",
			Args: []Arg{pretty(prettyHelp)},
			Run:  runTaskReconcile},
		{Name: "task-projects", Effect: "read",
			Summary: "The task-project universe: every 01 Projects/<project>/ folder a task can be filed in, with its open-task count. " +
				"The one owner of where a task can go — pickers (the Obsidian modal, the cockpit) derive from this instead of re-walking the vault layout.",
			Args: []Arg{pretty(prettyHelp)},
			Run:  runTaskProjects},
		{Name: "task-add", Effect: "read",
			Summary: "Author a new task file. With --project it colocates at 01 Projects/<project>/tasks/ and links related_projects; " +
				"without, it files a cross-cutting task in 07 Backlog/. Schema-validated, written through the one atomic serializer.",
			Args: []Arg{
				{Name: "title", Help: "Imperative task title."},
				{Name: "--project", Help: "Owning project (an 01 Projects/<project>/ folder).", TakesValue: true},
				{Name: "--related-projects", Help: "Projects a cross-cutting task touches.", TakesValue: true, Multi: true},
				{Name: "--effort", Help: "Effort S|M|L (default S).", TakesValue: true, Default: "S"},
				{Name: "--priority", Help: "Priority high|med|low (default med).", TakesValue: true, Default: "med"},
				{Name: "--tags", Help: "Tags (default: backlog).", TakesValue: true, Multi: true},
				pretty(prettyHelp),
			},
			Run: runTaskAdd},
		{Name: "task-delete", Effect: "read",
			Summary: "Permanently delete a task file (for mistakes / junk only — finished or abandoned work should be task-move'd to " +
				"done/wontfix, which keeps it queryable). Resolves a bare slug or the full id; refuses non-tasks.",
			Args: []Arg{{Name: "doc_id", Help: "Task id or slug (task-foo or foo)."}, pretty(prettyHelp)},
			Run:  runTaskDelete},
		{Name: "task-move", Effect: "read",
			Summary: "Move a task to a new status (open|active|parked|done|wontfix). Stamps closed: on done, clears it on reopen; " +
				"done tasks are kept so 'what shipped' stays a query. Resolves a bare slug or the full id.",
			Args: []Arg{
				{Name: "doc_id", Help: "Task id or slug (task-foo or foo)."},
				{Name: "status", Help: "Target status (open|active|parked|done|wontfix)."},
				pretty(prettyHelp),
			},
			Run: runTaskMove},
		{Name: "hq-manifest", Effect: "read",
			Summary: "Build the Severino HQ manifest with the package's shared frontmatter parser.",
			Args: []Arg{
				{Name: "vault", Help: "Vault root path."},
				{Name: "subdirs", Optional: true, Help: "Colon-separated vault subdirectories to index. Default: derived from the MCP config's " +
					"indexed_dirs plus the slim content dirs (05 Writeups, 06 Pages) — one list, one owner."},
				{Name: "--report", Help: "Print the full result (missing_frontmatter, duplicates, counts) as JSON instead of the manifest entries. Backs `hq doctor`."},
			},
			Run: runHQManifest},
		{Name: "brief", Effect: "read",
			Summary: "Doc-side vault state in one payload: recent changes, docs overdue for review, and inbox backlog. The vault leg of the `brief` shell tool.",
			Args: []Arg{
				{Name: "--days", Help: "Recent-changes look-back window in days (default 7).", TakesValue: true, Int: true, Default: "7"},
				{Name: "--review-after", Help: "Flag docs whose last_reviewed is older than N days (default 180).", TakesValue: true, Int: true, Default: "180"},
				{Name: "--limit", Help: "Max recent commits to return (default 15).", TakesValue: true, Int: true, Default: "15"},
				pretty("Indent the JSON output."),
			},
			Run: runBrief},
		{Name: "schema", Effect: "read",
			Summary: "Emit the canonical frontmatter schema (enum sets) as JSON. Severino HQ commits this output and validates against it " +
				"so the two systems share one definition.",
			Args: []Arg{
				{Name: "--json", Help: "Emit the schema as JSON (the default)."},
				{Name: "--contract", Help: "Emit the complete versioned profile contract, including task and per-document rules. The legacy default remains HQ-compatible."},
				{Name: "--fingerprint", Help: "Emit the stable SHA-256 fingerprint of the complete profile contract.", Dest: "schema_fingerprint"},
				{Name: "--json-schema", Help: "Emit a vault's frontmatter rules as a JSON Schema (draft 2020-12) for off-the-shelf validators " +
					"and editors, derived from the profile set_frontmatter and validate use."},
				{Name: "--vault", TakesValue: true, Default: "labs", Help: "With --json-schema: the vault whose profile to emit, labs (default) or edu."},
				{Name: "--check-doc", TakesValue: true, Metavar: "PATH",
					Help: "Instead of emitting, verify that a human schema doc's enum lines (doc_type/environment/status/sensitivity) match " +
						"the canonical schema. Exit 1 and print mismatches on drift."},
			},
			Run: runSchema},
		{Name: "daily-write", Effect: "vault_write",
			Summary: "Replace the generated brief region of a daily note (markdown on stdin): the marked MIRROR:daily-brief span only, " +
				"leaving the free-capture area untouched. Creates the note from the daily-note frontmatter contract if absent. The write behind tools' `vault daily`.",
			Args: []Arg{{Name: "--date", Help: "Daily note date (YYYY-MM-DD; default today).", TakesValue: true}, pretty(prettyHelp)},
			Run:  runDailyWrite},
		{Name: "export", Effect: "read",
			Summary: "Emit a vault's publishable dataset as JSON. `education`: institutions and courses with their `## Site` bullets, " +
				"read by jseverino.com and resume-engine.",
			Args: []Arg{{Name: "dataset", Help: "Dataset to emit.", Choices: []string{"education"}}, pretty(prettyHelp)},
			Run:  runExport},
		{Name: "validate", Effect: "read",
			Summary: "Check vault docs' frontmatter with the rules set_frontmatter writes by, without the MCP server: one " +
				"`<relative_path>: <problem>` line per problem, exit 1 on any. Paths outside the labs and edu vaults' indexed dirs " +
				"(read-only folders such as 00 Inbox, 05 Writeups and 06 Pages included), or not markdown, are ignored, so an editor or agent hook can pass every edited path.",
			Args: []Arg{
				{Name: "path", Multi: true, Help: "Doc path, absolute or relative to a vault root."},
				{Name: "--json", Help: `Print {"ok": ..., "problems": [{"relative_path", "problem"}]} instead of lines.`},
			},
			Run: runValidate},
		{Name: "describe", Effect: "read",
			Summary: "Emit this repo's command surface as structured JSON: every subcommand, its arguments, and help, generated from " +
				"the command table itself so it can't drift from --help. AI reads it; a TUI renders a command picker.",
			Args: []Arg{pretty(prettyHelp)},
			Run:  runDescribe},
		{Name: "serve", Effect: "read",
			Summary: "Serve MCP over stdio. The default with no subcommand.",
			Run:     runServe},
	}
}
