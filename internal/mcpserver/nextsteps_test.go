package mcpserver

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// stepProblems lists what keeps a result's next_steps from telling an agent
// its next move: none at all, or a step that doesn't start with a call to a
// registered tool.
func stepProblems(r *jsonx.Obj, registered []string) []string {
	raw, _ := r.Get("next_steps")
	steps, _ := raw.([]any)
	if len(steps) == 0 {
		return []string{"no next_steps"}
	}
	var problems []string
	for _, s := range steps {
		step, _ := s.(string)
		tool, _, called := strings.Cut(step, "(")
		if !called || !slices.Contains(registered, tool) {
			problems = append(problems, "step names no registered tool: "+step)
		}
	}
	return problems
}

func firstStep(steps any) string {
	list, _ := steps.([]any)
	if len(list) == 0 {
		return ""
	}
	s, _ := list[0].(string)
	return s
}

// exampleLog writes a log note with one open and one closed entry.
func exampleLog(t *testing.T, root string) {
	t.Helper()
	tk.Write(t, filepath.Join(root, "02 Infrastructure", "Example Log.md"), "---\ndoc_id: infra-example-log\ntitle: Example Log\n"+
		"doc_type: architecture_note\nsystem: Example\nenvironment: other\nstatus: active\nsensitivity: internal\nlast_reviewed: 2025-01-01\n---\n\n"+
		"# Example Log\n\n- 2026-10-19 · worker · repo-a · lesson one\n- 2026-10-19 · worker · repo-b · lesson two → fixed: abc123\n"+
		"- 2026-10-20 · worker · repo-c · lesson three\n")
}

// The log tool's promise end to end: list the open entries a filter names,
// close one, and a closed entry is not open.
func TestLogEntriesListAndCloseThroughTheServer(t *testing.T) {
	reg, root := labsOnly(t)
	exampleLog(t, root)
	s := connect(t, reg)
	list := map[string]any{"doc_id": "infra-example-log", "contains": "repo-a"}
	r, _ := call(t, s, "log_entries", list)
	if jsonx.Compact(tk.Get(r, "entries")) != jsonx.Compact([]any{jsonx.New("line", 14, "text", "2026-10-19 · worker · repo-a · lesson one")}) {
		t.Fatal(jsonx.Compact(r))
	}
	r, _ = call(t, s, "log_entries", map[string]any{"doc_id": "infra-example-log", "action": "close",
		"match": "lesson one", "resolution": "fixed: def456 (repo-a)"})
	if !r.Bool("ok") || !strings.HasSuffix(r.Str("text"), " → fixed: def456 (repo-a)") {
		t.Fatal(jsonx.Compact(r))
	}
	if r, _ = call(t, s, "log_entries", list); jsonx.Compact(tk.Get(r, "count")) != "0" {
		t.Fatal(jsonx.Compact(r))
	}
}

// Every failure an agent can hit in the core tools, and a search that finds
// nothing, says what to call next, with a tool the server registers.
func TestEveryFailureNamesItsNextCall(t *testing.T) {
	reg, root := labsOnly(t)
	tk.MultisectionDoc(t, root)
	exampleLog(t, root)
	s := connect(t, reg)
	registered := names(tools(t, s))
	type c = map[string]any
	cases := []struct {
		tool string
		args c
	}{
		{"find", c{"query": "nginx", "fields": []string{"nope"}}},
		{"find", c{"query": "zzqx vbnm"}},
		{"find", c{"query": "zzqx vbnm", "by": "text"}},
		{"find", c{"query": "nginx", "system": "no such system"}},
		{"read_doc", c{"doc_id": "no-such-doc"}},
		{"read_doc", c{"doc_id": "infra-local-pki"}},
		{"read_doc", c{"doc_id": "rb-backup-ops", "sections": []string{"nope"}}},
		{"read_doc", c{"doc_id": "rb-backup-ops", "sections": []string{}}},
		{"read_doc", c{"doc_id": "rb-backup-ops", "fields": []string{"nope"}}},
		{"set_frontmatter", c{"relative_path": "03 Runbooks/Missing.md", "status": "draft"}},
		{"set_frontmatter", c{"relative_path": "00 Inbox/Daily Note/2026-06-19.md", "status": "draft"}},
		{"set_frontmatter", c{"relative_path": "03 Runbooks/Add Nginx Proxy Host.md", "doc_id": "rb-other"}},
		{"set_frontmatter", c{"relative_path": "03 Runbooks/Add Nginx Proxy Host.md", "status": "bogus"}},
		{"set_frontmatter", c{"relative_path": "01 Projects/untagged.md", "title": "Untagged"}},
		{"write_doc", c{"body": "x", "doc_id": "rb-new", "title": "New", "doc_type": "runbook", "system": "x"}},
		{"write_doc", c{"body": "x", "relative_path": "03 Runbooks/Backup Ops.md", "doc_id": "rb-new", "title": "New", "doc_type": "runbook", "system": "x"}},
		{"write_doc", c{"body": "x", "relative_path": "00 Inbox/New.md", "doc_id": "rb-new", "title": "New", "doc_type": "runbook", "system": "x"}},
		{"write_doc", c{"body": "x", "relative_path": "03 Runbooks/New.md", "doc_id": "rb-new"}},
		{"write_doc", c{"body": "x", "relative_path": "03 Runbooks/New.md", "doc_id": "rb-backup-ops", "title": "New", "doc_type": "runbook", "system": "x"}},
		{"write_doc", c{"body": "x", "mode": "replace_section", "doc_id": "rb-nope", "heading": "Goal"}},
		{"write_doc", c{"body": "x", "mode": "replace_section", "doc_id": "rb-backup-ops", "heading": "Nope"}},
		{"write_doc", c{"body": "x", "mode": "replace_section", "doc_id": "rb-backup-ops"}},
		{"write_doc", c{"body": "x", "mode": "replace_section", "doc_id": "rb-backup-ops", "heading": "Troubleshooting", "title": "x"}},
		{"write_doc", c{"body": "x", "mode": "replace_section", "doc_id": "infra-local-pki", "heading": "Local PKI"}},
		{"write_doc", c{"body": "x", "mode": "append_section", "relative_path": "01 Projects/untagged.md", "heading": "Untagged"}},
		{"move_doc", c{"doc_id": "rb-nope", "to": "03 Runbooks/"}},
		{"move_doc", c{"doc_id": "rb-backup-ops", "to": "00 Inbox/"}},
		{"move_doc", c{"doc_id": "rb-backup-ops", "to": "03 Runbooks/Add Nginx Proxy Host.md"}},
		{"move_doc", c{"doc_id": "rb-backup-ops", "to": "01 Projects/Local PKI.md"}},
		{"move_doc", c{"doc_id": "rb-backup-ops", "to": "Elsewhere/"}},
		{"move_doc", c{"doc_id": "rb-backup-ops", "to": "03 Runbooks/Backup Ops"}},
		{"task_board", c{"fields": []string{"nope"}}},
		{"task_board", c{"limit": -1}},
		{"task_write", c{"action": "add"}},
		{"task_write", c{"action": "add", "title": "Example", "project": "no-such-project"}},
		{"task_write", c{"action": "status", "doc_id": "task-nope", "status": "done"}},
		{"task_write", c{"action": "delete", "doc_id": "task-nope"}},
		{"task_write", c{"action": "promote", "note_path": "00 Inbox/Missing.md", "title": "Example"}},
		{"log_entries", c{"doc_id": "no-such-log"}},
		{"log_entries", c{"doc_id": "infra-local-pki"}},
		{"log_entries", c{"doc_id": "infra-example-log", "fields": []string{"nope"}}},
		{"log_entries", c{"doc_id": "infra-example-log", "action": "close", "match": "repo-a"}},
		{"log_entries", c{"doc_id": "infra-example-log", "action": "close", "match": "lesson", "resolution": "fixed"}},
		{"log_entries", c{"doc_id": "infra-example-log", "action": "close", "match": "no such entry", "resolution": "fixed"}},
	}
	for _, tc := range cases {
		r, isErr := call(t, s, tc.tool, tc.args)
		if isErr || r.Bool("ok") || r.Bool("body_released") {
			t.Errorf("%s %v did not fail: %s", tc.tool, tc.args, jsonx.Compact(r))
			continue
		}
		if p := stepProblems(r, registered); len(p) > 0 {
			t.Errorf("%s %v: %v\n%s", tc.tool, tc.args, p, jsonx.Compact(r))
		}
	}
	// Arguments the schema refuses fail the same way, flagged as a tool error;
	// where a step is obvious, it is the call to make.
	argCases := []struct {
		tool string
		args c
		step string
	}{
		{"find", c{"query": "hq", "by": "system"}, `find(query="hq", system="hq")`},
		{"find", c{"query": "hq", "by": "project"}, `find(query="hq", project="hq")`},
		{"find", c{"query": "hq", "by": "fuzzy"}, `find(query="hq") with by one of ["relevance","text"]`},
		{"find", c{"query": "hq", "limit": "many"}, `find(query="hq") with limit as a valid integer`},
		{"find", c{"query": 7}, ""},
		{"find", c{}, `find() with query set`},
		{"find", c{"query": "hq", "vault": "nope"}, `find(query="hq") with vault `},
		{"find", c{"query": "hq", "sytem": "Example"}, `find(query="hq", system="Example")`},
		{"find", c{"query": "hq", "zzzz": 1}, `find(query="hq"): without ['zzzz']`},
		{"read_doc", c{"doc_id": "rb-backup-ops", "section": "goal"}, `read_doc(doc_id="rb-backup-ops", sections=["goal"])`},
		{"read_doc", c{"doc_id": "rb-backup-ops", "fields": "outline"}, ""},
		{"set_frontmatter", c{"relative_path": "03 Runbooks/Backup Ops.md", "titel": "x"}, `set_frontmatter(relative_path="03 Runbooks/Backup Ops.md", title="x")`},
		{"write_doc", c{"body": "x", "mode": "rewrite"}, `write_doc(body="x") with mode one of`},
		{"task_board", c{"include_all": "maybe"}, ""},
		{"task_write", c{"items": []any{c{"action": "add", "titel": "x"}}}, ""},
		{"log_entries", c{"doc_id": "infra-example-log", "action": "erase"}, ""},
	}
	for _, tc := range argCases {
		r, isErr := call(t, s, tc.tool, tc.args)
		if !isErr || r.Bool("ok") || r.Str("error") == "" || !r.Has("ok") {
			t.Errorf("%s %v is not a flagged {ok: false, error} failure: %s", tc.tool, tc.args, jsonx.Compact(r))
			continue
		}
		if p := stepProblems(r, registered); len(p) > 0 {
			t.Errorf("%s %v: %v\n%s", tc.tool, tc.args, p, jsonx.Compact(r))
		}
		if steps, _ := r.Get("next_steps"); tc.step != "" && !strings.HasPrefix(firstStep(steps), tc.step) {
			t.Errorf("%s %v: want a step starting %s\n%s", tc.tool, tc.args, tc.step, jsonx.Compact(r))
		}
	}
	// A batch reports each item's own next steps.
	r, _ := call(t, s, "task_write", c{"items": []any{c{"action": "delete", "doc_id": "task-nope"}}})
	results, _ := r.Get("results")
	if p := stepProblems(results.([]any)[0].(*jsonx.Obj), registered); len(p) > 0 {
		t.Errorf("batch item: %v\n%s", p, jsonx.Compact(r))
	}
	// A step that names an unregistered tool fails the check.
	if p := stepProblems(jsonx.New("next_steps", []any{`find_runbook(query="x")`}), registered); len(p) != 1 {
		t.Fatalf("an unregistered tool must fail: %v", p)
	}
}
