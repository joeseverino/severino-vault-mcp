// Package cli is the command-line face: one command table drives argument
// parsing, --help, and the cordon describe contract, so the three can't
// drift. Every subcommand runs one governed call against the labs vault (or
// the named dataset) and prints JSON; no subcommand serves MCP.
package cli

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	vaultmcp "github.com/joeseverino/severino-vault-mcp"
	"github.com/joeseverino/severino-vault-mcp/internal/brief"
	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/daily"
	"github.com/joeseverino/severino-vault-mcp/internal/doctor"
	"github.com/joeseverino/severino-vault-mcp/internal/education"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/hqmanifest"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/mcpserver"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/query"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/vaults"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
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
			names = append(names, pystr.Repr(cmd.Name))
		}
		return usageError(c, toolName, mainUsage(), fmt.Sprintf("argument command: invalid choice: %s (choose from %s)",
			pystr.Repr(name), strings.Join(names, ", ")))
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
					return nil, false, fmt.Sprintf("argument %s: invalid int value: %s", arg.Name, pystr.Repr(value))
				}
				out[arg.dest()] = n
			} else {
				out[arg.dest()] = value
			}
		default:
			if marker == "=" {
				return nil, false, fmt.Sprintf("argument %s: ignored explicit argument %s", arg.Name, pystr.Repr(value))
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
		if pi < len(positionals) {
			v := positionals[pi]
			pi++
			if len(a.Choices) > 0 && !slices.Contains(a.Choices, v) {
				var quoted []string
				for _, ch := range a.Choices {
					quoted = append(quoted, pystr.Repr(ch))
				}
				return nil, false, fmt.Sprintf("argument %s: invalid choice: %s (choose from %s)", a.Name, pystr.Repr(v), strings.Join(quoted, ", "))
			}
			out[a.dest()] = v
		} else if !a.Optional {
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

func runDoctor(c *Ctx, p Parsed) int {
	return doctor.Run(c.Stdout, config.Load("", c.Env), schema.Labs, p.Bool("propose"))
}

func runUpdateDocLink(c *Ctx, p Parsed) int {
	return c.emit(write.UpdateLink(c.labs(), p.Str("doc_id"), p.Str("label"), p.Str("expected_href"), p.Str("replacement_href")), p.Bool("pretty"))
}

func runTouchReviewed(c *Ctx, p Parsed) int {
	return c.emit(write.TouchReviewed(c.labs(), p.Str("relative_path")), p.Bool("pretty"))
}

func runBackfillAliases(c *Ctx, p Parsed) int {
	return c.emit(write.BackfillAliases(c.labs()), p.Bool("pretty"))
}

func runFind(c *Ctx, p Parsed) int {
	return c.emit(jsonx.New("ok", true).Merge(query.FindSections(c.labs(), p.Str("query"), p.Int("limit"))), p.Bool("pretty"))
}

func runRead(c *Ctx, p Parsed) int {
	return c.emit(query.ReadSection(c.labs(), p.Str("doc_id"), p.Str("section")), p.Bool("pretty"))
}

func runTaskList(c *Ctx, p Parsed) int {
	return c.emit(tasks.List(c.labs(), tasks.Filter{
		Status: p.Str("status"), Project: p.Str("project"), StaleOnly: p.Bool("stale_only"),
		IncludeAll: p.Bool("include_all"), StaleDays: p.Int("stale_days"),
	}), p.Bool("pretty"))
}

func runPromoteNote(c *Ctx, p Parsed) int {
	if !p.Has("title") {
		return usageError(c, toolName+" promote-note", commandUsage(commands[slices.IndexFunc(commands, func(cmd Command) bool { return cmd.Name == "promote-note" })]),
			"the following arguments are required: --title")
	}
	return c.emit(tasks.Promote(c.labs(), p.Str("source"), tasks.New{
		Title: p.Str("title"), Project: p.Str("project"), Effort: p.Str("effort"), Priority: p.Str("priority"),
	}), p.Bool("pretty"))
}

func optStr(p Parsed, k string) *string {
	if !p.Has(k) {
		return nil
	}
	v := p.Str(k)
	return &v
}

func runUpdateFrontmatter(c *Ctx, p Parsed) int {
	listOp := func(k string) write.ListOp {
		if !p.Has(k) {
			return write.ListOp{}
		}
		return write.ListOp{Set: p.List(k), HasSet: true}
	}
	return c.emit(write.UpdateFrontmatter(c.labs(), p.Str("relative_path"), write.Update{
		TouchLastReviewed: p.Bool("touch_last_reviewed"),
		Title:             optStr(p, "title"), DocType: optStr(p, "doc_type"), System: optStr(p, "system"),
		Environment: optStr(p, "environment"), Status: optStr(p, "status"), Sensitivity: optStr(p, "sensitivity"),
		Tags: listOp("set_tags"), RelatedProjects: listOp("set_related_projects"), RelatedAssets: listOp("set_related_assets"),
	}, schema.Labs), p.Bool("pretty"))
}

func runTaskReconcile(c *Ctx, p Parsed) int {
	return c.emit(tasks.Reconcile(c.labs()), p.Bool("pretty"))
}

func runTaskProjects(c *Ctx, p Parsed) int { return c.emit(tasks.Projects(c.labs()), p.Bool("pretty")) }

func runTaskAdd(c *Ctx, p Parsed) int {
	return c.emit(tasks.Add(c.labs(), tasks.New{
		Title: p.Str("title"), Project: p.Str("project"), RelatedProjects: p.List("related_projects"),
		Effort: p.Str("effort"), Priority: p.Str("priority"), Tags: p.List("tags"),
	}, tasks.Sections{}), p.Bool("pretty"))
}

func runTaskMove(c *Ctx, p Parsed) int {
	return c.emit(tasks.SetStatus(c.labs(), p.Str("doc_id"), p.Str("status")), p.Bool("pretty"))
}

func runTaskDelete(c *Ctx, p Parsed) int {
	return c.emit(tasks.Delete(c.labs(), p.Str("doc_id")), p.Bool("pretty"))
}

func runDescribe(c *Ctx, p Parsed) int { return c.emit(Describe(), p.Bool("pretty")) }

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
	case p.Bool("schema_fingerprint"):
		fmt.Fprintln(c.Stdout, schema.Labs.Fingerprint())
	case p.Bool("contract"):
		fmt.Fprintln(c.Stdout, jsonx.Canonical(schema.Labs.ContractDict()))
	default:
		fmt.Fprintln(c.Stdout, jsonx.Canonical(schema.Labs.AsDict()))
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
		fmt.Fprintf(c.Stderr, "warn: %d file(s) missing frontmatter (skipped) — run `hq doctor` to list them\n", len(missing.([]string)))
	}
	entries, _ := result.Get("entries")
	fmt.Fprintln(c.Stdout, jsonx.Pretty(entries))
	count, _ := result.Get("count")
	fmt.Fprintf(c.Stderr, "ok: %d entries\n", count)
	return 0
}

func runBrief(c *Ctx, p Parsed) int {
	return c.emit(brief.Vault(c.labs(), p.Int("days"), p.Int("review_after"), p.Int("limit")), p.Bool("pretty"))
}

func runDailyWrite(c *Ctx, p Parsed) int {
	content, err := io.ReadAll(c.Stdin)
	if err != nil {
		return c.emit(jsonx.New("ok", false, "error", "could not read stdin: "+err.Error()), p.Bool("pretty"))
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

// Terminal seams; tests replace them.
var (
	isTerminal   = term.IsTerminal
	readPassword = term.ReadPassword
)

func runUnlockHash(c *Ctx, _ Parsed) int {
	f, ok := c.Stdin.(*os.File)
	if !ok || !isTerminal(int(f.Fd())) {
		fmt.Fprintf(c.Stderr, "%s unlock-hash: stdin is not a terminal; run it interactively\n", toolName)
		return 2
	}
	read := func(prompt string) (string, error) {
		fmt.Fprint(c.Stderr, prompt)
		b, err := readPassword(int(f.Fd()))
		fmt.Fprintln(c.Stderr)
		return string(b), err
	}
	first, err := read("Unlock phrase: ")
	if err != nil {
		fmt.Fprintf(c.Stderr, "%s unlock-hash: %v\n", toolName, err)
		return 1
	}
	if first == "" {
		fmt.Fprintf(c.Stderr, "%s unlock-hash: empty phrase\n", toolName)
		return 1
	}
	second, err := read("Again: ")
	if err != nil {
		fmt.Fprintf(c.Stderr, "%s unlock-hash: %v\n", toolName, err)
		return 1
	}
	if subtle.ConstantTimeCompare([]byte(first), []byte(second)) != 1 {
		fmt.Fprintf(c.Stderr, "%s unlock-hash: the phrases don't match\n", toolName)
		return 1
	}
	hash, err := gate.HashPhrase(first, gate.DefaultParams)
	if err != nil {
		fmt.Fprintf(c.Stderr, "%s unlock-hash: %v\n", toolName, err)
		return 1
	}
	fmt.Fprintln(c.Stdout, hash)
	return 0
}
