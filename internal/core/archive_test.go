package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// runbook writes a labs doc at rel with status.
func runbook(t *testing.T, root, rel, id, title, status string) {
	t.Helper()
	tk.Doc(t, filepath.Join(root, rel), `doc_id: `+id+`
title: `+title+`
doc_type: runbook
system: Example Service
environment: other
status: `+status+`
sensitivity: internal
last_reviewed: 2026-09-01`, "# "+title+"\n\nRotate the example widget keys.\n")
}

func findIDs(t *testing.T, v *core.Vault, q string, a core.FindArgs) []string {
	t.Helper()
	a.By, a.Limit = core.FindModes[0], 10
	return tk.IDs(tk.Hits(core.Find(v, q, a), "hits"))
}

func TestMoveDocIntoTheArchiveTakesOnlyArchivedDocs(t *testing.T) {
	root := tk.Dir(t)
	runbook(t, root, "03 Runbooks/Rotate Widget Keys.md", "rb-rotate-widget-keys", "Rotate Widget Keys", "active")
	v := tk.Vault(root)

	refused := core.MoveDoc(v, core.MoveArgs{DocID: "rb-rotate-widget-keys", To: "99 Archive/"})
	if refused.Bool("ok") || !strings.Contains(refused.Str("error"), "status 'active'") {
		t.Fatal(jsonx.Compact(refused))
	}
	if steps := nextSteps(refused); len(steps) < 2 || !strings.Contains(steps[0], "archive=true") || !strings.Contains(steps[1], "set_frontmatter") {
		t.Fatalf("the refusal should offer archive=true and set_frontmatter: %v", steps)
	}

	r := mustOK(t, core.MoveDoc(v, core.MoveArgs{DocID: "rb-rotate-widget-keys", To: "99 Archive/", Archive: true}))
	final := "99 Archive/Rotate Widget Keys.md"
	if r.Str("to") != final {
		t.Fatal(jsonx.Compact(r))
	}
	fm := frontmatter.NewNote(tk.Read(t, filepath.Join(root, final))).FM
	if fm.Text("status") != "archived" || fm.Text("doc_id") != "rb-rotate-widget-keys" {
		t.Fatalf("archived doc frontmatter: %s", jsonx.Compact(fm))
	}

	// The archive is history: find leaves it out unless asked, read_doc
	// still serves it by doc_id.
	if got := findIDs(t, v, "rotate widget keys", core.FindArgs{}); slices.Contains(got, "rb-rotate-widget-keys") {
		t.Fatalf("find returned an archived doc by default: %v", got)
	}
	if got := findIDs(t, v, "rotate widget keys", core.FindArgs{IncludeArchived: true}); !slices.Contains(got, "rb-rotate-widget-keys") {
		t.Fatalf("include_archived did not find the archived doc: %v", got)
	}
	text := core.Find(v, "widget keys", core.FindArgs{By: "text", IncludeArchived: true})
	if got := tk.IDs(tk.Hits(text, "hits")); !slices.Contains(got, "rb-rotate-widget-keys") {
		t.Fatalf("text search with include_archived missed it: %s", jsonx.Compact(text))
	}
	if read := core.ReadDoc(v, "rb-rotate-widget-keys", core.ReadArgs{}); !read.Bool("found") {
		t.Fatal(jsonx.Compact(read))
	}
}

func TestAnOpenTaskStaysOutOfTheArchiveEvenWithArchive(t *testing.T) {
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "07 Backlog", "Fix the widget.md"), `doc_id: task-fix-the-widget
title: Fix the widget
doc_type: task
status: open
related_projects: []
effort: S
priority: med
created: 2026-09-01`, "# Fix the widget\n")
	v := tk.Vault(root)
	r := core.MoveDoc(v, core.MoveArgs{DocID: "task-fix-the-widget", To: "99 Archive/", Archive: true})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "close it") {
		t.Fatal(jsonx.Compact(r))
	}
	if steps := nextSteps(r); len(steps) == 0 || !strings.Contains(steps[0], "task_write") {
		t.Fatalf("the refusal should offer closing the task: %v", steps)
	}
}

// An archived doc that shares a doc_id with a live one never shadows it.
func TestALiveDocKeepsItsDocIDOverAnArchivedCopy(t *testing.T) {
	root := tk.Dir(t)
	runbook(t, root, "03 Runbooks/Rotate Widget Keys.md", "rb-rotate-widget-keys", "Rotate Widget Keys", "active")
	runbook(t, root, "99 Archive/Rotate Widget Keys (old).md", "rb-rotate-widget-keys", "Rotate Widget Keys (old)", "archived")
	v := tk.Vault(root)
	read := core.ReadDoc(v, "rb-rotate-widget-keys", core.ReadArgs{Fields: []string{"obsidian_path"}})
	if read.Str("obsidian_path") != "03 Runbooks/Rotate Widget Keys.md" {
		t.Fatal(jsonx.Compact(read))
	}
}
