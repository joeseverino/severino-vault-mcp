package core_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

func vaultSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out[rel] = tk.Read(t, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMovePreviewWritesNothingIncludingArchiveAndProjectMetadata(t *testing.T) {
	root, v := projectVault(t)
	before := vaultSnapshot(t, root)
	prev := gate.Approve
	t.Cleanup(func() { gate.Approve = prev })
	gate.Approve = func(string) error { t.Fatal("a preview must not unlock restricted notes"); return nil }
	for _, to := range []string{newProject, "99 Archive/Projects/Widgetry"} {
		r := mustOK(t, core.MoveFolder(v, core.FolderArgs{From: oldProject, To: to, Archive: true, IncludeRestricted: true, DryRun: true}))
		if !r.Bool("dry_run") || r.Int("files") != 4 || !strings.Contains(jsonx.Compact(tk.Get(r, "moves")), to+"/diagram.png") {
			t.Fatal(jsonx.Compact(r))
		}
		if len(tk.Get(r, "skipped_restricted").([]string)) == 0 {
			t.Fatal("preview omitted the withheld notes")
		}
		if strings.HasPrefix(to, "99 Archive/") && len(tk.Get(r, "archive_updates").([]string)) == 0 {
			t.Fatal("archive preview omitted the status writes")
		}
		if !maps.Equal(before, vaultSnapshot(t, root)) {
			t.Fatal("preview changed vault files")
		}
	}
	preview := mustOK(t, core.MoveFolder(v, core.FolderArgs{From: oldProject, To: newProject, DryRun: true}))
	actual := mustOK(t, core.MoveFolder(v, core.FolderArgs{From: oldProject, To: newProject}))
	if jsonx.Compact(tk.Get(preview, "links")) != jsonx.Compact(tk.Get(actual, "links")) {
		t.Fatalf("preview %s; actual %s", jsonx.Compact(preview), jsonx.Compact(actual))
	}
}

func TestMoveDocPreviewValidatesButDoesNotMoveOrRewrite(t *testing.T) {
	root, v := reportVault(t)
	before := vaultSnapshot(t, root)
	if r := core.MoveDoc(v, core.MoveArgs{DocID: auditID, To: "Security/Reports/", DryRun: true}); r.Bool("ok") {
		t.Fatal("a preview accepted an ambiguous file name")
	}
	to := auditDir + "/" + auditName + ".md"
	r := mustOK(t, core.MoveDoc(v, core.MoveArgs{DocID: auditID, To: to, DryRun: true}))
	if !r.Bool("dry_run") || r.Str("from") != auditRel || r.Str("to") != to || r.Int("files") != 1 {
		t.Fatal(jsonx.Compact(r))
	}
	if !maps.Equal(before, vaultSnapshot(t, root)) {
		t.Fatal("preview changed vault files")
	}
}

func TestFailedMoveLinkWriteReturnsAUsableExactTextRepair(t *testing.T) {
	root, v := projectVault(t)
	const rel = "03 Runbooks/Widget Runbook.md"
	prev := fsx.LockWait
	t.Cleanup(func() { fsx.LockWait = prev })
	fsx.LockWait = 20 * time.Millisecond
	release := tk.HoldLock(t, root, rel)
	defer release()
	r := mustOK(t, core.MoveFolder(v, core.FolderArgs{From: oldProject, To: newProject}))
	if failed := tk.Get(r, "failed").([]string); len(failed) != 2 || !strings.HasPrefix(failed[0], rel+":") {
		t.Fatal(jsonx.Compact(r))
	}
	repairs := tk.Get(r, "repairs").([]*jsonx.Obj)
	if len(repairs) != 2 || repairs[0].Str("tool") != "set_frontmatter" || repairs[1].Str("tool") != "write_doc" {
		t.Fatal(jsonx.Compact(r))
	}
	a := tk.Get(repairs[1], "arguments").(*jsonx.Obj)
	if !utf8.ValidString(a.Str("old_text")) || !utf8.ValidString(a.Str("new_text")) {
		t.Fatal("repair contains broken UTF-8")
	}
	release()
	fmArgs := tk.Get(repairs[0], "arguments").(*jsonx.Obj)
	mustOK(t, core.SetFrontmatter(v, fmArgs.Str("relative_path"), write.With("related_projects", tk.Get(fmArgs, "related_projects").([]string))))
	const appended = "\nA new observation after the move.\n"
	tk.Append(t, root, rel, appended, true)
	args := core.WriteDocArgs{DocID: a.Str("doc_id"), Mode: a.Str("mode"), OldText: a.Str("old_text"), NewText: new(a.Str("new_text"))}
	mustOK(t, core.WriteDoc(v, args))
	if got := tk.Read(t, filepath.Join(root, rel)); !strings.HasSuffix(got, appended) {
		t.Fatal("repair dropped an outside append")
	}
	if dangling := danglingLinks(t, root, "04 Reference/Widget Secrets.md"); len(dangling) > 0 {
		t.Fatal(dangling)
	}
	if again := core.WriteDoc(v, args); again.Bool("ok") {
		t.Fatal("an already-applied repair should refuse its stale old_text")
	}
	if _, err := os.Stat(filepath.Join(root, oldProject)); err == nil {
		t.Fatal("repair repeated the move")
	}
}
