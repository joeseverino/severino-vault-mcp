// Package cli is the command-line face: one command table drives argument
// parsing, --help, and the cordon describe contract, so the three can't
// drift. Every subcommand runs one governed call against the labs vault (or
// the named dataset) and prints JSON; no subcommand serves MCP.
package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	vaultmcp "github.com/joeseverino/severino-vault-mcp/v3"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/brief"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/daily"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/doctor"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/education"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/hqmanifest"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/mcpserver"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/queue"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vaults"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// Ctx carries a run's IO and environment.
type Ctx struct {
	Env    config.Env
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Parsed holds a command's values by dest.
type Parsed map[string]any

// Str returns a string value, or "".
func (p Parsed) Str(k string) string { s, _ := p[k].(string); return s }

// Has reports whether a value was given.
func (p Parsed) Has(k string) bool { _, ok := p[k]; return ok }

// Bool returns a flag.
func (p Parsed) Bool(k string) bool { b, _ := p[k].(bool); return b }

// Int returns an integer value.
func (p Parsed) Int(k string) int { n, _ := p[k].(int); return n }

// List returns a multi value; nil when not given.
func (p Parsed) List(k string) []string { l, _ := p[k].([]string); return l }

// Main runs the CLI and returns the exit code.
func Main(args []string, c *Ctx) int {
	if c.Env == nil {
		c.Env = config.OSEnv()
	}
	i := 0
	fingerprint := false
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch a := args[i]; {
		case a == "-h" || a == "--help":
			fmt.Fprint(c.Stdout, mainHelp())
			return 0
		case a == "--":
			i++
			goto dispatch
		case matchesLong(a, "--fingerprint"):
			fingerprint = true
		default:
			return usageError(c, toolName, mainUsage(), "unrecognized arguments: "+strings.Join(args[i:], " "))
		}
		i++
	}
dispatch:
	if fingerprint {
		fmt.Fprintln(c.Stdout, vaultmcp.Fingerprint())
		return 0
	}
	if i >= len(args) {
		return runServe(c, Parsed{})
	}
	name := args[i]
	idx := slices.IndexFunc(commands, func(cmd Command) bool { return cmd.Name == name })
	if idx < 0 {
		var names []string
		for _, cmd := range commands {
			names = append(names, cmd.Name)
		}
		return usageError(c, toolName, mainUsage(), fmt.Sprintf("argument command: invalid choice: %q (choose from %s)",
			name, pystr.Quoted(names)))
	}
	cmd := commands[idx]
	parsed, help, err := parse(cmd, args[i+1:])
	if help {
		fmt.Fprint(c.Stdout, commandHelp(cmd))
		return 0
	}
	if err != "" {
		return usageError(c, toolName+" "+cmd.Name, commandUsage(cmd), err)
	}
	return cmd.Run(c, parsed)
}

func peek(args []string, i int) (string, bool) {
	if i+1 < len(args) {
		return args[i+1], true
	}
	return "", false
}

// matchesLong reports whether token is long option opt or a prefix of it.
func matchesLong(token, opt string) bool {
	name, _, _ := strings.Cut(token, "=")
	return len(name) > 2 && strings.HasPrefix(opt, name)
}

func usageError(c *Ctx, prog, usage, msg string) int {
	fmt.Fprint(c.Stderr, usage)
	fmt.Fprintf(c.Stderr, "%s: error: %s\n", prog, msg)
	return 2
}

func resolveFlag(cmd Command, token string) (*Arg, string, string) {
	name, value, hasValue := strings.Cut(token, "=")
	var exact, prefixed []*Arg
	for i := range cmd.Args {
		a := &cmd.Args[i]
		if a.positional() {
			continue
		}
		if a.Name == name {
			exact = append(exact, a)
		} else if strings.HasPrefix(a.Name, name) {
			prefixed = append(prefixed, a)
		}
	}
	marker := ""
	if hasValue {
		marker = "="
	}
	switch {
	case len(exact) == 1:
		return exact[0], value, marker
	case len(prefixed) == 1:
		return prefixed[0], value, marker
	case len(prefixed) > 1:
		var names []string
		for _, a := range prefixed {
			names = append(names, a.Name)
		}
		return nil, "ambiguous option: " + name + " could match " + strings.Join(names, ", "), ""
	}
	return nil, "", ""
}

func parse(cmd Command, args []string) (Parsed, bool, string) {
	out := Parsed{}
	var positionals []string
	var unknown []string
	onlyPositional := false
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if onlyPositional || !strings.HasPrefix(tok, "-") || tok == "-" {
			positionals = append(positionals, tok)
			continue
		}
		if tok == "--" {
			onlyPositional = true
			continue
		}
		if tok == "-h" || tok == "--help" {
			return nil, true, ""
		}
		arg, value, marker := resolveFlag(cmd, tok)
		if arg == nil {
			if value != "" {
				return nil, false, value
			}
			unknown = append(unknown, tok)
			continue
		}
		switch {
		case arg.Multi:
			vals := []string{}
			if marker == "=" {
				vals = append(vals, value)
			}
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				vals = append(vals, args[i])
			}
			out[arg.dest()] = vals
		case arg.TakesValue:
			if marker != "=" {
				next, ok := peek(args, i)
				if !ok || (strings.HasPrefix(next, "-") && next != "-") {
					return nil, false, "argument " + arg.Name + ": expected one argument"
				}
				i++
				value = next
			}
			if arg.Int {
				n, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					return nil, false, fmt.Sprintf("argument %s: invalid int value: %q", arg.Name, value)
				}
				out[arg.dest()] = n
			} else {
				out[arg.dest()] = value
			}
		default:
			if marker == "=" {
				return nil, false, fmt.Sprintf("argument %s: ignored explicit argument %q", arg.Name, value)
			}
			out[arg.dest()] = true
		}
	}
	var missing []string
	pi := 0
	for _, a := range cmd.Args {
		if !a.positional() {
			if _, set := out[a.dest()]; !set {
				switch {
				case a.Multi:
				case a.TakesValue && a.Default != "":
					if a.Int {
						n, _ := strconv.Atoi(a.Default)
						out[a.dest()] = n
					} else {
						out[a.dest()] = a.Default
					}
				case !a.TakesValue:
					out[a.dest()] = false
				}
			}
			continue
		}
		switch {
		case a.Multi && pi < len(positionals):
			out[a.dest()] = slices.Clone(positionals[pi:])
			pi = len(positionals)
		case pi < len(positionals):
			v := positionals[pi]
			pi++
			if len(a.Choices) > 0 && !slices.Contains(a.Choices, v) {
				return nil, false, fmt.Sprintf("argument %s: invalid choice: %q (choose from %s)", a.Name, v, pystr.Quoted(a.Choices))
			}
			out[a.dest()] = v
		case !a.Optional:
			missing = append(missing, a.Name)
		}
	}
	if len(missing) > 0 {
		return nil, false, "the following arguments are required: " + strings.Join(missing, ", ")
	}
	unknown = append(unknown, positionals[pi:]...)
	if len(unknown) > 0 {
		return nil, false, "unrecognized arguments: " + strings.Join(unknown, " ")
	}
	return out, false, ""
}

func argUsage(a Arg) string {
	if a.positional() {
		if a.Optional {
			return "[" + a.Name + "]"
		}
		if a.Multi {
			return a.Name + " [" + a.Name + " ...]"
		}
		if len(a.Choices) > 0 {
			return "{" + strings.Join(a.Choices, ",") + "}"
		}
		return a.Name
	}
	meta := a.Metavar
	if meta == "" {
		meta = strings.ToUpper(a.dest())
	}
	switch {
	case a.Multi:
		return "[" + a.Name + " [" + meta + " ...]]"
	case a.TakesValue:
		return "[" + a.Name + " " + meta + "]"
	}
	return "[" + a.Name + "]"
}

func commandUsage(cmd Command) string {
	parts := []string{"usage: " + toolName + " " + cmd.Name, "[-h]"}
	var pos []string
	for _, a := range cmd.Args {
		if a.positional() {
			pos = append(pos, argUsage(a))
		} else {
			parts = append(parts, argUsage(a))
		}
	}
	return strings.Join(append(parts, pos...), " ") + "\n"
}

func commandHelp(cmd Command) string {
	var sb strings.Builder
	sb.WriteString(commandUsage(cmd))
	if cmd.Summary != "" {
		sb.WriteString("\n" + cmd.Summary + "\n")
	}
	var pos, opt []Arg
	for _, a := range cmd.Args {
		if a.positional() {
			pos = append(pos, a)
		} else {
			opt = append(opt, a)
		}
	}
	if len(pos) > 0 {
		sb.WriteString("\npositional arguments:\n")
		for _, a := range pos {
			fmt.Fprintf(&sb, "  %-24s %s\n", a.Name, a.Help)
		}
	}
	sb.WriteString("\noptions:\n")
	fmt.Fprintf(&sb, "  %-24s %s\n", "-h, --help", "show this help message and exit")
	for _, a := range opt {
		fmt.Fprintf(&sb, "  %-24s %s\n", strings.Trim(argUsage(a), "[]"), a.Help)
	}
	return sb.String()
}

func mainUsage() string {
	var names []string
	for _, cmd := range commands {
		names = append(names, cmd.Name)
	}
	return "usage: " + toolName + " [-h] [--fingerprint] {" + strings.Join(names, ",") + "} ...\n"
}

func mainHelp() string {
	var sb strings.Builder
	sb.WriteString(mainUsage())
	sb.WriteString("\n" + toolDesc + "\n\npositional arguments:\n")
	for _, cmd := range commands {
		fmt.Fprintf(&sb, "  %-20s %s\n", cmd.Name, firstSentence(cmd.Summary))
	}
	sb.WriteString("\noptions:\n")
	fmt.Fprintf(&sb, "  %-20s %s\n", "-h, --help", "show this help message and exit")
	fmt.Fprintf(&sb, "  %-20s %s\n", "--fingerprint", fingerprintHelp)
	return sb.String()
}

func firstSentence(s string) string {
	if head, _, ok := strings.Cut(s, ". "); ok {
		return head + "."
	}
	return s
}

// Describe is the cordon v4 command-surface document.
func Describe() *jsonx.Obj {
	cmds := []*jsonx.Obj{}
	for _, cmd := range commands {
		args := []*jsonx.Obj{}
		for _, a := range cmd.Args {
			args = append(args, describeArg(a))
		}
		cmds = append(cmds, jsonx.New("name", cmd.Name, "summary", cmd.Summary, "args", args,
			"effect", cmd.Effect, "paras", []any{}, "examples", []any{}))
	}
	return jsonx.New(
		"ok", true, "schema_version", 4, "name", toolName, "description", toolDesc,
		"group", "Vault MCP", "order", 1, "effect", "read",
		"global_options", []*jsonx.Obj{describeArg(Arg{Name: "--fingerprint", Help: fingerprintHelp})},
		"positionals", []any{}, "paras", []any{}, "examples", []any{},
		"commands", cmds,
	)
}

func describeArg(a Arg) *jsonx.Obj {
	if a.positional() {
		o := jsonx.New("name", a.Name, "positional", true, "required", !a.Optional, "help", a.Help)
		if a.Multi {
			o.Set("variadic", true)
		}
		if len(a.Choices) > 0 {
			o.Set("choices", slices.Clone(a.Choices))
		}
		return o
	}
	o := jsonx.New("name", a.Name, "positional", false, "required", false, "help", a.Help,
		"flags", []string{a.Name}, "takes_value", a.TakesValue)
	if a.Metavar != "" {
		o.Set("metavar", a.Metavar)
	}
	return o
}

// ----- runners ----------------------------------------------------------------

func (c *Ctx) emit(result *jsonx.Obj, pretty bool) int {
	fmt.Fprintln(c.Stdout, jsonx.Dumps(result, pretty))
	if pystr.Truthy(func() any { v, _ := result.Get("ok"); return v }()) {
		return 0
	}
	return 1
}

func (c *Ctx) labs() *vault.Loader { return vault.NewLoader(config.Load("", c.Env)) }

// readLabs runs f on the labs loader, or emits the unreadable-vault failure
// when this process cannot read the vault.
func (c *Ctx) readLabs(p Parsed, f func(l *vault.Loader) *jsonx.Obj) int {
	l := c.labs()
	if fail := core.Unreadable("labs", l); fail != nil {
		return c.emit(fail, p.Bool("pretty"))
	}
	return c.emit(f(l), p.Bool("pretty"))
}

func runDoctor(c *Ctx, p Parsed) int {
	cfg := config.Load("", c.Env)
	if fail := core.Unreadable("labs", vault.NewLoader(cfg)); fail != nil {
		fmt.Fprintln(c.Stdout, jsonx.Dumps(fail, false))
		return 1
	}
	return doctor.Run(c.Stdout, cfg, schema.Labs, p.Bool("propose"))
}

func runTouchReviewed(c *Ctx, p Parsed) int {
	return c.emit(core.TouchReviewed(c.labsVault(), p.Str("doc_id")), p.Bool("pretty"))
}

func runBackfillAliases(c *Ctx, p Parsed) int {
	return c.readLabs(p, write.BackfillAliases)
}

// labsVault is the labs vault as the MCP tools see it, for the commands that
// share their core function.
func (c *Ctx) labsVault() *core.Vault {
	return core.NewVault("labs", config.Load("", c.Env), schema.Labs)
}

// view is the CLI's field mask: the whole result unless --fields narrows it,
// since scripts read these commands' JSON.
func view(p Parsed) core.View { return core.View{Fields: p.List("fields"), Full: true} }

// okUnlessRefused stamps ok on a result that only says so when it fails.
func okUnlessRefused(o *jsonx.Obj, ok bool) *jsonx.Obj {
	if o.Has("ok") {
		return o
	}
	return jsonx.New("ok", ok).Merge(o)
}

func runFind(c *Ctx, p Parsed) int {
	r := core.Find(c.labsVault(), p.Str("query"), core.FindArgs{By: core.FindModes[0], Limit: p.Int("limit"),
		IncludeArchived: p.Bool("include_archived"), View: view(p)})
	return c.emit(okUnlessRefused(r, true), p.Bool("pretty"))
}

func runRead(c *Ctx, p Parsed) int {
	r := core.ReadDoc(c.labsVault(), p.Str("doc_id"), core.ReadArgs{Sections: p.List("sections"), View: view(p)})
	return c.emit(okUnlessRefused(r, r.Bool("found") && !r.Has("section_error")), p.Bool("pretty"))
}

func optStr(p Parsed, k string) *string {
	if !p.Has(k) {
		return nil
	}
	v := p.Str(k)
	return &v
}

func runUpdateFrontmatter(c *Ctx, p Parsed) int {
	return c.emit(core.SetFrontmatter(c.labsVault(), p.Str("relative_path"), changesFrom(p, updateArgs)), p.Bool("pretty"))
}

func runWriteDoc(c *Ctx, p Parsed) int {
	var content []byte
	if m := p.Str("mode"); m != "delete_section" && m != "replace_text" {
		var err error
		if content, err = io.ReadAll(c.Stdin); err != nil {
			return c.emit(jsonx.Fail("could not read stdin: "+err.Error()), p.Bool("pretty"))
		}
	}
	return c.emit(core.WriteDoc(vaults.Labs(c.Env), core.WriteDocArgs{
		Mode: p.Str("mode"), RelativePath: p.Str("relative_path"), DocID: p.Str("doc_id"), Heading: optStr(p, "heading"),
		Body: pystr.Decode(content), OldText: p.Str("old_text"), NewText: optStr(p, "new_text"), New: changesFrom(p, createArgs),
	}), p.Bool("pretty"))
}

func runMoveDoc(c *Ctx, p Parsed) int {
	return c.emit(core.MoveDoc(c.labsVault(), core.MoveArgs{
		DocID: p.Str("doc_id"), RelativePath: p.Str("relative_path"), To: p.Str("to"), WithFolder: p.Bool("with_folder"),
		Archive: p.Bool("archive"),
	}), p.Bool("pretty"))
}

func runMoveFolder(c *Ctx, p Parsed) int {
	return c.emit(core.MoveFolder(c.labsVault(), core.FolderArgs{
		From: p.Str("from"), To: p.Str("to"), Archive: p.Bool("archive"),
	}), p.Bool("pretty"))
}

func runLogEntries(c *Ctx, p Parsed) int {
	return c.emit(core.LogEntries(c.labsVault(), p.Str("doc_id"), core.LogArgs{
		Action: p.Str("action"), OpenOnly: !p.Bool("include_closed"), Contains: p.Str("contains"), Limit: p.Int("limit"),
		Match: p.Str("match"), Resolution: p.Str("resolution"), Reopen: p.Bool("reopen"), View: view(p),
	}), p.Bool("pretty"))
}

func runQueue(c *Ctx, p Parsed) int {
	q := core.QueueArgs{
		Action: p.Str("action"), ID: p.Str("id"), Project: p.Str("project"), Repo: p.Str("repo"), Cwd: p.Str("cwd"),
		All: p.Bool("all"), Agent: p.Str("agent"), Origin: p.Str("origin"), IncludeOrigin: p.Bool("include_origin"), Kind: p.Str("kind"), Text: optStr(p, "text"), Owner: optStr(p, "owner"),
		Body:      optStr(p, "body"),
		BlockedBy: p.List("blocked_by"), HasBlockedBy: p.Has("blocked_by"), Repos: p.List("repos"), HasRepos: p.Has("repos"),
		Base: optStr(p, "base"), Transcript: optStr(p, "transcript"), Owns: p.List("owns"), HasOwns: p.Has("owns"),
		Partial: p.Bool("partial"), Status: p.Str("status"), Ready: p.Bool("ready"),
		Contains: p.Str("contains"), Limit: p.Int("limit"), Claim: p.Bool("claim"), Release: p.Bool("release"), Resolution: p.Str("resolution"),
		View: view(p),
	}
	if p.Has("items") {
		q.Items = []core.QueueClose{}
		for _, v := range p.List("items") {
			id, resolution, _ := strings.Cut(v, "=")
			q.Items = append(q.Items, core.QueueClose{ID: id, Resolution: resolution})
		}
	}
	for _, k := range queue.LinkKeys {
		if p.Has(k) {
			if q.Links == nil {
				q.Links = map[string]string{}
			}
			q.Links[k] = p.Str(k)
		}
	}
	return c.emit(core.Queue(c.labsVault(), q), p.Bool("pretty"))
}

func runDescribe(c *Ctx, p Parsed) int { return c.emit(Describe(), p.Bool("pretty")) }

// inVault reports whether a validate path may be a doc of the vault at root: a
// vault-relative path always may; an absolute one must lie under it.
func inVault(path, root string) bool {
	return !filepath.IsAbs(path) || config.Under(path, strings.TrimRight(root, "/"))
}

// runValidate checks each path against the first local vault that holds it.
// A hook runs it on every edited file, so a path no vault holds is not an error.
func runValidate(c *Ctx, p Parsed) int {
	local := vaults.Local(c.Env, io.Discard)
	problems := []*jsonx.Obj{}
	for _, path := range p.List("path") {
		for _, v := range local {
			if fail := v.Unreadable(); fail != nil && inVault(config.ExpandUser(path), v.Config.VaultPath) {
				return c.emit(fail, p.Bool("json"))
			}
			rel, found, ok := core.CheckDoc(v, config.ExpandUser(path))
			if !ok {
				continue
			}
			for _, problem := range found {
				problems = append(problems, jsonx.New("relative_path", rel, "problem", problem))
			}
			break
		}
	}
	if p.Bool("json") {
		return c.emit(jsonx.New("ok", len(problems) == 0, "problems", problems), false)
	}
	for _, o := range problems {
		fmt.Fprintf(c.Stdout, "%s: %s\n", o.Str("relative_path"), o.Str("problem"))
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

func runSchema(c *Ctx, p Parsed) int {
	if path := p.Str("check_doc"); path != "" {
		text, err := pystr.ReadText(config.ExpandUser(path))
		if err != nil {
			fmt.Fprintf(c.Stderr, "%s: cannot read %s: %v\n", toolName, path, err)
			return 1
		}
		if mismatches := schema.Labs.CheckDocEnums(text); len(mismatches) > 0 {
			fmt.Fprintf(c.Stderr, "schema doc drift in %s:\n", path)
			for _, m := range mismatches {
				fmt.Fprintf(c.Stderr, "  - %s\n", m)
			}
			return 1
		}
		fmt.Fprintf(c.Stdout, "ok: %s matches the canonical schema\n", path)
		return 0
	}
	switch {
	case p.Bool("json_schema"):
		profile, ok := schema.Builtin[p.Str("vault")]
		if !ok {
			fmt.Fprintf(c.Stderr, "%s: %s\n", toolName, schema.NotIn("--vault", p.Str("vault"), slices.Sorted(maps.Keys(schema.Builtin))))
			return 2
		}
		fmt.Fprintln(c.Stdout, jsonx.Pretty(profile.JSONSchema()))
	case p.Bool("schema_fingerprint"):
		fmt.Fprintln(c.Stdout, schema.Labs.Fingerprint())
	case p.Bool("contract"):
		fmt.Fprintln(c.Stdout, jsonx.Pretty(jsonx.Sorted(schema.Labs.ContractDict())))
	default:
		fmt.Fprintln(c.Stdout, jsonx.Pretty(jsonx.Sorted(schema.Labs.AsDict())))
	}
	return 0
}

func runHQManifest(c *Ctx, p Parsed) int {
	var subdirs []string
	if s := p.Str("subdirs"); s != "" {
		subdirs = config.SplitColon(s)
	} else {
		subdirs = hqmanifest.DefaultDirs(config.Load("", c.Env).IndexedDirs)
	}
	result := hqmanifest.Build(config.ExpandUser(p.Str("vault")), subdirs)
	ok := result.Bool("ok")
	if p.Bool("report") {
		fmt.Fprintln(c.Stdout, jsonx.Pretty(result))
		if ok {
			return 0
		}
		return 1
	}
	if !ok {
		fmt.Fprintln(c.Stderr, jsonx.Pretty(result))
		return 1
	}
	missingDirs, _ := result.Get("missing_dirs")
	for _, d := range missingDirs.([]string) {
		fmt.Fprintf(c.Stderr, "warn: %s not under vault, skipping\n", d)
	}
	if missing, _ := result.Get("missing_frontmatter"); len(missing.([]string)) > 0 {
		fmt.Fprintf(c.Stderr, "warn: %d file(s) missing frontmatter (skipped); run `hq doctor` to list them\n", len(missing.([]string)))
	}
	entries, _ := result.Get("entries")
	fmt.Fprintln(c.Stdout, jsonx.Pretty(entries))
	count, _ := result.Get("count")
	fmt.Fprintf(c.Stderr, "ok: %d entries\n", count)
	return 0
}

func runBrief(c *Ctx, p Parsed) int {
	return c.readLabs(p, func(l *vault.Loader) *jsonx.Obj {
		return brief.Vault(l, p.Int("days"), p.Int("review_after"), p.Int("limit"))
	})
}

func runDailyWrite(c *Ctx, p Parsed) int {
	content, err := io.ReadAll(c.Stdin)
	if err != nil {
		return c.emit(jsonx.Fail("could not read stdin: "+err.Error()), p.Bool("pretty"))
	}
	return c.emit(daily.Write(config.Load("", c.Env), pystr.Decode(content), p.Str("date")), p.Bool("pretty"))
}

func runExport(c *Ctx, p Parsed) int {
	edu := vaults.Edu(c.Env)
	return c.emit(education.Dataset(edu.Loader, edu.Profile.Statuses), p.Bool("pretty"))
}

func runServe(c *Ctx, _ Parsed) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpserver.Serve(ctx, c.Env, c.Stderr); err != nil && ctx.Err() == nil {
		fmt.Fprintf(c.Stderr, "%s: %v\n", toolName, err)
		return 1
	}
	return 0
}
