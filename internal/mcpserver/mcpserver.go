// Package mcpserver is the MCP face: one server for every vault. Shared tools
// register once with a `vault` argument; edu adds education_dataset; each
// provider's tools are re-exported as they are.
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
	"github.com/joeseverino/severino-vault-mcp/internal/education"
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

1. Operational questions ("how do I X", "what's the runbook for Y"): call
   ` + "`find`" + ` before writing any prose, then ` + "`read_doc`" + ` on the best hit, and
   answer in the doc's own words at the doc's length. Quote commands
   verbatim. Never substitute a generic tutorial for an existing doc.
2. No relevant hit: say so, then offer to write the doc (write the file,
   then ` + "`set_frontmatter`" + ` to register it).
3. Broad questions: start from the ` + "`vault://labs/quick-index`" + ` resource (or
   ` + "`read_doc`" + ` on the Quick Index doc), then read the target doc.
4. Progress or log questions ("what did I do Friday?"): ` + "`daily_progress`" + `.
5. ` + "`find(by=...)`" + `: relevance (default), system (the ` + "`system:`" + ` field),
   project (` + "`related_projects`" + `), text (full-text body search).

Sensitivity: public, internal and sensitive bodies come back from
` + "`read_doc`" + ` (sensitive adds an ` + "`advisory`" + ` to pass along). restricted bodies
are withheld unless the user explicitly needs one; pass
` + "`include_restricted=True`" + `, which also needs a local unlock.

Writes: ` + "`set_frontmatter`" + ` creates or updates a doc's frontmatter against that
vault's schema; ` + "`task_write`" + ` adds, moves, promotes or deletes tasks. After a
write, remind the user to run their vault sync.

edu: ` + "`education_dataset`" + ` returns the publishable institutions and courses
the site and resume read.
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
	if edu := reg.Lookup("edu"); edu != nil {
		s.AddTool(&mcp.Tool{
			Name: "education_dataset",
			Description: "The edu vault's publishable dataset: institutions and their courses (row facts from frontmatter, " +
				"public bullets from each course's `## Site` section). The same JSON `severino-vault-mcp export education` " +
				"emits for the site and the resume.",
			InputSchema: object(jsonx.New(), nil),
		}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return result(education.Dataset(edu.Loader, edu.Profile.Statuses)), nil
		})
	}
	taken := map[string]bool{}
	for _, name := range coreToolNames {
		taken[name] = true
	}
	taken["education_dataset"] = true
	for _, p := range reg.Providers {
		for _, tool := range p.Tools {
			if taken[tool.Name] {
				fmt.Fprintf(log, "severino-vault-mcp: provider %s tool %s skipped; the name is taken\n", p.Vault(), tool.Name)
				continue
			}
			taken[tool.Name] = true
			p := p
			t := *tool
			s.AddTool(&t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return p.Call(ctx, t.Name, req.Params.Arguments)
			})
		}
	}
	return s
}

var coreToolNames = []string{"find", "read_doc", "recent_changes", "daily_progress", "set_frontmatter", "update_link", "task_board", "task_write"}

// ----- schemas ------------------------------------------------------------------

func object(props *jsonx.Obj, required []string) json.RawMessage {
	o := jsonx.New("type", "object", "properties", props)
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

type args struct{ o *jsonx.Obj }

type argError struct{ msg string }

func (e argError) Error() string { return e.msg }

func parseArgs(raw json.RawMessage) (args, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return args{jsonx.New()}, nil
	}
	v, err := jsonx.Decode(raw)
	if err != nil {
		return args{}, argError{"arguments are not valid JSON"}
	}
	o, ok := v.(*jsonx.Obj)
	if !ok {
		return args{}, argError{"arguments must be an object"}
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
			return "", argError{name + ": field required"}
		}
		return def, nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", argError{name + ": input should be a valid string"}
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
		return nil, argError{name + ": input should be a valid string"}
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
	return 0, argError{name + ": input should be a valid integer"}
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
	return false, argError{name + ": input should be a valid boolean"}
}

func (a args) list(name string) ([]string, bool, error) {
	v, ok := a.value(name)
	if !ok {
		return nil, false, nil
	}
	arr, isArr := v.([]any)
	if !isArr {
		return nil, false, argError{name + ": input should be a valid list"}
	}
	out := []string{}
	for _, item := range arr {
		s, isStr := item.(string)
		if !isStr {
			return nil, false, argError{name + ": items should be strings"}
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
		return "", argError{fmt.Sprintf("%s: input should be %s", name, strings.Join(quoted, " or "))}
	}
	return s, nil
}

// ----- results ------------------------------------------------------------------

func result(o *jsonx.Obj) *mcp.CallToolResult {
	text, _ := jsonx.Encode(o, jsonx.Options{Indent: 2})
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}, StructuredContent: o}
}

func toolError(name string, err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Error executing tool " + name + ": " + err.Error()}},
		IsError: true,
	}
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
		return nil, "", argError{fmt.Sprintf("unknown vault %s; one of %s", pystr.Repr(name), pystr.ReprList(h.reg.Names()))}
	}
	return v, name, nil
}

type toolFunc func(a args) (*jsonx.Obj, error)

func (h *handlers) add(s *mcp.Server, name, desc string, schema json.RawMessage, fn toolFunc) {
	s.AddTool(&mcp.Tool{Name: name, Description: desc, InputSchema: schema},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			a, err := parseArgs(req.Params.Arguments)
			if err != nil {
				return toolError(name, err), nil
			}
			o, err := fn(a)
			if err != nil {
				return toolError(name, err), nil
			}
			return result(o), nil
		})
}

func (h *handlers) registerCore(s *mcp.Server) {
	h.add(s, "find", descFind, object(jsonx.New(
		"query", str("What to look for."),
		"vault", h.vaultProp("Which vault to search."),
		"by", enum([]string{"relevance", "system", "project", "text"}, "relevance", "How to match; see the tool description."),
		"limit", integer(10, "Maximum hits (documents for `text`)."),
		"context_lines", integer(1, "Lines of context per match, `text` only."),
		"case_sensitive", boolean("Case-sensitive match, `text` only."),
	), []string{"query"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		q, err := a.str("query", true, "")
		if err != nil {
			return nil, err
		}
		by, err := a.choice("by", []string{"relevance", "system", "project", "text"}, "relevance")
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
		return core.Find(v, q, core.FindArgs{By: by, Limit: limit, ContextLines: ctxLines, CaseSensitive: cs}), nil
	})

	h.add(s, "read_doc", descReadDoc, object(jsonx.New(
		"doc_id", str("The doc's stable id from a `find` hit (exact titles and vault-relative paths also resolve)."),
		"vault", h.vaultProp("Which vault the doc lives in."),
		"section", optStr("A section slug or heading path from a `find` hit, to return just that section."),
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
		section, err := a.str("section", false, "")
		if err != nil {
			return nil, err
		}
		inc, err := a.boolean("include_restricted")
		if err != nil {
			return nil, err
		}
		return core.ReadDoc(v, id, section, inc), nil
	})

	h.add(s, "recent_changes", descRecent, object(jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"days", integer(7, "Look-back window in days."),
		"limit", integer(50, "Maximum commits."),
	), nil), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		days, err := a.integer("days", 7)
		if err != nil {
			return nil, err
		}
		limit, err := a.integer("limit", 50)
		if err != nil {
			return nil, err
		}
		return core.RecentChanges(v, days, limit), nil
	})

	h.add(s, "daily_progress", descDaily, object(jsonx.New(
		"query", str("The natural-language progress question."),
		"vault", h.vaultProp("Which vault's daily notes."),
		"today", optStr("ISO anchor date for relative terms; omit in normal use."),
	), []string{"query"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		q, err := a.str("query", true, "")
		if err != nil {
			return nil, err
		}
		today, err := a.str("today", false, "")
		if err != nil {
			return nil, err
		}
		return core.DailyProgress(v, q, today), nil
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

	h.add(s, "update_link", descLink, object(jsonx.New(
		"doc_id", str("The doc's stable id."),
		"label", str("The link's exact visible label."),
		"expected_href", str("The exact current URL."),
		"replacement_href", str("The new URL."),
		"vault", h.vaultProp("Which vault."),
	), []string{"doc_id", "label", "expected_href", "replacement_href"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		vals := make([]string, 4)
		for i, name := range []string{"doc_id", "label", "expected_href", "replacement_href"} {
			if vals[i], err = a.str(name, true, ""); err != nil {
				return nil, err
			}
		}
		return core.UpdateLink(v, vals[0], vals[1], vals[2], vals[3]), nil
	})

	h.add(s, "task_board", descBoard, object(jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"status", optStr("Only this status (open, active, parked, done, wontfix)."),
		"project", optStr("Only this project (folder name or a related_projects link)."),
		"stale_only", boolean("Only open or active tasks untouched past stale_days."),
		"include_all", boolean("Include parked, done and wontfix."),
		"stale_days", integer(14, "Stale window in days."),
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
		return core.TaskBoard(v, f), nil
	})

	taskProps := jsonx.New(
		"action", jsonx.New("type", "string", "enum", []string{"add", "status", "promote", "delete"}, "description", "add, status, promote or delete."),
		"vault", h.vaultProp("Which vault."),
	)
	for _, f := range []string{"title", "project"} {
		taskProps.Set(f, optStr(""))
	}
	taskProps.Set("related_projects", strList(""))
	taskProps.Set("effort", jsonx.New("type", "string", "default", "S", "description", "S, M or L."))
	taskProps.Set("priority", jsonx.New("type", "string", "default", "med", "description", "high, med or low."))
	taskProps.Set("tags", strList(""))
	for _, f := range []string{"problem", "fix", "principle", "source", "doc_id", "status", "note_path"} {
		taskProps.Set(f, optStr(""))
	}
	h.add(s, "task_write", descTaskWrite, object(taskProps, []string{"action"}), func(a args) (*jsonx.Obj, error) {
		v, _, err := h.vault(a)
		if err != nil {
			return nil, err
		}
		var t core.TaskWriteArgs
		if t.Action, err = a.str("action", true, ""); err != nil {
			return nil, err
		}
		for _, f := range []struct {
			name string
			dst  *string
			def  string
		}{{"title", &t.Title, ""}, {"project", &t.Project, ""}, {"effort", &t.Effort, "S"}, {"priority", &t.Priority, "med"},
			{"problem", &t.Problem, ""}, {"fix", &t.Fix, ""}, {"principle", &t.Principle, ""}, {"source", &t.Source, ""},
			{"doc_id", &t.DocID, ""}, {"status", &t.Status, ""}, {"note_path", &t.NotePath, ""}} {
			if *f.dst, err = a.str(f.name, false, f.def); err != nil {
				return nil, err
			}
		}
		if t.RelatedProjects, _, err = a.list("related_projects"); err != nil {
			return nil, err
		}
		if t.Tags, _, err = a.list("tags"); err != nil {
			return nil, err
		}
		return core.TaskWrite(v, t), nil
	})

	h.addResources(s)
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

by: relevance (ranked title/system/tag/section match, the default),
system (docs whose system, title or doc_id contains the query),
project (docs listing the query in related_projects), or
text (ripgrep over doc bodies; hits carry line snippets, and
restricted bodies are never searched).`

const descReadDoc = `Read a vault doc's markdown body, gated by its sensitivity.

public, internal and sensitive bodies are released (sensitive ones carry an
` + "`advisory`" + ` to pass along). restricted bodies are withheld unless
` + "`include_restricted=True`" + ` and the local unlock on the Mac succeeds.`

const descRecent = `Recent commits touching a vault's indexed folders (metadata only, no diffs).`

const descDaily = `Read the daily note a progress question refers to ("what did I do Friday?").

Resolves today, yesterday, weekday names, ` + "`last Friday`" + `, ` + "`YYYY-MM-DD`" + ` and
` + "`MM/DD/YYYY`" + `, and returns the note body plus its progress lines.`

const descSet = `Create or update a doc's frontmatter, validated against the vault's schema.

A file without frontmatter gets a new block (doc_id, title, doc_type and
system are required; environment, status and sensitivity default to
other, active, internal). A file with frontmatter is updated in place:
omitted fields stay as they are, and doc_id can never change.`

const descLink = `Replace one exact Markdown link in a doc, atomically.

Exactly one link with this label and href must match, both URLs must be
absolute http(s), and restricted docs are refused.`

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
    status done or wontfix).`
