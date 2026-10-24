// Package mcpserver is the MCP face: one server for every vault. Shared tools
// register once with a `vault` argument; each provider's tools are
// re-exported as they are.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/queue"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	"github.com/joeseverino/severino-vault-mcp/internal/vaults"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// Instructions is the server's guidance to the model.
const Instructions = `Joe's Obsidian vaults: labs (homelab infrastructure, runbooks, projects),
edu (Georgia Tech coursework and certifications), and any vault a provider
adds below. Every shared tool takes ` + "`vault`" + ` (default labs).
Load the vault skill for how to use these tools.
`

// Version is the module version from build info.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// Serve composes the vaults and serves MCP over stdio until ctx ends.
func Serve(ctx context.Context, env config.Env, log io.Writer) error {
	reg := vaults.Build(ctx, env, log)
	defer reg.Close()
	return New(reg, log).Run(ctx, &mcp.StdioTransport{})
}

// New builds the server for a registry.
func New(reg *vaults.Registry, log io.Writer) *mcp.Server {
	instructions := strings.Join(append([]string{Instructions}, reg.Instructions...), "\n")
	s := mcp.NewServer(&mcp.Implementation{Name: "severino-vault-mcp", Version: Version()},
		&mcp.ServerOptions{Instructions: instructions, Capabilities: &mcp.ServerCapabilities{}})
	h := &handlers{reg: reg, bin: watchBinary()}
	h.registerCore(s)
	taken := map[string]bool{}
	for _, name := range coreToolNames {
		taken[name] = true
	}
	for _, p := range reg.Providers {
		for _, tool := range p.Tools {
			if taken[tool.Name] {
				fmt.Fprintf(log, "severino-vault-mcp: provider %s tool %s skipped; the name is taken\n", p.Vault(), tool.Name)
				continue
			}
			taken[tool.Name] = true
			p := p
			t := *tool
			addTool(s, &t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return p.Call(ctx, t.Name, req.Params.Arguments)
			})
		}
	}
	return s
}

var coreToolNames = []string{"find", "read_doc", "set_frontmatter", "write_doc", "move_doc", "move_folder", "task_board", "task_write", "log_entries", "queue"}

// ----- schemas ------------------------------------------------------------------

func object(props *jsonx.Obj, required []string) json.RawMessage {
	o := jsonx.New("type", "object", "properties", props, "additionalProperties", false)
	if len(required) > 0 {
		o.Set("required", required)
	}
	b, _ := jsonx.Encode(o, jsonx.Options{})
	return b
}

func str(desc string) *jsonx.Obj { return jsonx.New("type", "string", "description", desc) }
func optStr(desc string) *jsonx.Obj {
	return jsonx.New("type", []string{"string", "null"}, "description", desc)
}
func integer(def int, desc string) *jsonx.Obj {
	return jsonx.New("type", "integer", "default", def, "description", desc)
}
func boolean(desc string) *jsonx.Obj {
	return jsonx.New("type", "boolean", "default", false, "description", desc)
}
func strList(desc string) *jsonx.Obj {
	return jsonx.New("type", []string{"array", "null"}, "items", jsonx.New("type", "string"), "description", desc)
}
func enum(values []string, def, desc string) *jsonx.Obj {
	return jsonx.New("type", "string", "enum", values, "default", def, "description", desc)
}

func (h *handlers) vaultProp(desc string) *jsonx.Obj {
	names := h.reg.Names()
	o := jsonx.New("type", "string")
	if len(names) == 1 {
		o.Set("const", names[0])
	} else {
		o.Set("enum", names)
	}
	return o.Set("default", vaults.DefaultVault).Set("description", desc)
}

// ----- arguments ------------------------------------------------------------------

// addTool registers every tool, core or provider, so a call naming an argument
// its schema doesn't declare fails before the handler runs: a mistyped key
// would otherwise be dropped and the call answer as if it had done the work.
func addTool(s *mcp.Server, tool *mcp.Tool, next mcp.ToolHandler) {
	valid, closed := declaredArgs(tool.InputSchema)
	if !closed {
		s.AddTool(tool, next)
		return
	}
	s.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := unknownArgs(req.Params.Arguments, valid); err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		return next(ctx, req)
	})
}

// declaredArgs reads a schema's property names. A schema that declares no
// properties, or allows additional ones, is open.
func declaredArgs(schema any) ([]string, bool) {
	raw, isRaw := schema.(json.RawMessage)
	if !isRaw {
		b, err := json.Marshal(schema)
		if err != nil {
			return nil, false
		}
		raw = b
	}
	var s struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties *json.RawMessage           `json:"additionalProperties"`
	}
	if json.Unmarshal(raw, &s) != nil || s.Properties == nil {
		return nil, false
	}
	if s.AdditionalProperties != nil && strings.TrimSpace(string(*s.AdditionalProperties)) != "false" {
		return nil, false
	}
	names := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		names = append(names, k)
	}
	slices.Sort(names)
	return names, true
}

func unknownArgs(raw json.RawMessage, valid []string) error {
	var given map[string]json.RawMessage
	if json.Unmarshal(raw, &given) != nil {
		return nil // not an object; the handler reports it
	}
	var unknown []string
	for k := range given {
		if !slices.Contains(valid, k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	if len(valid) == 0 {
		return argError{msg: fmt.Sprintf("unknown arguments %s; this tool takes none", pystr.ReprList(unknown)), unknown: unknown}
	}
	return argError{msg: fmt.Sprintf("unknown arguments %s; valid arguments are %s", pystr.ReprList(unknown), pystr.ReprList(valid)), unknown: unknown}
}

type args struct{ o *jsonx.Obj }

// argError is an argument the schema refuses. arg names the one at fault and
// unknown the undeclared ones, so the failure can name the call to make instead.
type argError struct {
	msg     string
	arg     string
	missing bool
	unknown []string
}

func (e argError) Error() string { return e.msg }

func parseArgs(raw json.RawMessage) (args, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return args{jsonx.New()}, nil
	}
	v, err := jsonx.Decode(raw)
	if err != nil {
		return args{}, argError{msg: "arguments are not valid JSON"}
	}
	o, ok := v.(*jsonx.Obj)
	if !ok {
		return args{}, argError{msg: "arguments must be an object"}
	}
	return args{o}, nil
}

func (a args) value(name string) (any, bool) {
	v, ok := a.o.Get(name)
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

func (a args) str(name string, required bool, def string) (string, error) {
	v, ok := a.value(name)
	if !ok {
		if required {
			return "", argError{msg: name + ": field required", arg: name, missing: true}
		}
		return def, nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", argError{msg: name + ": input should be a valid string", arg: name}
	}
	return s, nil
}

func (a args) opt(name string) (*string, error) {
	v, ok := a.value(name)
	if !ok {
		return nil, nil
	}
	s, isStr := v.(string)
	if !isStr {
		return nil, argError{msg: name + ": input should be a valid string", arg: name}
	}
	return &s, nil
}

func (a args) integer(name string, def int) (int, error) {
	v, ok := a.value(name)
	if !ok {
		return def, nil
	}
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n), nil
		}
		if f, err := x.Float64(); err == nil && f == float64(int(f)) {
			return int(f), nil
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%d", &n); err == nil {
			return n, nil
		}
	}
	return 0, argError{msg: name + ": input should be a valid integer", arg: name}
}

func (a args) boolean(name string) (bool, error) {
	v, ok := a.value(name)
	if !ok {
		return false, nil
	}
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(x) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		}
	case json.Number:
		switch x.String() {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
	}
	return false, argError{msg: name + ": input should be a valid boolean", arg: name}
}

func (a args) list(name string) ([]string, bool, error) {
	v, ok := a.value(name)
	if !ok {
		return nil, false, nil
	}
	arr, isArr := v.([]any)
	if !isArr {
		return nil, false, argError{msg: name + ": input should be a valid list", arg: name}
	}
	out := []string{}
	for _, item := range arr {
		s, isStr := item.(string)
		if !isStr {
			return nil, false, argError{msg: name + ": items should be strings", arg: name}
		}
		out = append(out, s)
	}
	return out, true, nil
}

func (a args) choice(name string, choices []string, def string) (string, error) {
	s, err := a.str(name, false, def)
	if err != nil {
		return "", err
	}
	if !slices.Contains(choices, s) {
		var quoted []string
		for _, c := range choices {
			quoted = append(quoted, pystr.Repr(c))
		}
		return "", argError{msg: fmt.Sprintf("%s: input should be %s", name, strings.Join(quoted, " or ")), arg: name}
	}
	return s, nil
}

// ----- results ------------------------------------------------------------------

// result is a tool's answer. A failure envelope is also an MCP tool error
// (isError), the standard signal a client acts on without reading ok.
func result(o *jsonx.Obj) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: core.Text(o)}}, StructuredContent: o, IsError: o.Failed()}
}

// ----- core tools ------------------------------------------------------------------

type handlers struct {
	reg *vaults.Registry
	bin *binaryWatch
}

// fields are the frontmatter fields every vault's profile declares.
func (h *handlers) fields() []schema.Field {
	var profiles []*schema.Profile
	for _, v := range h.reg.Vaults {
		if v.Profile != nil {
			profiles = append(profiles, v.Profile)
		}
	}
	return schema.Union(profiles...)
}

// frontmatterProps are the input properties of frontmatter arguments.
func frontmatterProps(fargs []write.Arg) *jsonx.Obj {
	o := jsonx.New()
	for _, a := range fargs {
		switch {
		case a.Op == write.OpTouch:
			o.Set(a.Key, boolean(a.Help()))
		case a.Op != write.OpSet:
			o.Set(a.Key, strList(a.Help()))
		case a.Field.Kind == schema.KindInteger:
			o.Set(a.Key, jsonx.New("type", []string{"integer", "null"}, "description", a.Help()))
		case a.Field.Kind == schema.KindBoolean:
			o.Set(a.Key, jsonx.New("type", []string{"boolean", "null"}, "description", a.Help()))
		default:
			o.Set(a.Key, optStr(a.Help()))
		}
	}
	return o
}

// readChanges reads the frontmatter arguments a call gives.
func readChanges(a args, fargs []write.Arg) (write.Changes, error) {
	var c write.Changes
	for _, fa := range fargs {
		if _, given := a.value(fa.Key); !given {
			continue
		}
		var value any
		var err error
		switch {
		case fa.Op == write.OpTouch, fa.Field.Kind == schema.KindBoolean:
			value, err = a.boolean(fa.Key)
		case fa.Op != write.OpSet:
			value, _, err = a.list(fa.Key)
		case fa.Field.Kind == schema.KindInteger:
			value, err = a.integer(fa.Key, 0)
		default:
			value, err = a.str(fa.Key, false, "")
		}
		if err != nil {
			return c, err
		}
		c.Apply(fa, value)
	}
	return c, nil
}

func (h *handlers) vault(a args) (*core.Vault, string, error) {
	name, err := a.str("vault", false, vaults.DefaultVault)
	if err != nil {
		return nil, "", err
	}
	v := h.reg.Lookup(name)
	if v == nil {
		return nil, "", argError{msg: fmt.Sprintf("unknown vault %s; one of %s", pystr.Repr(name), pystr.ReprList(h.reg.Names())), arg: "vault"}
	}
	return v, name, nil
}

type toolFunc func(a args) (*jsonx.Obj, error)

func (h *handlers) add(s *mcp.Server, name, desc string, schema json.RawMessage, fn toolFunc) {
	tool := &mcp.Tool{Name: name, Description: desc, InputSchema: schema, Annotations: annotations[name], OutputSchema: outputSchemas[name],
		Meta: toolMeta[name]}
	reads := readOnly(name)
	addTool(s, tool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stale := h.bin.replaced()
		if stale && !reads {
			return result(h.bin.staleFailure(name)), nil
		}
		a, err := parseArgs(req.Params.Arguments)
		if err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		o, err := fn(a)
		if err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		if stale {
			o.Set("warning", h.bin.staleText())
		}
		return result(o), nil
	})
}

func (h *handlers) registerCore(s *mcp.Server) {
	h.add(s, "find", descFind, object(jsonx.New(
		"query", str("What to look for."),
		"vault", h.vaultProp("Which vault to search."),
		"by", enum(core.FindModes, core.FindModes[0], "relevance or text; see the tool description."),
		"system", optStr("Only docs whose `system` contains this."),
		"project", optStr("Only docs with a `related_projects` entry containing this."),
		"doc_type", optStr("Only docs of this doc_type. Queue notes (doc_type queue) are left out unless asked for."),
		"limit", integer(10, "Maximum hits (documents for `text`)."),
		"context_lines", integer(1, "Lines of context per match, `text` only."),
		"case_sensitive", boolean("Case-sensitive match, `text` only."),
		"include_archived", boolean("Also search the archive folder's docs (status archived), left out by default."),
		"fields", strList("Hit fields as dotted paths (`tags`, `snippets.text`)."),
	), []string{"query"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		q, err := a.str("query", true, "")
		if err != nil {
			return nil, err
		}
		by, err := a.choice("by", core.FindModes, core.FindModes[0])
		if err != nil {
			return nil, err
		}
		system, err := a.str("system", false, "")
		if err != nil {
			return nil, err
		}
		project, err := a.str("project", false, "")
		if err != nil {
			return nil, err
		}
		limit, err := a.integer("limit", 10)
		if err != nil {
			return nil, err
		}
		ctxLines, err := a.integer("context_lines", 1)
		if err != nil {
			return nil, err
		}
		cs, err := a.boolean("case_sensitive")
		if err != nil {
			return nil, err
		}
		docType, err := a.str("doc_type", false, "")
		if err != nil {
			return nil, err
		}
		archived, err := a.boolean("include_archived")
		if err != nil {
			return nil, err
		}
		fields, _, err := a.list("fields")
		if err != nil {
			return nil, err
		}
		return core.Find(v, q, core.FindArgs{By: by, Limit: limit, ContextLines: ctxLines, CaseSensitive: cs,
			System: system, Project: project, DocType: docType, IncludeArchived: archived, Fields: fields}), nil
	})

	h.add(s, "read_doc", descReadDoc, object(jsonx.New(
		"doc_id", str("A `find` hit's doc_id (a title or vault path also resolves)."),
		"vault", h.vaultProp("Which vault the doc lives in."),
		"sections", strList("Section slugs or heading paths, from a `find` hit or the outline."),
		"fields", strList("Doc fields to return; `outline` lists sections, frontmatter fields (`tags`, `system`) add more."),
		"include_restricted", boolean("Request a restricted body."),
	), []string{"doc_id"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		id, err := a.str("doc_id", true, "")
		if err != nil {
			return nil, err
		}
		var r core.ReadArgs
		if r.Sections, _, err = a.list("sections"); err != nil {
			return nil, err
		}
		if r.Fields, _, err = a.list("fields"); err != nil {
			return nil, err
		}
		if r.IncludeRestricted, err = a.boolean("include_restricted"); err != nil {
			return nil, err
		}
		return core.ReadDoc(v, id, r), nil
	})

	fields := h.fields()
	setArgs := write.Args(fields, "", false)
	setProps := jsonx.New(
		"relative_path", str("Path from the vault root."),
		"vault", h.vaultProp("Which vault."),
	).Merge(frontmatterProps(setArgs))
	h.add(s, "set_frontmatter", descSet, object(setProps, []string{"relative_path"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		rel, err := a.str("relative_path", true, "")
		if err != nil {
			return nil, err
		}
		c, err := readChanges(a, setArgs)
		if err != nil {
			return nil, err
		}
		return core.SetFrontmatter(v, rel, c), nil
	})

	createArgs := write.Args(slices.DeleteFunc(slices.Clone(fields), func(f schema.Field) bool { return f.Name == "doc_id" }), "", true)
	writeProps := jsonx.New(
		"mode", enum(core.WriteDocModes, "create", "See the tool description."),
		"body", str("Markdown without frontmatter: the whole doc, or the section's content."),
		"vault", h.vaultProp("Which vault."),
		"relative_path", optStr("Path from the vault root; required to create."),
		"doc_id", optStr("The new doc's id, or the doc to edit (or give relative_path)."),
		"heading", optStr(`The section to edit: a heading_path or section slug from read_doc's outline, or a unique heading text without the #s; "" for the text before the first heading. Section modes only.`),
		"include_restricted", boolean("Edit a restricted doc."),
	).Merge(frontmatterProps(createArgs))
	h.add(s, "write_doc", descWriteDoc, object(writeProps, []string{"body"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		var w core.WriteDocArgs
		if w.Mode, err = a.choice("mode", core.WriteDocModes, "create"); err != nil {
			return nil, err
		}
		if w.Body, err = a.str("body", true, ""); err != nil {
			return nil, err
		}
		if w.RelativePath, err = a.str("relative_path", false, ""); err != nil {
			return nil, err
		}
		if w.DocID, err = a.str("doc_id", false, ""); err != nil {
			return nil, err
		}
		if w.Heading, err = a.opt("heading"); err != nil {
			return nil, err
		}
		if w.New, err = readChanges(a, createArgs); err != nil {
			return nil, err
		}
		if w.IncludeRestricted, err = a.boolean("include_restricted"); err != nil {
			return nil, err
		}
		return core.WriteDoc(v, w), nil
	})

	h.add(s, "move_doc", descMoveDoc, object(jsonx.New(
		"to", str(`Destination from the vault root: a .md path ("Security/Reports/Example Audit.md"), or a folder ending in / to keep the file name.`),
		"vault", h.vaultProp("Which vault."),
		"doc_id", optStr("The doc to move (or give relative_path)."),
		"relative_path", optStr("The doc's current path from the vault root."),
		"with_folder", boolean("Move the doc's whole folder, files beside it included, into `to` (a folder)."),
		"archive", boolean("Into the archive: set status: archived first instead of refusing a doc without it."),
	), []string{"to"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		var m core.MoveArgs
		if m.To, err = a.str("to", true, ""); err != nil {
			return nil, err
		}
		if m.DocID, err = a.str("doc_id", false, ""); err != nil {
			return nil, err
		}
		if m.RelativePath, err = a.str("relative_path", false, ""); err != nil {
			return nil, err
		}
		if m.WithFolder, err = a.boolean("with_folder"); err != nil {
			return nil, err
		}
		if m.Archive, err = a.boolean("archive"); err != nil {
			return nil, err
		}
		return core.MoveDoc(v, m), nil
	})

	h.add(s, "move_folder", descMoveFolder, object(jsonx.New(
		"from", str(`The folder to move, from the vault root ("01 Projects/example-old").`),
		"to", str(`Its new path from the vault root ("01 Projects/example-new", "99 Archive/example-old").`),
		"vault", h.vaultProp("Which vault."),
		"archive", boolean("Into the archive: set status: archived on every doc in the folder first instead of refusing."),
		"include_restricted", boolean("Also rewrite links inside restricted notes, behind a Touch ID approval through 1Password."),
	), []string{"from", "to"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		var m core.FolderArgs
		if m.From, err = a.str("from", true, ""); err != nil {
			return nil, err
		}
		if m.To, err = a.str("to", true, ""); err != nil {
			return nil, err
		}
		if m.Archive, err = a.boolean("archive"); err != nil {
			return nil, err
		}
		if m.IncludeRestricted, err = a.boolean("include_restricted"); err != nil {
			return nil, err
		}
		return core.MoveFolder(v, m), nil
	})

	h.add(s, "task_board", descBoard, object(jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"status", optStr("Only this status."),
		"project", optStr("Only this project."),
		"stale_only", boolean("Only open or active tasks idle past stale_days."),
		"include_all", boolean("Include parked, done, wontfix."),
		"stale_days", integer(14, "Stale window in days."),
		"include_queue", boolean("Add open and returned queue items (of project, else every queue) under `queue`."),
		"limit", integer(boardLimit, "Most tasks, best first; 0 for all."),
		"fields", strList("Task fields to return (also slug, related_projects, effort, created, closed, tags, relative_path, age_days, stale)."),
	), nil), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		var f tasks.Filter
		if f.Status, err = a.str("status", false, ""); err != nil {
			return nil, err
		}
		if f.Project, err = a.str("project", false, ""); err != nil {
			return nil, err
		}
		if f.StaleOnly, err = a.boolean("stale_only"); err != nil {
			return nil, err
		}
		if f.IncludeAll, err = a.boolean("include_all"); err != nil {
			return nil, err
		}
		if f.StaleDays, err = a.integer("stale_days", 14); err != nil {
			return nil, err
		}
		var b core.BoardArgs
		if b.Limit, err = a.integer("limit", boardLimit); err != nil {
			return nil, err
		}
		if b.IncludeQueue, err = a.boolean("include_queue"); err != nil {
			return nil, err
		}
		if b.Fields, _, err = a.list("fields"); err != nil {
			return nil, err
		}
		return core.TaskBoard(v, f, b), nil
	})

	item := taskItemProps()
	taskProps := jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"items", jsonx.New("type", []string{"array", "null"}, "maxItems", core.MaxTaskItems,
			"items", jsonx.New("type", "object", "properties", item, "required", []string{"action"}, "additionalProperties", false),
			"description", "Several changes in one call, each with the fields below; leave the top-level ones unset."),
	).Merge(item)
	taskProps.Set("action", jsonx.New("type", "string", "enum", core.TaskActions, "description", "add, status, promote or delete; omit with items."))
	h.add(s, "task_write", descTaskWrite, object(taskProps, nil), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		raw, ok := a.value("items")
		if !ok {
			t, err := taskItem(a)
			if err != nil {
				return nil, err
			}
			return core.TaskWrite(v, t), nil
		}
		for _, k := range a.o.Keys() {
			if k != "vault" && k != "items" && !isNull(a.o, k) {
				return nil, argError{msg: "items: give each change's fields inside its item, not beside items (got " + pystr.Repr(k) + ")", arg: "items"}
			}
		}
		list, isList := raw.([]any)
		if !isList {
			return nil, argError{msg: "items: input should be a valid list", arg: "items"}
		}
		valid := item.Keys()
		var batch []core.TaskWriteArgs
		for i, entry := range list {
			o, isObj := entry.(*jsonx.Obj)
			if !isObj {
				return nil, argError{msg: fmt.Sprintf("items[%d]: input should be an object", i), arg: "items"}
			}
			if err := unknownArgs(json.RawMessage(jsonx.Compact(o)), valid); err != nil {
				return nil, argError{msg: fmt.Sprintf("items[%d]: %s", i, err), arg: "items"}
			}
			t, err := taskItem(args{o})
			if err != nil {
				return nil, argError{msg: fmt.Sprintf("items[%d]: %s", i, err), arg: "items"}
			}
			batch = append(batch, t)
		}
		return core.TaskWrites(v, batch), nil
	})

	h.add(s, "log_entries", descLogEntries, object(jsonx.New(
		"doc_id", str("The log note's doc_id (from `find`)."),
		"action", enum(core.LogActions, core.LogActions[0], "list or close; see the tool description."),
		"vault", h.vaultProp("Which vault."),
		"open_only", jsonx.New("type", "boolean", "default", true, "description", "list: only entries without a resolution."),
		"contains", optStr("list: only entries whose text contains this (case-insensitive)."),
		"limit", integer(logLimit, "list: most entries to return, in file order; 0 for all. `count` says how many matched."),
		"match", optStr("close: text (case-insensitive) that exactly one open entry contains."),
		"resolution", optStr("close: one line saying how the entry was resolved, e.g. a commit."),
		"fields", strList("Entry fields to return. Default: line, text; also closed, resolution."),
	), []string{"doc_id"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		id, err := a.str("doc_id", true, "")
		if err != nil {
			return nil, err
		}
		l := core.LogArgs{OpenOnly: true}
		if l.Action, err = a.choice("action", core.LogActions, core.LogActions[0]); err != nil {
			return nil, err
		}
		if _, given := a.value("open_only"); given {
			if l.OpenOnly, err = a.boolean("open_only"); err != nil {
				return nil, err
			}
		}
		for _, f := range []struct {
			name string
			dst  *string
		}{{"contains", &l.Contains}, {"match", &l.Match}, {"resolution", &l.Resolution}} {
			if *f.dst, err = a.str(f.name, false, ""); err != nil {
				return nil, err
			}
		}
		if l.Limit, err = a.integer("limit", logLimit); err != nil {
			return nil, err
		}
		if l.Fields, _, err = a.list("fields"); err != nil {
			return nil, err
		}
		return core.LogEntries(v, id, l), nil
	})

	linkProps := jsonx.New()
	for _, k := range queue.LinkKeys {
		linkProps.Set(k, jsonx.New("type", []string{"string", "null"}))
	}
	h.add(s, "queue", descQueue, object(jsonx.New(
		"action", enum(core.QueueActions, core.QueueActions[0], "list, add, set or close."),
		"vault", h.vaultProp("Which vault."),
		"project", optStr("The queue's project folder under 01 Projects, or backlog."),
		"repo", optStr("A repository directory name, resolved through index.md repos."),
		"cwd", optStr("A working directory, resolved to its repository's project."),
		"all", boolean("list: every queue."),
		"id", optStr("set, close: the item's id, e.g. example-3."),
		"body", optStr("add, set: the doc_id of the doc holding the item's detail; it must exist (\"\" removes it on set)."),
		"agent", optStr("add: the worker's agent id. list: only that agent's items. set, close: the item by agent instead of id; set with id writes the agent onto that item."),
		"origin", optStr("add: the sending session's repo or working directory. list: only items from it."),
		"include_origin", boolean("list: also delegated items anywhere whose origin is this repo."),
		"kind", optStr("add: "+strings.Join(queue.Kinds, ", ")+". list: only this kind. set: delegated, for a next or follow-up item (the only change)."),
		"text", optStr("add: one line saying what the work is. set: replace it. Escaped in the note, never by you."),
		"owner", optStr("add, set: who holds the item (one word; \"\" or \"-\" for nobody). list: only that owner's items."),
		"links", jsonx.New("type", []string{"object", "null"}, "properties", linkProps, "additionalProperties", false,
			"description", "add, set, close: typed links, each one word; on set, \"\" removes one. list: only items whose links equal these, e.g. {\"branch\": \"pass/x\"}."),
		"blocked_by", strList("add, set: item ids that must close before this one is ready."),
		"repos", strList("add, set: the repos one worker's item spans."),
		"base", optStr("add, set: the branch the item's work lands on (\"\" removes it on set)."),
		"transcript", optStr("add, set: the absolute path of the worker transcript holding the item's brief and report (\"\" removes it on set)."),
		"owns", strList("add, set: the paths the item's worker owns ([] clears them on set)."),
		"status", optStr("set: open or returned. close: closed (default) or dropped. list: comma-separated statuses or all; default open,returned."),
		"ready", boolean("list: only open next and follow-up items whose blocked_by are all closed or dropped."),
		"contains", optStr("list: only items whose text contains this (case-insensitive)."),
		"limit", integer(queueLimit, "list: most items to return; 0 for all; `count` is all that matched."),
		"claim", boolean("set: take the item for owner (and agent) only if no other owner or agent holds it and it is not done; it becomes open."),
		"release", boolean("set: hand the item back to the queue: no agent or owner, kind next (from delegated), status open. With id, agent names the holder it must have."),
		"resolution", optStr("close: one line: repo@sha (comma-separated for repos), a PR, a doc id, or why it was dropped."),
		"partial", boolean("close: record these repo@sha landings, closing once every repo has one."),
		"fields", strList("list: item fields to return. Default: id, kind, status, owner, text (and project, origin "+
			"on include_origin); also links (or links.pr), blocked_by, body, agent, repos, base, owns, transcript, created, resolution."),
	), nil), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		q, err := queueArgs(a)
		if err != nil {
			return nil, err
		}
		return core.Queue(v, q), nil
	})

	h.addResources(s)
}

// queueLimit is the queue list's default cap.
const queueLimit = 50

func queueArgs(a args) (core.QueueArgs, error) {
	q := core.QueueArgs{}
	var err error
	if q.Action, err = a.choice("action", core.QueueActions, core.QueueActions[0]); err != nil {
		return q, err
	}
	for _, f := range []struct {
		name string
		dst  *string
	}{{"project", &q.Project}, {"repo", &q.Repo}, {"cwd", &q.Cwd}, {"id", &q.ID}, {"agent", &q.Agent}, {"origin", &q.Origin}, {"kind", &q.Kind},
		{"status", &q.Status}, {"contains", &q.Contains}, {"resolution", &q.Resolution}} {
		if *f.dst, err = a.str(f.name, false, ""); err != nil {
			return q, err
		}
	}
	if q.Text, err = a.opt("text"); err != nil {
		return q, err
	}
	if q.Owner, err = a.opt("owner"); err != nil {
		return q, err
	}
	if q.Body, err = a.opt("body"); err != nil {
		return q, err
	}
	if q.Base, err = a.opt("base"); err != nil {
		return q, err
	}
	for _, f := range []struct {
		name string
		dst  *bool
	}{{"all", &q.All}, {"ready", &q.Ready}, {"claim", &q.Claim}, {"release", &q.Release}, {"include_origin", &q.IncludeOrigin}, {"partial", &q.Partial}} {
		if *f.dst, err = a.boolean(f.name); err != nil {
			return q, err
		}
	}
	if q.Limit, err = a.integer("limit", queueLimit); err != nil {
		return q, err
	}
	if q.BlockedBy, q.HasBlockedBy, err = a.list("blocked_by"); err != nil {
		return q, err
	}
	if q.Repos, q.HasRepos, err = a.list("repos"); err != nil {
		return q, err
	}
	if q.Transcript, err = a.opt("transcript"); err != nil {
		return q, err
	}
	if q.Owns, q.HasOwns, err = a.list("owns"); err != nil {
		return q, err
	}
	if q.Fields, _, err = a.list("fields"); err != nil {
		return q, err
	}
	if raw, ok := a.value("links"); ok {
		o, isObj := raw.(*jsonx.Obj)
		if !isObj {
			return q, argError{msg: "links: input should be an object", arg: "links"}
		}
		q.Links = map[string]string{}
		for _, k := range o.Keys() {
			v, _ := o.Get(k)
			switch x := v.(type) {
			case nil:
				q.Links[k] = ""
			case string:
				q.Links[k] = x
			default:
				return q, argError{msg: "links." + k + ": input should be a valid string", arg: "links"}
			}
		}
	}
	return q, nil
}

// logLimit is log_entries' default cap on a list.
const logLimit = 50

// boardLimit is task_board's default cap: the top of the ranked board.
const boardLimit = 25

// taskItemProps are one task change's arguments, top-level or in items.
func taskItemProps() *jsonx.Obj {
	p := jsonx.New("action", jsonx.New("type", "string", "enum", core.TaskActions, "description", "add, status, promote or delete."))
	for _, f := range []string{"title", "project"} {
		p.Set(f, optStr(""))
	}
	p.Set("related_projects", strList(""))
	p.Set("effort", jsonx.New("type", "string", "default", "S", "description", "S, M or L."))
	p.Set("priority", jsonx.New("type", "string", "default", "med", "description", "high, med or low."))
	p.Set("tags", strList(""))
	for _, f := range []string{"problem", "fix", "principle", "source", "doc_id", "status", "note_path"} {
		p.Set(f, optStr(""))
	}
	return p
}

func taskItem(a args) (core.TaskWriteArgs, error) {
	var t core.TaskWriteArgs
	var err error
	if t.Action, err = a.str("action", true, ""); err != nil {
		return t, err
	}
	for _, f := range []struct {
		name string
		dst  *string
		def  string
	}{{"title", &t.Title, ""}, {"project", &t.Project, ""}, {"effort", &t.Effort, "S"}, {"priority", &t.Priority, "med"},
		{"problem", &t.Problem, ""}, {"fix", &t.Fix, ""}, {"principle", &t.Principle, ""}, {"source", &t.Source, ""},
		{"doc_id", &t.DocID, ""}, {"status", &t.Status, ""}, {"note_path", &t.NotePath, ""}} {
		if *f.dst, err = a.str(f.name, false, f.def); err != nil {
			return t, err
		}
	}
	if t.RelatedProjects, _, err = a.list("related_projects"); err != nil {
		return t, err
	}
	if t.Tags, _, err = a.list("tags"); err != nil {
		return t, err
	}
	return t, nil
}

func isNull(o *jsonx.Obj, k string) bool {
	v, _ := o.Get(k)
	return v == nil
}

func (h *handlers) addResources(s *mcp.Server) {
	read := func(uri, docID string, vaultName string) (*mcp.ReadResourceResult, error) {
		v := h.reg.Lookup(vaultName)
		if v == nil {
			return nil, fmt.Errorf("unknown vault %s; one of %s", pystr.Repr(vaultName), pystr.ReprList(h.reg.Names()))
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: core.RenderDoc(v, docID)}}}, nil
	}
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vault://{vault}/quick-index", Name: "quick-index", Title: "Vault Quick Index",
		Description: "A vault's navigation hub for broad 'how do I' or 'where do I look' questions.", MIMEType: "text/markdown",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		rest, _ := strings.CutPrefix(uri, "vault://")
		name, _ := strings.CutSuffix(rest, "/quick-index")
		return read(uri, core.QuickIndexDocID, name)
	})
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vault://{vault}/doc/{doc_id}", Name: "vault-doc", Title: "Vault doc by doc_id",
		Description: "One indexed doc's markdown body, or an advisory plus metadata when restricted.", MIMEType: "text/markdown",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		rest, _ := strings.CutPrefix(uri, "vault://")
		name, docID, ok := strings.Cut(rest, "/doc/")
		if !ok {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return read(uri, docID, name)
	})
}

const descFind = `Find docs in a vault. Use this first for any question the vault may answer.

Returns ` + "`match_count`" + ` and ` + "`hits`" + ` (each with a doc_id); then ` + "`read_doc`" + ` the best hit.

by relevance (default) ranks title, system, tag and section matches. by text
searches bodies: several words match docs holding all of them, a single token
or a regex is a ripgrep pattern, zero hits fall back to relevance
("fallback"); restricted bodies are never searched.`

const descReadDoc = `Read a vault doc's markdown body, gated by its sensitivity.

Restricted bodies are withheld unless include_restricted is set and a Touch
ID approval through 1Password succeeds. sections returns just those sections
(the hit's ` + "`section`" + ` is the cheap read); fields=["outline"] lists them without text.`

const descSet = `Create or update a doc's frontmatter, validated against the vault's schema.

A file without frontmatter gets a new block (doc_id, title, doc_type and
system required; environment, status and sensitivity default to other,
active, internal). With frontmatter, omitted fields stay and doc_id never
changes once set. A list field is replaced whole, or edited with add_<field>
and remove_<field>. Every field the vault's profile declares is an argument.`

const descWriteDoc = `Create a registered doc, or edit one section of its body, validated against the vault's schema.

create (default): write relative_path with body and a new frontmatter block
    (doc_id, title, doc_type and system required; environment, status and
    sensitivity default to other, active, internal). Refuses an existing
    file and a body that carries its own frontmatter; adds ` + "`# <title>`" + `
    when the body has no H1.
replace_section: replace the section heading names (doc_id or
    relative_path names the doc): exactly what read_doc returns for it, so
    a small H2 includes its H3s; the heading line stays. heading takes a
    heading_path or section slug from read_doc's outline, or a unique
    heading text; "" is the text before the first heading.
append_section: add body at the end of the text under that heading,
    before its first subheading.
Section modes keep the frontmatter as it is except last_reviewed, which
becomes today. Restricted docs need include_restricted and Touch ID.`

const descMoveDoc = `Move or rename a registered doc; its doc_id and frontmatter stay as they are.

to is a .md path, or a folder ending in / that keeps the file name; missing
folders are created. Refused: a destination that exists, one outside the
indexed folders or inside a read-only one, and a file name another note in
the vault already has (Obsidian resolves [[Name]] by unique file name). A
rename in the same folder is how a shared name like Audit.md gets a unique
one. with_folder moves the doc's folder, evidence files included, into to.
The archive (99 Archive) takes only archived docs: status archived, or a
closed task; archive=true sets status: archived first. find leaves archived
docs out unless include_archived=true.
Every [[path/link]], ![[embed]] and [text](path) in the vault that named the
doc by path names its new path, alias and #heading kept; links by file name
alone need nothing. links lists each rewritten note with a count. Restricted
notes are not read: they are listed in skipped_restricted.`

const descMoveFolder = `Move or rename a folder with everything in it (notes, Reports/, assets), then point every link at the new path.

Afterwards each [[path/link]], ![[embed]] and [text](path) anywhere in the
vault that named a file in the folder by path names its new path, and a
relative link the move broke is recomputed; links by file name alone need
nothing. links lists each rewritten note with a count. Restricted notes are
rewritten only with include_restricted and Touch ID, else listed in
skipped_restricted. Refused: a destination that exists, one outside the
indexed folders and the archive, and a read-only folder.
A project renamed under 01 Projects also updates every related_projects entry
naming the old slug and retitles its queue note; doc_ids and queue item ids
never change, and an index.md without queue_prefix gets the old slug pinned
so new items keep their prefix. Into 99 Archive every doc must be archived
(a task: closed); archive=true sets status: archived first.`

const descLogEntries = `Work an append-only log note: one top-level bullet per entry, closed once
its line carries " → " and a resolution.

list (default): the entries as {line, text}, open ones only unless
    open_only=false, narrowed by contains, in file order.
close: append " → <resolution>" to the one open entry match names; zero or
    several matches is an error listing the candidates. The frontmatter
    stays as it is except last_reviewed, which becomes today.
Restricted notes are refused; notes in read-only folders can be listed, not closed.`

const descQueue = `Track work in a project's queue: one Obsidian task per item in
01 Projects/<project>/Queue.md (cross-cutting work in 07 Backlog/Queue.md).
Kinds: next, delegated (a worker holds it), decision (the user must
answer), follow-up. Status: open, returned (delegated only), closed,
dropped; a box ticked in Obsidian reads as closed. Each item carries the
date it was filed (created); a worker's item may carry base (the branch
it lands on), owns (the paths its worker owns) and transcript (the
absolute path of the worker transcript holding its brief and report).

Name the queue by project, repo or cwd (through the projects' index.md
repos). A write from a repo no project lists is refused with the call that
maps it (the backlog takes writes only as project=backlog); a list there
reads the backlog under a warning.
list (default): open and returned items; all=true every queue;
    include_origin adds the workers this repo sent to other queues.
add: kind and text; returns the new id (an agent's open item with the
    same text answers instead, as no_op); body names its detail doc.
set: id (or agent, when it holds one item) and what changes; claim=true
    with owner takes an item no other owner or agent holds, atomically.
    With an id, agent, kind=delegated (from next or follow-up) and
    links.branch ride in the same locked edit. release=true hands an item
    back as unowned next work. A change to an item another agent holds
    goes through with a warning naming it.
close: id (or agent) and resolution; status=dropped for work not done. An
    item with repos closes once each has a repo@sha; partial=true adds some.`

const descBoard = `A vault's task board: open work, counts, recently shipped, and the projects tasks can go in.`

const descTaskWrite = `Change a vault's tasks.

add: file a task (title; optional project, related_projects, effort S|M|L,
    priority high|med|low, tags, and the body sections problem, fix,
    principle, source). Without project it goes to the cross-cutting backlog.
status: move doc_id to status (open, active, parked, done, wontfix);
    done stamps ` + "`closed:`" + `.
promote: turn the inbox note at note_path into a task titled title,
    keeping its body and removing the note.
delete: remove doc_id permanently (mistakes only; finished work is
    status done or wontfix).
items: a list of these changes, each with its own action, applied in
    order in one call; each reports its own result, and a failure doesn't
    stop the rest or undo the earlier ones.`
