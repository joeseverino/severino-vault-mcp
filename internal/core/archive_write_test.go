package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

const archivedRel = "99 Archive/Old/Archived Runbook.md"

func archivedFront(status string) string {
	return "doc_id: rb-archived-runbook\ntitle: Archived Runbook\ndoc_type: runbook\nsystem: Widget\nenvironment: other\nstatus: " + status + "\nsensitivity: internal"
}

// An archived doc is maintained through set_frontmatter and write_doc like
// any other, as long as it stays archived.
func TestAnArchivedDocTakesWritesThatKeepItArchived(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	tk.Doc(t, filepath.Join(root, archivedRel), archivedFront("archived"), "# Archived Runbook\n\n## Notes\n\nold\n")

	mustOK(t, core.SetFrontmatter(v, archivedRel, write.With("title", "Archived Widget Runbook")))
	mustOK(t, core.WriteDoc(v, core.WriteDocArgs{Mode: "append_section", RelativePath: archivedRel, Heading: new("Notes"), Body: "why it retired"}))
	mustOK(t, core.WriteDoc(v, core.WriteDocArgs{Mode: "replace_section", DocID: "rb-archived-runbook", Heading: new("Notes"), Body: "replaced"}))

	r := core.SetFrontmatter(v, archivedRel, write.With("status", "active"))
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "status: archived") {
		t.Fatal(jsonx.Compact(r))
	}
	if note := tk.Read(t, filepath.Join(root, archivedRel)); !strings.Contains(note, "title: Archived Widget Runbook") || !strings.Contains(note, "\nreplaced\n") || !strings.Contains(note, "status: archived") {
		t.Fatal(note)
	}
}

// A doc in the archive that is not archived is refused, unless the same
// call sets status: archived.
func TestAnUnarchivedDocInTheArchiveTakesOnlyTheCallThatArchivesIt(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	tk.Doc(t, filepath.Join(root, archivedRel), archivedFront("active"), "# Archived Runbook\n\n## Notes\n\nold\n")

	if r := core.SetFrontmatter(v, archivedRel, write.With("title", "Still Active")); r.Bool("ok") || !strings.Contains(r.Str("error"), "status: archived") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.WriteDoc(v, core.WriteDocArgs{Mode: "append_section", RelativePath: archivedRel, Heading: new("Notes"), Body: "x"}); r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	mustOK(t, core.SetFrontmatter(v, archivedRel, write.With("status", "archived", "title", "Retired Runbook")))
}

// An archive note without frontmatter is registered with doc_id, title,
// doc_type and status archived, and no system; without status archived it
// stays refused.
func TestSetFrontmatterRegistersAnArchiveNote(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	path := filepath.Join(root, "99 Archive", "Old", "Loose Note.md")
	tk.Write(t, path, "# Loose Note\n\nkept for the record\n")
	fields := write.With("doc_id", "note-loose", "title", "Loose Note", "doc_type", "decision_record")

	if r := core.SetFrontmatter(v, "99 Archive/Old/Loose Note.md", fields); r.Bool("ok") || !strings.Contains(r.Str("error"), "status") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.SetFrontmatter(v, "99 Archive/Old/Loose Note.md", fields.Set("status", "draft")); r.Bool("ok") || !strings.Contains(r.Str("error"), "status: archived") {
		t.Fatal(jsonx.Compact(r))
	}
	mustOK(t, core.SetFrontmatter(v, "99 Archive/Old/Loose Note.md", fields.Set("status", "archived")))
	if note := tk.Read(t, path); !strings.Contains(note, "doc_id: note-loose") || strings.Contains(note, "system:") {
		t.Fatal(note)
	}
	if r := core.Find(v, "loose note", core.FindArgs{By: "relevance", IncludeArchived: true}); !strings.Contains(jsonx.Compact(r), "note-loose") {
		t.Fatal(jsonx.Compact(r))
	}
}

// A new doc never takes the doc_id an archived doc holds: the archived one
// would stop resolving by its id.
func TestANewDocCannotTakeAnArchivedDocsID(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	tk.Doc(t, filepath.Join(root, archivedRel), archivedFront("archived"), "# Archived Runbook\n")

	r := core.WriteDoc(v, core.WriteDocArgs{Mode: "create", RelativePath: "03 Runbooks/Archived Runbook.md", Body: "## Steps\n\n1. Run `x`.\n",
		DocID: "rb-archived-runbook", New: write.With("title", "Archived Runbook Again", "doc_type", "runbook", "system", "homelab")})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), `doc_id "rb-archived-runbook" already exists at `+archivedRel) {
		t.Fatal(jsonx.Compact(r))
	}
}
