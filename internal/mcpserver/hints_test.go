package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

func TestCoreToolsCarryHonestAnnotations(t *testing.T) {
	reg, _ := labsOnly(t)
	all := tools(t, connect(t, reg))
	for _, name := range coreTools {
		a := all[name].Annotations
		if a == nil || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Fatalf("%s: %+v", name, a)
		}
		readOnly := slices.Contains([]string{"find", "read_doc"}, name)
		if a.ReadOnlyHint != readOnly {
			t.Errorf("%s readOnlyHint %v", name, a.ReadOnlyHint)
		}
		destructive := slices.Contains([]string{"set_frontmatter", "write_doc", "move_folder", "queue"}, name)
		if !readOnly && (a.DestructiveHint == nil || *a.DestructiveHint != destructive) {
			t.Errorf("%s destructiveHint %v, want %v", name, a.DestructiveHint, destructive)
		}
	}
	for _, name := range []string{"write_doc", "log_entries", "queue"} {
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
		{"set_frontmatter", map[string]any{"relative_path": "03 Runbooks/Missing.md"}},
		{"write_doc", map[string]any{"body": "x", "mode": "append_section", "heading": "Nope", "doc_id": "rb-backup-ops"}},
		{"write_doc", map[string]any{"mode": "replace_text", "old_text": "e", "new_text": "x", "doc_id": "rb-backup-ops"}},
		{"write_doc", map[string]any{"mode": "replace_text", "old_text": " to keep things current", "new_text": "", "doc_id": "rb-backup-ops"}},
		{"move_doc", map[string]any{"doc_id": "rb-backup-ops", "to": "02 Infrastructure/"}},
		{"move_doc", map[string]any{"doc_id": "rb-backup-ops", "to": "00 Inbox/"}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "fields": []string{"line", "text", "closed", "resolution"}, "open_only": false}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "action": "close", "match": "lesson", "resolution": "x"}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "action": "close", "match": "lesson one", "resolution": "x"}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "action": "annotate", "match": "lesson one", "resolution": "y"}},
		{"log_entries", map[string]any{"doc_id": "infra-example-log", "action": "annotate", "match": "lesson one", "reopen": true}},
		{"queue", map[string]any{"action": "add", "project": "backlog", "kind": "delegated", "text": "Schema check", "agent": "a1",
			"links": map[string]any{"branch": "pass/x"}, "blocked_by": []string{"backlog-9"}}},
		{"queue", map[string]any{"action": "set", "agent": "a1", "status": "returned", "owner": "worker"}},
		{"queue", map[string]any{"action": "set", "id": "backlog-1", "claim": true, "owner": "someone-else"}},
		{"queue", map[string]any{"action": "list", "all": true, "fields": []string{"id", "owner", "links", "blocked_by", "agent"}}},
		{"queue", map[string]any{"action": "close", "id": "backlog-1", "resolution": "example-repo@abc1234"}},
		{"queue", map[string]any{"action": "list", "project": "backlog", "status": "all", "fields": []string{"resolution", "links.branch"}}},
		{"queue", map[string]any{"action": "list"}},
		{"queue", map[string]any{"action": "add", "project": "backlog", "kind": "delegated", "text": "Span", "agent": "a2",
			"origin": "example-site", "repos": []string{"example-a", "example-b"}}},
		{"queue", map[string]any{"action": "close", "agent": "a2", "partial": true, "resolution": "example-a@abc1234"}},
		{"queue", map[string]any{"action": "list", "cwd": root, "include_origin": true, "fields": []string{"id", "project", "origin", "repos", "resolution"}}},
		{"queue", map[string]any{"action": "add", "cwd": root, "kind": "next", "text": "Unmapped"}},
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
		if err != nil {
			t.Fatalf("%s %v: %v %+v", c.tool, c.args, err, res)
		}
		if err := resolved[c.tool].Validate(res.StructuredContent); err != nil {
			t.Errorf("%s %v: %v\n%s", c.tool, c.args, err, res.Content[0].(*mcp.TextContent).Text)
		}
	}
}

// Claude Code reads two _meta keys on a tools/list entry: anthropic/alwaysLoad
// keeps a tool out of tool search, and anthropic/maxResultSizeChars raises the
// point past which a result is saved to a file (default 50,000 characters).
// Every answer read_doc gives by default fits under that default, so no tool
// raises it.
func TestToolsCarryClaudeCodeLoadingHints(t *testing.T) {
	reg, _ := labsOnly(t)
	all := tools(t, connect(t, reg))
	for _, name := range coreTools {
		meta := all[name].Meta
		always := slices.Contains([]string{"find", "read_doc"}, name)
		if got, _ := meta["anthropic/alwaysLoad"].(bool); got != always {
			t.Errorf("%s alwaysLoad %v, want %v", name, meta["anthropic/alwaysLoad"], always)
		}
		if _, sized := meta["anthropic/maxResultSizeChars"]; sized {
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
