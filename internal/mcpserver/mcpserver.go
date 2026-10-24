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
	h := &handlers{reg: reg}
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

var coreToolNames = []string{"find", "read_doc", "set_frontmatter", "write_doc", "task_board", "task_write", "log_entries"}

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
	return jsonx.New("type", []string{"string", "null"}, "default", nil, "description", desc)
}
func integer(def int, desc string) *jsonx.Obj {
	return jsonx.New("type", "integer", "default", def, "description", desc)
}
func boolean(desc string) *jsonx.Obj {
	return jsonx.New("type", "boolean", "default", false, "description", desc)
}
func strList(desc string) *jsonx.Obj {
	return jsonx.New("type", []string{"array", "null"}, "items", jsonx.New("type", "string"), "default", nil, "description", desc)
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

func result(o *jsonx.Obj) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: core.Text(o)}}, StructuredContent: o}
}

// ----- core tools ------------------------------------------------------------------

type handlers struct{ reg *vaults.Registry }

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
	addTool(s, tool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a, err := parseArgs(req.Params.Arguments)
		if err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		o, err := fn(a)
		if err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		return result(o), nil
	})
}

func (h *handlers) registerCore(s *mcp.Server) {
	h.add(s, "find", descFind, object(jsonx.New(
		"query", str("What to look for."),
		"vault", h.vaultProp("Which vault to search."),
		"by", enum(core.FindModes, core.FindModes[0], "How to match; see the tool description."),
		"system", optStr("Only docs whose `system` field contains this (case-insensitive)."),
		"project", optStr("Only docs with a `related_projects` entry containing this (case-insensitive)."),
		"limit", integer(10, "Maximum hits (documents for `text`)."),
		"context_lines", integer(1, "Lines of context per match, `text` only."),
		"case_sensitive", boolean("Case-sensitive match, `text` only."),
		"fields", strList("Hit fields to return, as dotted paths (`tags`, `snippets.text`). "+
			"Default: doc_id, title, heading_path, section, section_summary, score."),
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
		fields, _, err := a.list("fields")
		if err != nil {
			return nil, err
		}
		return core.Find(v, q, core.FindArgs{By: by, Limit: limit, ContextLines: ctxLines, CaseSensitive: cs,
			System: system, Project: project, Fields: fields}), nil
	})

	h.add(s, "read_doc", descReadDoc, object(jsonx.New(
		"doc_id", str("The doc's stable id from a `find` hit (exact titles and vault-relative paths also resolve)."),
		"vault", h.vaultProp("Which vault the doc lives in."),
		"sections", strList("Section slugs or heading paths (from a `find` hit or the outline): return just their text."),
		"fields", strList("Doc fields to return. `outline` lists the sections without their text; frontmatter fields "+
			"such as `tags`, `system` or `obsidian_path` ask for more. Default: doc_id, title, status, last_reviewed, body."),
		"include_restricted", boolean("Request a restricted body; local policy still decides."),
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

	setProps := jsonx.New(
		"relative_path", str(`Path from the vault root, e.g. "03 Runbooks/Add Proxy Host.md".`),
		"vault", h.vaultProp("Which vault."),
		"doc_id", optStr("Stable id; required to create, must match when updating."),
	)
	for _, f := range []string{"title", "doc_type", "system", "environment", "status", "sensitivity"} {
		setProps.Set(f, optStr("Field value."))
	}
	for _, f := range []string{"tags", "related_projects", "related_assets"} {
		setProps.Set(f, strList("Replace the list."))
		setProps.Set("add_"+f, strList("Append to the list."))
		setProps.Set("remove_"+f, strList("Remove from the list (update only)."))
	}
	setProps.Set("last_reviewed", optStr("ISO date. Defaults to today on create."))
	setProps.Set("touch_last_reviewed", boolean("Set last_reviewed to today (update)."))
	h.add(s, "set_frontmatter", descSet, object(setProps, []string{"relative_path"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		rel, err := a.str("relative_path", true, "")
		if err != nil {
			return nil, err
		}
		var set write.Set
		for _, f := range []struct {
			name string
			dst  **string
		}{{"doc_id", &set.DocID}, {"title", &set.Title}, {"doc_type", &set.DocType}, {"system", &set.System},
			{"environment", &set.Environment}, {"status", &set.Status}, {"sensitivity", &set.Sensitivity}, {"last_reviewed", &set.LastReviewed}} {
			if *f.dst, err = a.opt(f.name); err != nil {
				return nil, err
			}
		}
		for _, f := range []struct {
			name string
			dst  *write.ListOp
		}{{"tags", &set.Tags}, {"related_projects", &set.RelatedProjects}, {"related_assets", &set.RelatedAssets}} {
			whole, has, err := a.list(f.name)
			if err != nil {
				return nil, err
			}
			add, _, err := a.list("add_" + f.name)
			if err != nil {
				return nil, err
			}
			remove, _, err := a.list("remove_" + f.name)
			if err != nil {
				return nil, err
			}
			*f.dst = write.ListOp{Set: whole, HasSet: has, Add: add, Remove: remove}
		}
		if set.TouchLastReviewed, err = a.boolean("touch_last_reviewed"); err != nil {
			return nil, err
		}
		return core.SetFrontmatter(v, rel, set), nil
	})

	writeProps := jsonx.New(
		"mode", enum(core.WriteDocModes, "create", "create, replace_section or append_section; see the tool description."),
		"body", str("Markdown without frontmatter: the whole doc for create, the section's new content otherwise."),
		"vault", h.vaultProp("Which vault."),
		"relative_path", optStr(`Path from the vault root, e.g. "03 Runbooks/Add Proxy Host.md". Required to create.`),
		"doc_id", optStr("Stable id: the new doc's on create, the doc to edit otherwise (or give relative_path)."),
	)
	for _, f := range []string{"title", "doc_type", "system", "environment", "status", "sensitivity"} {
		writeProps.Set(f, optStr("Frontmatter field, create only."))
	}
	for _, f := range []string{"tags", "related_projects", "related_assets"} {
		writeProps.Set(f, strList("Frontmatter list, create only."))
	}
	writeProps.Set("last_reviewed", optStr("ISO date, create only. Defaults to today."))
	writeProps.Set("heading", optStr("The section's exact heading text, any level, without the #s. Section modes only."))
	writeProps.Set("include_restricted", boolean("Edit a restricted doc; local policy still decides."))
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
		for _, f := range []struct {
			name string
			dst  *string
		}{{"relative_path", &w.RelativePath}, {"heading", &w.Heading}, {"doc_id", &w.New.DocID}, {"title", &w.New.Title},
			{"doc_type", &w.New.DocType}, {"system", &w.New.System}, {"environment", &w.New.Environment}, {"status", &w.New.Status},
			{"sensitivity", &w.New.Sensitivity}, {"last_reviewed", &w.New.LastReviewed}} {
			if *f.dst, err = a.str(f.name, false, ""); err != nil {
				return nil, err
			}
		}
		for _, f := range []struct {
			name string
			dst  *[]string
		}{{"tags", &w.New.Tags}, {"related_projects", &w.New.RelatedProjects}, {"related_assets", &w.New.RelatedAssets}} {
			if *f.dst, _, err = a.list(f.name); err != nil {
				return nil, err
			}
		}
		if w.IncludeRestricted, err = a.boolean("include_restricted"); err != nil {
			return nil, err
		}
		return core.WriteDoc(v, w), nil
	})

	h.add(s, "task_board", descBoard, object(jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"status", optStr("Only this status (open, active, parked, done, wontfix)."),
		"project", optStr("Only this project (folder name or a related_projects link)."),
		"stale_only", boolean("Only open or active tasks untouched past stale_days."),
		"include_all", boolean("Include parked, done and wontfix."),
		"stale_days", integer(14, "Stale window in days."),
		"limit", integer(boardLimit, "Most tasks to return, best first; 0 for all. `count` says how many matched."),
		"fields", strList("Task fields to return. Default: doc_id, title, status, project, priority; "+
			"also slug, related_projects, effort, created, closed, tags, relative_path, age_days, stale."),
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
		if b.Fields, _, err = a.list("fields"); err != nil {
			return nil, err
		}
		return core.TaskBoard(v, f, b), nil
	})

	item := taskItemProps()
	taskProps := jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"items", jsonx.New("type", []string{"array", "null"}, "default", nil, "maxItems", core.MaxTaskItems,
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

	h.addResources(s)
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

Returns ` + "`match_count`" + ` and ` + "`hits`" + ` (each with a doc_id) in every mode. Then
call ` + "`read_doc`" + ` on the best hit and answer from the doc's own text.

by: relevance (ranked title/system/tag/section match, the default) or
text (body search: several words match docs holding all of them in any
order, ranked by doc with the best section as the hit; a single token or a
regex is a ripgrep pattern; zero hits fall back to relevance and say
"fallback": "relevance"; restricted bodies are never searched).

system and project narrow either mode to docs whose ` + "`system`" + ` field, or one
of whose ` + "`related_projects`" + ` entries, contains the value (case-insensitive).`

const descReadDoc = `Read a vault doc's markdown body, gated by its sensitivity.

public, internal and sensitive bodies are released (sensitive ones carry an
` + "`advisory`" + ` to pass along). restricted bodies are withheld unless
` + "`include_restricted=True`" + ` and a Touch ID approval through 1Password succeeds.

sections returns just those sections (the hit's ` + "`section`" + ` is the cheap read);
fields=["outline"] lists the doc's sections without any text.`

const descSet = `Create or update a doc's frontmatter, validated against the vault's schema.

A file without frontmatter gets a new block (doc_id, title, doc_type and
system are required; environment, status and sensitivity default to
other, active, internal). A file with frontmatter is updated in place:
omitted fields stay as they are, and doc_id can never change.`

const descWriteDoc = `Create a registered doc, or edit one section of its body, validated against the vault's schema.

create (default): write relative_path with body and a new frontmatter block
    (doc_id, title, doc_type and system required; environment, status and
    sensitivity default to other, active, internal). Refuses an existing
    file and a body that carries its own frontmatter; adds ` + "`# <title>`" + `
    when the body has no H1.
replace_section: replace the content under heading (doc_id or
    relative_path names the doc) up to the next heading of the same or
    higher level; the heading line stays.
append_section: add body to the end of that section.
Section modes keep the frontmatter as it is except last_reviewed, which
becomes today. Restricted docs need include_restricted and a Touch ID approval through 1Password.`

const descLogEntries = `Work an append-only log note: one top-level bullet per entry, closed once
its line carries " → " and a resolution.

list (default): the entries as {line, text}, open ones only unless
    open_only=false, narrowed by contains, in file order.
close: append " → <resolution>" to the one open entry match names; zero or
    several matches is an error listing the candidates. The frontmatter
    stays as it is except last_reviewed, which becomes today.
Restricted notes are refused; notes in read-only folders can be listed, not closed.`

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
