package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestCoreToolsCarryHonestAnnotations(t *testing.T) {
	reg, _ := labsOnly(t)
	all := tools(t, connect(t, reg))
	for _, name := range coreTools {
		a := all[name].Annotations
		if a == nil || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Fatalf("%s: %+v", name, a)
		}
		readOnly := slices.Contains([]string{"find", "read_doc", "task_board"}, name)
		if a.ReadOnlyHint != readOnly {
			t.Errorf("%s readOnlyHint %v", name, a.ReadOnlyHint)
		}
		destructive := slices.Contains([]string{"set_frontmatter", "write_doc", "task_write"}, name)
		if !readOnly && (a.DestructiveHint == nil || *a.DestructiveHint != destructive) {
			t.Errorf("%s destructiveHint %v, want %v", name, a.DestructiveHint, destructive)
		}
	}
	for _, name := range []string{"write_doc", "task_write", "log_entries"} {
		if all[name].Annotations.IdempotentHint {
			t.Errorf("%s appends or adds, so repeating it is not idempotent", name)
		}
	}
}

// Clients validate structuredContent against outputSchema, so every result a
// core tool returns, failures included, must fit the schema it declares.
func TestStructuredContentFitsEachOutputSchema(t *testing.T) {
	reg, root := labsOnly(t)
	tk.MultisectionDoc(t, root)
	exampleLog(t, root)
	s := connect(t, reg)
	all := tools(t, s)
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"find", map[string]any{"query": "nginx proxy"}},
		{"find", map[string]any{"query": "nginx proxy", "fields": []string{"tags", "last_reviewed", "obsidian_path"}}},
		{"find", map[string]any{"query": "daily job", "by": "text", "fields": []string{"snippets", "snippet", "match_count"}}},
		{"find", map[string]any{"query": "nginx", "fields": []string{"nope"}}},
		{"read_doc", map[string]any{"doc_id": "rb-backup-ops"}},
		{"read_doc", map[string]any{"doc_id": "rb-backup-ops", "fields": []string{"outline", "tags"}}},
		{"read_doc", map[string]any{"doc_id": "rb-backup-ops", "sections": []string{"troubleshooting"}}},
		{"read_doc", map[string]any{"doc_id": "rb-backup-ops", "sections": []string{"nope"}}},
		{"read_doc", map[string]any{"doc_id": "infra-local-pki"}},
		{"read_doc", map[string]any{"doc_id": "no-such-doc"}},
		{"task_write", map[string]any{"action": "add", "title": "Schema check"}},
		{"task_write", map[string]any{"items": []any{map[string]any{"action": "add", "title": "Batch"}, map[string]any{"action": "delete", "doc_id": "task-nope"}}}},
		{"task_board", map[string]any{}},
		{"task_board", map[string]any{"fields": []string{"stale", "age_days", "tags"}}},
		{"set_frontmatter", map[string]any{"relative_path": "03 Runbooks/Missing.md"}},
		{"write_doc", map[string]any{"body": "x", "mode": "append_section", "heading": "Nope", "doc_id": "rb-backup-ops"}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "fields": []string{"line", "text", "closed", "resolution"}, "open_only": false}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "action": "close", "match": "lesson", "resolution": "x"}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "action": "close", "match": "lesson one", "resolution": "x"}},
	}
	resolved := map[string]*jsonschema.Resolved{}
	for _, name := range coreTools {
		raw, _ := json.Marshal(all[name].OutputSchema)
		var schema jsonschema.Schema
		if err := json.Unmarshal(raw, &schema); err != nil || schema.Type != "object" {
			t.Fatalf("%s outputSchema: %s", name, raw)
		}
		r, err := schema.Resolve(nil)
		if err != nil {
			t.Fatal(name, err)
		}
		resolved[name] = r
	}
	for _, c := range calls {
		res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil || res.IsError {
			t.Fatalf("%s %v: %v %+v", c.tool, c.args, err, res)
		}
		if err := resolved[c.tool].Validate(res.StructuredContent); err != nil {
			t.Errorf("%s %v: %v\n%s", c.tool, c.args, err, res.Content[0].(*mcp.TextContent).Text)
		}
	}
}

// Claude Code reads two _meta keys on a tools/list entry: anthropic/alwaysLoad
// keeps a tool out of tool search, and anthropic/maxResultSizeChars moves the
// point past which a result is saved to a file (default 50,000 characters,
// ceiling 500,000).
func TestToolsCarryClaudeCodeLoadingHints(t *testing.T) {
	reg, _ := labsOnly(t)
	all := tools(t, connect(t, reg))
	for _, name := range coreTools {
		meta := all[name].Meta
		always := slices.Contains([]string{"find", "read_doc"}, name)
		if got, _ := meta["anthropic/alwaysLoad"].(bool); got != always {
			t.Errorf("%s alwaysLoad %v, want %v", name, meta["anthropic/alwaysLoad"], always)
		}
		size, sized := meta["anthropic/maxResultSizeChars"].(float64)
		if want := slices.Contains([]string{"read_doc", "task_board"}, name); sized != want || (sized && (size <= 50_000 || size > 500_000)) {
			t.Errorf("%s maxResultSizeChars %v", name, meta["anthropic/maxResultSizeChars"])
		}
	}
}

func TestToolsListIsInAStableOrder(t *testing.T) {
	reg, _ := labsOnly(t)
	s := connect(t, reg)
	var first []string
	for range 3 {
		var got []string
		for tool, err := range s.Tools(context.Background(), nil) {
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, tool.Name)
		}
		if !slices.IsSorted(got) || (first != nil && !slices.Equal(got, first)) {
			t.Fatal(got)
		}
		first = got
	}
}

func TestTaskWriteItemsBatchThroughOneCall(t *testing.T) {
	reg, _ := labsOnly(t)
	s := connect(t, reg)
	r, isErr := call(t, s, "task_write", map[string]any{"items": []any{
		map[string]any{"action": "add", "title": "First batched task"},
		map[string]any{"action": "status", "doc_id": "task-no-such-task", "status": "done"},
		map[string]any{"action": "add", "title": "Second batched task", "priority": "high"},
	}})
	if isErr || r.Bool("ok") || jsonx.Compact(tk.Get(r, "applied")) != "2" || jsonx.Compact(tk.Get(r, "failed")) != "[1]" ||
		tk.Get(r, "results.1.ok") != false || tk.Get(r, "results.2.ok") != true || strings.Contains(jsonx.Compact(r), "receipt") {
		t.Fatal(jsonx.Compact(r))
	}
	board, _ := call(t, s, "task_board", map[string]any{})
	if got := decodedIDs(board, "tasks"); !slices.Contains(got, tk.Get(r, "results.0.doc_id").(string)) || len(got) != 2 {
		t.Fatal(got)
	}
	for name, args := range map[string]map[string]any{
		"items[1]: unknown arguments ['titel']":       {"items": []any{map[string]any{"action": "add", "title": "x"}, map[string]any{"action": "add", "titel": "x"}}},
		"items[0]: action: field required":            {"items": []any{map[string]any{"title": "x"}}},
		"give each change's fields inside its item":   {"items": []any{map[string]any{"action": "add", "title": "x"}}, "action": "add"},
		"items[0]: input should be an object":         {"items": []any{"add"}},
		"action: field required":                      {"title": "x"},
		"items: give at least one item, or call with": {"items": []any{}},
	} {
		r, isErr := call(t, s, "task_write", args)
		if !strings.Contains(r.Str("error"), name) {
			t.Errorf("%s: %v %s", name, isErr, jsonx.Compact(r))
		}
	}
	if board, _ := call(t, s, "task_board", map[string]any{}); len(decodedIDs(board, "tasks")) != 2 {
		t.Fatal("a refused batch wrote", jsonx.Compact(board))
	}
}

// decodedIDs reads doc_ids from a list in a decoded tool result.
func decodedIDs(o *jsonx.Obj, path string) []string {
	out := []string{}
	list, _ := tk.Get(o, path).([]any)
	for _, item := range list {
		if obj, ok := item.(*jsonx.Obj); ok {
			out = append(out, obj.Str("doc_id"))
		}
	}
	return out
}
