package cli

// Arg is one argument of a command. Flags start with "--".
type Arg struct {
	Name       string
	Help       string
	TakesValue bool     // flags only
	Multi      bool     // flag taking zero or more values (argparse nargs="*")
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

// commands is the command surface, in --help and describe order.
var commands []Command

func init() {
	commands = []Command{
		{Name: "doctor", Summary: "Validate configured vault frontmatter without starting the MCP server.", Effect: "read",
			Args: []Arg{{Name: "--propose", Help: "Print starter frontmatter for markdown files that are missing it."}},
			Run:  runDoctor},
		{Name: "update-doc-link", Summary: "Replace one exact Markdown link in an indexed document body.", Effect: "vault_write",
			Args: []Arg{{Name: "doc_id"}, {Name: "label"}, {Name: "expected_href"}, {Name: "replacement_href"}, pretty("")},
			Run:  runUpdateDocLink},
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
				pretty(prettyHelp),
			},
			Run: runFind},
		{Name: "read", Effect: "read",
			Summary: "Read one vault doc by doc_id and print JSON. With --section, return just that H2 span (the token-minimal path); " +
				"without it, the whole body. Honors the sensitivity gate — restricted bodies are withheld (no interactive unlock on the CLI path).",
			Args: []Arg{
				{Name: "doc_id", Help: "Stable doc_id, e.g. 'rb-add-nginx-proxy-host'."},
				{Name: "--section", Help: "Section slug or heading path from a `find` hit. Omit for the whole body.", TakesValue: true},
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
		{Name: "update-frontmatter", Effect: "read",
			Summary: "Update fields in an existing vault doc's frontmatter (the one writer — doc_id is immutable). Enum + relation fields " +
				"are validated against the schema. Used by the Obsidian relation editor so author-time edits can't dangle.",
			Args: []Arg{
				{Name: "relative_path", Help: "Vault-relative path to the doc."},
				{Name: "--title", TakesValue: true},
				{Name: "--doc-type", TakesValue: true},
				{Name: "--system", TakesValue: true},
				{Name: "--environment", TakesValue: true},
				{Name: "--status", TakesValue: true},
				{Name: "--sensitivity", TakesValue: true},
				{Name: "--set-related-projects", Help: "Replace related_projects (empty clears).", TakesValue: true, Multi: true},
				{Name: "--set-related-assets", Help: "Replace related_assets (empty clears).", TakesValue: true, Multi: true},
				{Name: "--set-tags", Help: "Replace tags (empty clears).", TakesValue: true, Multi: true},
				{Name: "--touch-last-reviewed", Help: "Set last_reviewed to today."},
				pretty(prettyHelp),
			},
			Run: runUpdateFrontmatter},
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
		{Name: "describe", Effect: "read",
			Summary: "Emit this repo's command surface as structured JSON: every subcommand, its arguments, and help, generated from " +
				"the command table itself so it can't drift from --help. AI reads it; a TUI renders a command picker.",
			Args: []Arg{pretty(prettyHelp)},
			Run:  runDescribe},
		{Name: "unlock-hash", Effect: "read",
			Summary: "Read an unlock phrase twice from the terminal without echo and print its argon2id PHC string, " +
				"for the Keychain item or SVMC_RESTRICTED_UNLOCK_HASH_FILE. Refuses when stdin isn't a terminal.",
			Run: runUnlockHash},
		{Name: "serve", Effect: "read",
			Summary: "Serve MCP over stdio. The default with no subcommand.",
			Run:     runServe},
	}
}
