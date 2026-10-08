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

func TestUpdateLinkReplacesOneExactLinkAtomically(t *testing.T) {
	root, p := linkDoc(t, "internal")
	r := write.UpdateLink(tk.Loader(root), "report-link-test", "Dashboard", "https://old.example", "https://github.com/example/dashboard")
	if !r.Bool("ok") || r.Str("old_href") != "https://old.example" {
		t.Fatal(jsonx.Compact(r))
	}
	text := tk.Read(t, p)
	if !strings.Contains(text, "[Dashboard](https://github.com/example/dashboard)") || strings.Contains(text, "legacy note") {
		t.Fatal(text)
	}
}

// The Python writer rebuilt the file as text[:body_start] + body, slicing
// characters by a line number, which destroyed the frontmatter.
func TestUpdateLinkPreservesTheFrontmatterByteForByte(t *testing.T) {
	root, p := linkDoc(t, "internal")
	before := tk.Read(t, p)
	write.UpdateLink(tk.Loader(root), "report-link-test", "Dashboard", "https://old.example", "https://new.example")
	after := tk.Read(t, p)
	head, _, _ := strings.Cut(before, "Open [")
	if !strings.HasPrefix(after, head) {
		t.Fatalf("frontmatter changed:\n%s", after)
	}
	if after != head+"Open [Dashboard](https://new.example).\n" {
		t.Fatalf("unexpected body:\n%q", after)
	}
	fm, err := frontmatter.Read(p)
	if err != nil || fm.Str("doc_id") != "report-link-test" {
		t.Fatal("frontmatter no longer parses")
	}
}

func TestUpdateLinkFailsClosedOnNoMatch(t *testing.T) {
	root, p := linkDoc(t, "internal")
	before := tk.Read(t, p)
	r := write.UpdateLink(tk.Loader(root), "report-link-test", "Other", "https://old.example", "https://new.example")
	if jsonx.Compact(r) != `{"ok":false,"error":"expected exactly one matching link; found 0"}` || tk.Read(t, p) != before {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestUpdateLinkRefusesRestrictedBodies(t *testing.T) {
	root, p := linkDoc(t, "restricted")
	before := tk.Read(t, p)
	r := write.UpdateLink(tk.Loader(root), "report-link-test", "Dashboard", "https://old.example", "https://new.example")
	if jsonx.Compact(r) != `{"ok":false,"error":"restricted document bodies cannot be mutated"}` || tk.Read(t, p) != before {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestUpdateLinkRequiresAbsoluteHTTPURLs(t *testing.T) {
	root, _ := linkDoc(t, "internal")
	r := write.UpdateLink(tk.Loader(root), "report-link-test", "Dashboard", "https://old.example", "javascript:alert(1)")
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "replacement_href must be an absolute HTTP(S) URL") {
		t.Fatal(jsonx.Compact(r))
	}
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
