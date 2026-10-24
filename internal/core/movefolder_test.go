package core_test

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

const (
	oldProject = "01 Projects/Widgetry"
	newProject = "01 Projects/widgetworks"
	retired    = "01 Projects/Gizmo Retired"
	fenced     = "```\n[[01 Projects/Widgetry/index]]\n```\n"
)

// projectVault is a labs vault with a project (index.md, a report, an asset
// and a queue), a retired project, and notes that link into both by path,
// by partial path, by relative markdown link and by file name, and that
// name the first in related_projects.
func projectVault(t *testing.T) (string, *core.Vault) {
	t.Helper()
	root := tk.Dir(t)
	doc := func(rel, id, docType, status, sens string, related []string, body string) {
		t.Helper()
		front := "doc_id: " + id + "\ntitle: " + strings.TrimSuffix(path.Base(rel), ".md") + "\ndoc_type: " + docType +
			"\nsystem: Example Service\nenvironment: other\nstatus: " + status + "\nsensitivity: " + sens + "\nlast_reviewed: 2026-09-01"
		if docType == schema.ReportDocType {
			front += "\nkind: audit"
		}
		if len(related) > 0 {
			front += "\nrelated_projects:\n  - " + strings.Join(related, "\n  - ")
		}
		tk.Doc(t, filepath.Join(root, rel), front, body)
	}
	doc(oldProject+"/index.md", "project-widgetry", "architecture_note", "active", "internal", nil,
		"# Widgetry\n\nSee [[01 Projects/Widgetry/Reports/Widget Audit|the audit]], [the same](Reports/Widget%20Audit.md) and\n"+
			"![[01 Projects/Widgetry/diagram.png]].\n")
	doc(oldProject+"/Reports/Widget Audit.md", "report-widget-audit", schema.ReportDocType, "active", "internal", []string{"Widgetry"},
		"# Widget Audit\n\nBack to [[Widgetry/index|the project]]; steps in [the runbook](<../../../03 Runbooks/Widget Runbook.md>).\n")
	tk.Write(t, filepath.Join(root, oldProject, "diagram.png"), "png\n")
	doc("01 Projects/example-other/index.md", "project-example-other", "architecture_note", "active", "internal", nil, "# Other\n")
	doc("03 Runbooks/Widget Runbook.md", "rb-widget-runbook", "runbook", "active", "internal", []string{"Widgetry", "example-other"},
		"# Widget Runbook\n\nThe project: [[01 Projects/Widgetry/index|Widgetry]] and [[01 Projects/Widgetry/Reports/Widget Audit]].\n"+
			"| Doc | Note |\n| --- | --- |\n| [[01 Projects/Widgetry/index\\|home]] | table |\n"+
			"Also [report](<01 Projects/Widgetry/Reports/Widget Audit.md>), ![diagram](01%20Projects/Widgetry/diagram.png),\n"+
			"[[Widget Audit]] by name, `[[01 Projects/Widgetry/index]]` in code, and [[01 Projects/Gizmo Retired/index|the old one]].\n\n"+fenced)
	doc("04 Reference/Widget Secrets.md", "note-widget-secrets", "runbook", "active", "restricted", nil,
		"Keys for [[01 Projects/Widgetry/index]].\n")
	doc(retired+"/index.md", "project-gizmo-retired", "architecture_note", "active", "internal", nil, "# Gizmo Retired\n")
	doc(retired+"/Reports/Gizmo Notes.md", "report-gizmo-notes", schema.ReportDocType, "archived", "internal", nil,
		"# Gizmo Notes\n\nSee [[01 Projects/Gizmo Retired/index]] and [the runbook](../../../03%20Runbooks/Widget%20Runbook.md#widget-runbook).\n")
	v := tk.Vault(root)
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "Widgetry", Kind: "next", Text: new("Ship the widget")}))
	return root, v
}

var (
	checkWiki = regexp.MustCompile(`\[\[([^\]|#]*?)\\?(?:[|#][^\]]*)?\]\]`)
	checkMD   = regexp.MustCompile(`\]\((<[^>]*>|[^)\s]+)\)`)
	checkCode = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")
)

// danglingLinks lists every link in the vault's notes that resolves to no
// file, the way Obsidian resolves them: a wikilink by file name anywhere or
// by a trailing part of a path, a markdown link from the note's folder or
// the vault root. Code is not read, and neither are the notes in skip.
func danglingLinks(t *testing.T, root string, skip ...string) []string {
	t.Helper()
	var files, notes []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			files = append(files, strings.ToLower(filepath.ToSlash(rel)))
			if strings.HasSuffix(rel, ".md") {
				notes = append(notes, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resolves := func(target string) bool {
		target = strings.ToLower(strings.TrimSpace(target))
		for _, f := range files {
			if f == target || f == target+".md" || strings.HasSuffix(f, "/"+target) || strings.HasSuffix(f, "/"+target+".md") {
				return true
			}
		}
		return false
	}
	var out []string
	for _, rel := range notes {
		if slices.Contains(skip, rel) {
			continue
		}
		body := checkCode.ReplaceAllString(tk.Read(t, filepath.Join(root, rel)), "")
		for _, m := range checkWiki.FindAllStringSubmatch(body, -1) {
			if m[1] != "" && !resolves(m[1]) {
				out = append(out, rel+": [["+m[1]+"]]")
			}
		}
		for _, m := range checkMD.FindAllStringSubmatch(body, -1) {
			target, _, _ := strings.Cut(strings.Trim(m[1], "<>"), "#")
			if d, err := url.PathUnescape(target); err == nil {
				target = d
			}
			if !slices.Contains(files, strings.ToLower(path.Join(path.Dir(rel), target))) && !slices.Contains(files, strings.ToLower(target)) {
				out = append(out, rel+": ("+m[1]+")")
			}
		}
	}
	return out
}

func itemIDs(r *jsonx.Obj) []string {
	out := []string{}
	for _, o := range tk.Hits(r, "items") {
		out = append(out, o.Str("id"))
	}
	return out
}

func related(t *testing.T, root, rel string) []string {
	t.Helper()
	fm := frontmatter.NewNote(tk.Read(t, filepath.Join(root, rel))).FM
	v, _ := fm.Get("related_projects")
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestTheFixtureLinksResolveBeforeAnyMove(t *testing.T) {
	root, _ := projectVault(t)
	if got := danglingLinks(t, root); len(got) > 0 {
		t.Fatalf("the fixture itself has dangling links: %v", got)
	}
}

func TestRenamingAProjectFolderCarriesEveryLinkAndReference(t *testing.T) {
	root, v := projectVault(t)
	itemID := itemIDs(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Widgetry"})))[0]

	r := mustOK(t, core.MoveFolder(v, core.FolderArgs{From: oldProject, To: newProject}))
	if r.Str("to") != newProject || r.Str("from") != oldProject {
		t.Fatal(jsonx.Compact(r))
	}
	for _, rel := range []string{"index.md", "Reports/Widget Audit.md", "diagram.png", "Queue.md"} {
		if _, err := os.Stat(filepath.Join(root, newProject, rel)); err != nil {
			t.Fatalf("%s did not move: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, oldProject)); !os.IsNotExist(err) {
		t.Fatal("the old folder is still there")
	}

	// Every link resolves but the restricted note's, which was never read.
	if got := danglingLinks(t, root, "04 Reference/Widget Secrets.md"); len(got) > 0 {
		t.Fatalf("dangling links after the rename: %v", got)
	}
	if got := tk.Get(r, "skipped_restricted"); !slices.Equal(got.([]string), []string{"04 Reference/Widget Secrets.md"}) {
		t.Fatalf("skipped_restricted: %v", got)
	}
	runbookText := tk.Read(t, filepath.Join(root, "03 Runbooks", "Widget Runbook.md"))
	for _, want := range []string{
		"[[01 Projects/widgetworks/index|Widgetry]]", "[[01 Projects/widgetworks/index\\|home]]",
		"[report](<01 Projects/widgetworks/Reports/Widget Audit.md>)", "![diagram](01%20Projects/widgetworks/diagram.png)",
		"[[Widget Audit]] by name", "`[[01 Projects/Widgetry/index]]` in code", strings.TrimSuffix(fenced, "\n"),
	} {
		if !strings.Contains(runbookText, want) {
			t.Errorf("the runbook lacks %q:\n%s", want, runbookText)
		}
	}
	links := map[string]int{}
	for _, o := range tk.Hits(r, "links") {
		n, _ := o.Get("count")
		links[o.Str("relative_path")] = n.(int)
	}
	if links["03 Runbooks/Widget Runbook.md"] != 5 || links[newProject+"/Reports/Widget Audit.md"] != 1 || links[newProject+"/index.md"] != 2 {
		t.Fatalf("links: %v", links)
	}

	// The slug follows: related_projects, the queue note's title; doc_ids
	// and item ids stay.
	if got := related(t, root, "03 Runbooks/Widget Runbook.md"); !slices.Equal(got, []string{"widgetworks", "example-other"}) {
		t.Fatal(got)
	}
	if got := related(t, root, newProject+"/Reports/Widget Audit.md"); !slices.Equal(got, []string{"widgetworks"}) {
		t.Fatal(got)
	}
	queueNote := tk.Read(t, filepath.Join(root, newProject, "Queue.md"))
	qfm := frontmatter.NewNote(queueNote).FM
	if qfm.Text("doc_id") != "queue-widgetry" || qfm.Text("title") != "widgetworks Queue" {
		t.Fatalf("queue note:\n%s", queueNote)
	}
	list := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "widgetworks"}))
	if got := itemIDs(list); !slices.Equal(got, []string{itemID}) || list.Str("doc_id") != "queue-widgetry" {
		t.Fatalf("queue after the rename: %s", jsonx.Compact(list))
	}
	added := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "widgetworks", Kind: "next", Text: new("Polish the widget")}))
	if !strings.HasPrefix(added.Str("id"), "widgetry-") || tk.Get(r, "project.queue_prefix") != "widgetry" {
		t.Fatalf("new items keep the prefix: %s / %s", added.Str("id"), jsonx.Compact(r))
	}

	// Every moved doc still validates, and the index finds it where it is.
	for _, rel := range []string{newProject + "/index.md", newProject + "/Reports/Widget Audit.md", newProject + "/Queue.md", "03 Runbooks/Widget Runbook.md"} {
		if _, problems, ok := core.CheckDoc(v, rel); !ok || len(problems) > 0 {
			t.Errorf("%s: %v %v", rel, ok, problems)
		}
	}
	hit := core.ReadDoc(v, "report-widget-audit", core.ReadArgs{Fields: []string{"obsidian_path"}})
	if hit.Str("obsidian_path") != newProject+"/Reports/Widget Audit.md" {
		t.Fatal(jsonx.Compact(hit))
	}
	if got := findIDs(t, v, "widget audit", core.FindArgs{}); !slices.Contains(got, "report-widget-audit") {
		t.Fatalf("find lost the moved doc: %v", got)
	}
}

func TestArchivingAProjectFolderNeedsArchivedDocsAndKeepsItsLinks(t *testing.T) {
	root, v := projectVault(t)
	// One level deeper than it was, so its relative links out must change.
	to := "99 Archive/Projects/Gizmo Retired"
	before := tree(t, root)
	refused := core.MoveFolder(v, core.FolderArgs{From: retired, To: to})
	if refused.Bool("ok") || !strings.Contains(refused.Str("error"), retired+"/index.md has status \"active\"") {
		t.Fatal(jsonx.Compact(refused))
	}
	if steps := nextSteps(refused); len(steps) == 0 || !strings.Contains(steps[0], "archive=true") {
		t.Fatalf("the refusal should offer archive=true: %v", steps)
	}
	if after := tree(t, root); !slices.Equal(before, after) {
		t.Fatal("a refused move changed the vault")
	}

	mustOK(t, core.MoveFolder(v, core.FolderArgs{From: retired, To: to, Archive: true}))
	if got := danglingLinks(t, root, "04 Reference/Widget Secrets.md"); len(got) > 0 {
		t.Fatalf("dangling links after archiving: %v", got)
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, "03 Runbooks", "Widget Runbook.md")), "[[99 Archive/Projects/Gizmo Retired/index|the old one]]") {
		t.Fatal("the runbook's link did not follow the archive")
	}
	notes := tk.Read(t, filepath.Join(root, to, "Reports", "Gizmo Notes.md"))
	if !strings.Contains(notes, "[the runbook](../../../../03%20Runbooks/Widget%20Runbook.md#widget-runbook)") {
		t.Fatalf("the relative link out of the folder was not recomputed:\n%s", notes)
	}
	for _, rel := range []string{to + "/index.md", to + "/Reports/Gizmo Notes.md"} {
		if fm := frontmatter.NewNote(tk.Read(t, filepath.Join(root, rel))).FM; fm.Text("status") != "archived" {
			t.Fatalf("%s: %s", rel, jsonx.Compact(fm))
		}
	}
	if got := findIDs(t, v, "gizmo retired", core.FindArgs{}); slices.Contains(got, "project-gizmo-retired") {
		t.Fatalf("find returned the archived project by default: %v", got)
	}
	if got := findIDs(t, v, "gizmo retired", core.FindArgs{IncludeArchived: true}); !slices.Contains(got, "project-gizmo-retired") {
		t.Fatalf("include_archived lost the archived project: %v", got)
	}
}

func TestMoveFolderRefusalsLeaveTheVaultAlone(t *testing.T) {
	cases := map[string]struct {
		args core.FolderArgs
		want string
	}{
		"no from":           {core.FolderArgs{To: newProject}, "from is required"},
		"no to":             {core.FolderArgs{From: oldProject}, "to is required"},
		"missing folder":    {core.FolderArgs{From: "01 Projects/Nowhere", To: newProject}, "no folder at"},
		"top-level folder":  {core.FolderArgs{From: "03 Runbooks", To: "04 Reference/Runbooks"}, "not one of them"},
		"destination taken": {core.FolderArgs{From: oldProject, To: retired}, "destination exists"},
		"into itself":       {core.FolderArgs{From: oldProject, To: oldProject + "/Inner"}, "into itself"},
		"outside the dirs":  {core.FolderArgs{From: oldProject, To: "Elsewhere/Widgetry"}, "to must be a folder inside"},
		"read-only":         {core.FolderArgs{From: oldProject, To: "00 Inbox/Widgetry"}, "read-only"},
		"escapes":           {core.FolderArgs{From: oldProject, To: "../Widgetry"}, "inside the vault"},
		"field scope":       {core.FolderArgs{From: oldProject, To: "03 Runbooks/Widgetry"}, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root, v := projectVault(t)
			if name == "field scope" {
				mustOK(t, core.SetFrontmatter(v, oldProject+"/index.md", write.With("queue_prefix", "wdg")))
				c.want = "queue_prefix only applies"
			}
			before := tree(t, root)
			r := core.MoveFolder(v, c.args)
			if r.Bool("ok") || !strings.Contains(r.Str("error"), c.want) {
				t.Fatal(jsonx.Compact(r))
			}
			if len(nextSteps(r)) == 0 {
				t.Fatalf("no next_steps: %s", jsonx.Compact(r))
			}
			if after := tree(t, root); !slices.Equal(before, after) {
				t.Fatalf("a refused move changed the vault:\n%v\n%v", before, after)
			}
		})
	}
}

func TestAnInvalidRelatedProjectsEntryIsRefusedWithTheClosestFolders(t *testing.T) {
	_, v := projectVault(t)
	r := core.SetFrontmatter(v, "03 Runbooks/Widget Runbook.md", write.With("related_projects", []string{"widgetry", "Widgetry"}))
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "\"widgetry\" names no project folder (closest: Widgetry,") {
		t.Fatal(jsonx.Compact(r))
	}
	steps, _ := r.Get("next_steps")
	if s, ok := steps.([]string); !ok || len(s) != 1 || !strings.Contains(s[0], "naming a folder the error lists") {
		t.Fatal(jsonx.Compact(r))
	}
}
