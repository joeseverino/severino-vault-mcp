package write_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

func linkDoc(t *testing.T, sensitivity string) (string, string) {
	root := tk.Dir(t)
	p := filepath.Join(root, "02 Infrastructure", "doc.md")
	tk.Write(t, p, "---\ndoc_id: report-link-test\ntitle: Link Test\ndoc_type: architecture_note\nsystem: test\n"+
		"environment: other\nstatus: active\nsensitivity: "+sensitivity+"\nlast_reviewed: 2026-01-01\n---\n\n"+
		"Open [Dashboard](https://old.example \"legacy note\").\n")
	return root, p
}

func addFM(root, rel, docID, title, docType, status, env, sens string, p *schema.Profile) *jsonx.Obj {
	return write.AddFrontmatter(tk.Loader(root), rel, docID, title, docType, "gt", env, status, sens, nil, nil, nil, false, false, false, "", p)
}

func TestWritePathValidatesAgainstTheHandedProfile(t *testing.T) {
	root := tk.Dir(t)
	tk.Write(t, filepath.Join(root, "03 Runbooks", "cs6250.md"), "# CS6250\n")
	if r := addFM(root, "03 Runbooks/cs6250.md", "course-cs6250", "CS6250", "course", "active", "gatech", "internal", schema.Labs); r.Bool("ok") || !strings.Contains(r.Str("error"), "doc_type") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := addFM(root, "03 Runbooks/cs6250.md", "course-cs6250", "CS6250", "course", "active", "gatech", "internal", schema.Education); !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestAddFrontmatterTaskRetrofitRidesTheTaskContract(t *testing.T) {
	root := tk.Dir(t)
	tk.Write(t, filepath.Join(root, "07 Backlog", "loose-todo.md"), "# Loose todo\n")
	l := tk.Loader(root)
	r := write.AddFrontmatter(l, "07 Backlog/loose-todo.md", "task-loose-todo", "Loose todo", "task", "", "other", "open", "internal",
		nil, nil, nil, false, false, false, "", schema.Labs)
	if !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	text := tk.Read(t, filepath.Join(root, "07 Backlog", "loose-todo.md"))
	for _, want := range []string{"doc_type: task", "status: open", "effort: S", "priority: med"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(text, "environment:") || strings.Contains(text, "sensitivity:") {
		t.Fatal(text)
	}
	tk.Write(t, filepath.Join(root, "07 Backlog", "bad.md"), "# Bad\n")
	bad := write.AddFrontmatter(l, "07 Backlog/bad.md", "task-bad", "Bad", "task", "", "other", "deprecated", "internal",
		nil, nil, nil, false, false, false, "", schema.Labs)
	if bad.Bool("ok") || !strings.Contains(bad.Str("error"), "task lifecycle") {
		t.Fatal(jsonx.Compact(bad))
	}
}

func TestPathGuardsRefuseEscapesAndUnindexedDirs(t *testing.T) {
	root := tk.Dir(t)
	tk.Write(t, filepath.Join(root, "Elsewhere", "x.md"), "# X\n")
	l := tk.Loader(root)
	if r := write.TouchReviewed(l, "../outside.md"); !strings.Contains(r.Str("error"), "escapes vault root") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := write.TouchReviewed(l, "Elsewhere/x.md"); !strings.Contains(r.Str("error"), "outside the indexed dirs") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestEveryWriterRefusesTheReadOnlyFolders(t *testing.T) {
	root := tk.Dir(t)
	writeup := filepath.Join(root, "05 Writeups", "example-article", "index.md")
	tk.Write(t, writeup, "---\ntitle: Example Article\npublished: true\n---\n\n# Example Article\n\n## Body\n\n"+
		"See [site](https://old.example).\n")
	inbox := filepath.Join(root, "00 Inbox", "capture.md")
	tk.Write(t, inbox, "---\ndoc_id: inbox-20260101-120000\ncreated: 2026-01-01 12:00:00\n---\n\nA capture.\n")
	before := tk.Read(t, writeup) + tk.Read(t, inbox)
	l := tk.Loader(root)
	refused := func(name string, r *jsonx.Obj) {
		t.Helper()
		e := r.Str("error")
		if r.Bool("ok") || !strings.Contains(e, "read-only") || !strings.Contains(e, "'05 Writeups'") || !strings.Contains(e, "'00 Inbox'") {
			t.Fatalf("%s: %s", name, jsonx.Compact(r))
		}
	}
	refused("set_frontmatter", write.SetFrontmatter(l, "05 Writeups/example-article/index.md", write.Set{Status: new("active")}, schema.Labs))
	refused("touch_reviewed", write.TouchReviewed(l, "00 Inbox/capture.md"))
	refused("create", write.CreateDoc(l, "00 Inbox/new.md", "body\n",
		write.NewDoc{DocID: "note-new", Title: "New", DocType: "architecture_note", System: "example"}, schema.Labs))
	refused("edit_section", write.EditSection(l, "05 Writeups/example-article/index.md", "Body", "new\n", false, schema.Labs))
	if r := write.BackfillAliases(l); !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, writeup)+tk.Read(t, inbox) != before {
		t.Fatal("a read-only file changed")
	}
}

func TestBackfillAliasesSetsTitleAliasIdempotently(t *testing.T) {
	root := tk.Dir(t)
	proj := filepath.Join(root, "01 Projects", "sitedrift")
	tk.Write(t, filepath.Join(proj, "index.md"), "---\ndoc_id: project-sitedrift\ntitle: 'sitedrift: DEV/LIVE compare & SEO'\n"+
		"doc_type: decision_record\nsystem: tools\nenvironment: local_mac\nstatus: active\nsensitivity: internal\n---\n\nbody\n")
	r := write.BackfillAliases(tk.Loader(root))
	upd, _ := r.Get("updated")
	if !r.Bool("ok") || len(upd.([]string)) != 1 || upd.([]string)[0] != "01 Projects/sitedrift/index.md" {
		t.Fatal(jsonx.Compact(r))
	}
	fm, _ := frontmatter.Read(filepath.Join(proj, "index.md"))
	if jsonx.Compact(func() any { v, _ := fm.Get("aliases"); return v }()) != `["sitedrift: DEV/LIVE compare & SEO"]` {
		t.Fatal(jsonx.Compact(fm))
	}
	r2 := write.BackfillAliases(tk.Loader(root))
	if upd2, _ := r2.Get("updated"); len(upd2.([]string)) != 0 {
		t.Fatal("not idempotent")
	}
}

func TestTouchReviewedIsANoOpWhenAlreadyToday(t *testing.T) {
	root := tk.Dir(t)
	p := filepath.Join(root, "03 Runbooks", "x.md")
	tk.Write(t, p, "---\ndoc_id: rb-x\nlast_reviewed: "+write.Today()+"\n---\n\nbody\n")
	if r := write.TouchReviewed(tk.Loader(root), "03 Runbooks/x.md"); !r.Bool("no_op") {
		t.Fatal(jsonx.Compact(r))
	}
	if info, _ := os.Stat(p); info.Size() == 0 {
		t.Fatal("file emptied")
	}
}

func TestFrontmatterWritersRefuseTraversalAndEscapingSymlinks(t *testing.T) {
	root, _ := linkDoc(t, "internal")
	outside := filepath.Join(filepath.Dir(root), "outside-doc.md")
	tk.Write(t, outside, "# outside\n")
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.Symlink(outside, filepath.Join(root, "02 Infrastructure", "link.md")); err != nil {
		t.Fatal(err)
	}
	l := tk.Loader(root)
	title := "Changed"
	for _, rel := range []string{"../outside-doc.md", "02 Infrastructure/../../outside-doc.md", "02 Infrastructure/link.md"} {
		for name, r := range map[string]*jsonx.Obj{
			"update": write.UpdateFrontmatter(l, rel, write.Update{Title: &title}, schema.Labs),
			"touch":  write.TouchReviewed(l, rel),
			"set":    write.SetFrontmatter(l, rel, write.Set{Title: &title}, schema.Labs),
		} {
			if r.Bool("ok") {
				t.Fatalf("%s(%q) accepted: %s", name, rel, jsonx.Compact(r))
			}
		}
	}
	if got := tk.Read(t, outside); got != "# outside\n" {
		t.Fatalf("outside file changed: %q", got)
	}
}
