package core_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/links"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

const (
	auditDir  = "00 Reporting/Security Audit — 2026-09-07"
	auditRel  = auditDir + "/Audit.md"
	auditID   = "report-security-audit-2026-09-07"
	auditName = "Security Audit — 2026-09-07"
)

// reportVault is a vault with two report folders that each hold an Audit.md,
// one with an evidence script beside it, and notes that link to the first.
func reportVault(t *testing.T) (string, *core.Vault) {
	t.Helper()
	root := tk.Dir(t)
	report := func(rel, id, title string) {
		tk.Doc(t, filepath.Join(root, rel), `doc_id: `+id+`
title: `+title+`
doc_type: report
kind: audit
system: Example Site
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-09-07`, "# "+title+"\n")
	}
	report(auditRel, auditID, auditName)
	tk.Write(t, filepath.Join(root, auditDir, "check_headers.py"), "print('headers')\n")
	report("00 Reporting/Network Review — 2026-08-01/Audit.md", "report-network-review-2026-08-01", "Network Review — 2026-08-01")
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Harden Example Site.md"), `doc_id: rb-harden-example-site
title: Harden Example Site
doc_type: runbook
system: Example Site
environment: other
status: active
sensitivity: internal`, "See [[Security Audit — 2026-09-07/Audit|the audit]] and [[Audit]].\n")
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Secret Notes.md"), `doc_id: rb-secret-notes
title: Secret Notes
doc_type: runbook
system: Example Site
environment: other
status: active
sensitivity: restricted`, "Mentions [[Audit]].\n")
	tk.Write(t, filepath.Join(root, "00 Inbox", "capture.md"), "---\ndoc_id: note-capture\ntitle: Capture\n---\n\nA capture.\n")
	return root, tk.Vault(root, "SVMC_INDEXED_DIRS", "00 Reporting:03 Runbooks:Security", "SVMC_READ_ONLY_DIRS", "00 Inbox")
}

func TestMoveDocRenamesANonUniqueNameThenMovesItsFolder(t *testing.T) {
	root, v := reportVault(t)
	before := tk.Read(t, filepath.Join(root, auditRel))

	// Audit.md is not a unique name, so it can't move as it is.
	refused := core.MoveDoc(v, core.MoveArgs{DocID: auditID, To: "Security/Reports/", WithFolder: true})
	if refused.Bool("ok") || !strings.Contains(refused.Str("error"), "00 Reporting/Network Review — 2026-08-01/Audit.md") {
		t.Fatal(jsonx.Compact(refused))
	}
	renamed := auditDir + "/" + auditName + ".md"
	if steps := nextSteps(refused); len(steps) == 0 || !strings.Contains(steps[0], renamed) {
		t.Fatalf("the refusal should offer the rename: %v", steps)
	}

	r := mustOK(t, core.MoveDoc(v, core.MoveArgs{DocID: auditID, To: renamed}))
	if r.Str("from") != auditRel || r.Str("to") != renamed || r.Str("doc_id") != auditID {
		t.Fatal(jsonx.Compact(r))
	}
	// The runbook's path link follows the file, its name-only link stays; the
	// restricted note is not read, only listed.
	if got := jsonx.Compact(tk.Get(r, "links")); got != `[{"relative_path":"03 Runbooks/Harden Example Site.md","count":1}]` {
		t.Fatal(got)
	}
	if got := tk.Read(t, filepath.Join(root, "03 Runbooks", "Harden Example Site.md")); !strings.Contains(got, "[[00 Reporting/Security Audit — 2026-09-07/Security Audit — 2026-09-07|the audit]] and [[Audit]].") {
		t.Fatal(got)
	}
	if got := tk.Get(r, "skipped_restricted"); !slices.Equal(got.([]string), []string{"03 Runbooks/Secret Notes.md"}) {
		t.Fatal(got)
	}

	moved := mustOK(t, core.MoveDoc(v, core.MoveArgs{DocID: auditID, To: "Security/Reports/", WithFolder: true}))
	final := "Security/Reports/" + auditName + "/" + auditName + ".md"
	if moved.Str("to") != final {
		t.Fatal(jsonx.Compact(moved))
	}
	if got := tk.Read(t, filepath.Join(root, final)); got != before {
		t.Fatalf("the move changed the doc:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "Security/Reports", auditName, "check_headers.py")); err != nil {
		t.Fatal("the evidence file stayed behind: ", err)
	}
	if _, err := os.Stat(filepath.Join(root, auditDir)); !os.IsNotExist(err) {
		t.Fatal("the old folder is still there")
	}
	hit := core.ReadDoc(v, auditID, core.ReadArgs{Fields: []string{"obsidian_path"}})
	if !strings.Contains(hit.Str("obsidian_path"), final) {
		t.Fatalf("the index still has the old path: %s", jsonx.Compact(hit))
	}
	if rel, problems, ok := core.CheckDoc(v, final); !ok || rel != final || len(problems) > 0 {
		t.Fatal(ok, problems)
	}
}

func TestMoveDocKeepsTheFileNameWhenToIsAFolder(t *testing.T) {
	root, v := reportVault(t)
	r := mustOK(t, core.MoveDoc(v, core.MoveArgs{RelativePath: "03 Runbooks/Harden Example Site.md", To: "Security/"}))
	if r.Str("to") != "Security/Harden Example Site.md" {
		t.Fatal(jsonx.Compact(r))
	}
	if got := jsonx.Compact(tk.Get(r, "links")); got != "[]" {
		t.Fatalf("a move that keeps a unique name breaks no link: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "Security", "Harden Example Site.md")); err != nil {
		t.Fatal(err)
	}
}

func TestMoveDocRefusalsLeaveTheVaultAloneAndSayWhatToDo(t *testing.T) {
	cases := map[string]struct {
		args core.MoveArgs
		want string
	}{
		"name taken":          {core.MoveArgs{DocID: "rb-harden-example-site", To: "Security/Secret Notes.md"}, "already names 03 Runbooks/Secret Notes.md"},
		"destination exists":  {core.MoveArgs{DocID: auditID, To: "00 Reporting/Network Review — 2026-08-01/Audit.md"}, "destination exists"},
		"read-only folder":    {core.MoveArgs{DocID: "rb-harden-example-site", To: "00 Inbox/"}, "read-only"},
		"read-only source":    {core.MoveArgs{RelativePath: "00 Inbox/capture.md", To: "Security/"}, "a read-only folder"},
		"outside the indexed": {core.MoveArgs{DocID: "rb-harden-example-site", To: "Elsewhere/"}, "indexed dirs"},
		"escapes":             {core.MoveArgs{DocID: "rb-harden-example-site", To: "../outside.md"}, "escapes"},
		"not a path":          {core.MoveArgs{DocID: "rb-harden-example-site", To: "Security/Harden"}, "must end in .md"},
		"no destination":      {core.MoveArgs{DocID: "rb-harden-example-site"}, "to is required"},
		"same place":          {core.MoveArgs{DocID: "rb-harden-example-site", To: "03 Runbooks/"}, "already there"},
		"unknown doc":         {core.MoveArgs{DocID: "rb-missing", To: "Security/"}, "unknown doc_id"},
		"both names":          {core.MoveArgs{DocID: auditID, RelativePath: auditRel, To: "Security/"}, "not both"},
		"neither name":        {core.MoveArgs{To: "Security/"}, "pass doc_id or relative_path"},
		"folder to a file":    {core.MoveArgs{DocID: auditID, To: "Security/Audit.md", WithFolder: true}, "give to ending in /"},
		"top-level folder":    {core.MoveArgs{DocID: "rb-harden-example-site", To: "Security/", WithFolder: true}, "top-level folder"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root, v := reportVault(t)
			before := tree(t, root)
			r := core.MoveDoc(v, c.args)
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

func TestMoveDocWithFolderRefusesAFolderHoldingOtherDocs(t *testing.T) {
	root, v := reportVault(t)
	tk.Doc(t, filepath.Join(root, auditDir, "Appendix.md"), "doc_id: report-appendix\ntitle: Appendix\ndoc_type: decision_record\nsystem: Example Site\nenvironment: other\nstatus: active\nsensitivity: internal", "")
	r := core.MoveDoc(v, core.MoveArgs{DocID: auditID, To: "Security/", WithFolder: true})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "Appendix.md") || len(nextSteps(r)) == 0 {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestMoveDocRefusesToLeaveAFieldsScope(t *testing.T) {
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "01 Projects", "example", "index.md"), `doc_id: project-example
title: Example
doc_type: architecture_note
system: Example
environment: other
status: active
sensitivity: internal
queue_prefix: ex`, "# Example\n")
	v := tk.Vault(root)
	r := core.MoveDoc(v, core.MoveArgs{DocID: "project-example", To: "03 Runbooks/Example Project.md"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "queue_prefix only applies") || len(nextSteps(r)) == 0 {
		t.Fatal(jsonx.Compact(r))
	}
}

func nextSteps(r *jsonx.Obj) []string {
	steps, _ := r.Get("next_steps")
	s, _ := steps.([]string)
	return s
}

// tree lists every file under root, so a test can see that nothing moved.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMoveDocRewritesPathLinksAndLeavesNameOnlyOnes(t *testing.T) {
	root := tk.Dir(t)
	doc := func(rel, id, title, body string) {
		tk.Doc(t, filepath.Join(root, rel), `doc_id: `+id+`
title: `+title+`
doc_type: runbook
system: Example Site
environment: other
status: active
sensitivity: internal`, body)
	}
	doc("03 Runbooks/Dev Log.md", "rb-dev-log", "Dev Log", "# Dev Log\n")
	doc("03 Runbooks/Linker.md", "rb-linker", "Linker", "Path [[03 Runbooks/Dev Log]], aliased [[03 Runbooks/Dev Log#Plan|log]], "+
		"embed ![[03 Runbooks/Dev Log.md]], name [[Dev Log]], code `[[03 Runbooks/Dev Log]]`.\n")
	v := tk.Vault(root, "SVMC_INDEXED_DIRS", "03 Runbooks:Security")

	r := mustOK(t, core.MoveDoc(v, core.MoveArgs{DocID: "rb-dev-log", To: "Security/"}))
	if got := jsonx.Compact(tk.Get(r, "links")); got != `[{"relative_path":"03 Runbooks/Linker.md","count":3}]` {
		t.Fatal(got)
	}
	want := "Path [[Security/Dev Log]], aliased [[Security/Dev Log#Plan|log]], embed ![[Security/Dev Log.md]], name [[Dev Log]], code `[[03 Runbooks/Dev Log]]`."
	if got := tk.Read(t, filepath.Join(root, "03 Runbooks", "Linker.md")); !strings.Contains(got, want) {
		t.Fatal(got)
	}
	files := links.NewFiles(links.VaultFiles(root))
	body := tk.Read(t, filepath.Join(root, "03 Runbooks", "Linker.md"))
	if bad := links.Dangling(body, "03 Runbooks/Linker.md", files); len(bad) != 0 {
		t.Fatalf("dangling after the move: %v", bad)
	}
}
