package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

const runbook = "03 Runbooks/Example Runbook.md"

func projectDoc(t *testing.T, root, rel, id string, related ...string) {
	t.Helper()
	front := "doc_id: " + id + "\ntitle: " + strings.TrimSuffix(filepath.Base(rel), ".md") + "\ndoc_type: runbook\nsystem: Example Site\n" +
		"environment: other\nstatus: active\nsensitivity: internal\nlast_reviewed: 2026-10-01"
	if len(related) > 0 {
		front += "\nrelated_projects:\n  - " + strings.Join(related, "\n  - ")
	}
	tk.Doc(t, filepath.Join(root, rel), front, "# Example\n")
}

func projectVault(t *testing.T) string {
	root := tk.Dir(t)
	projectDoc(t, root, "01 Projects/example-site/index.md", "project-example-site")
	projectDoc(t, root, "99 Archive/Projects/example-old/index.md", "project-example-old")
	return root
}

func TestARelatedProjectsEntryNamesAProjectFolder(t *testing.T) {
	root := projectVault(t)
	projectDoc(t, root, runbook, "rb-example-runbook", "example-site", "example-old", "backlog")
	if got := findings(root); len(got) != 0 {
		t.Fatalf("live, archived and backlog entries are valid: %+v", got)
	}

	projectDoc(t, root, runbook, "rb-example-runbook", "Example-Site", "example-missing")
	var msgs []string
	for _, f := range findings(root) {
		msgs = append(msgs, f.Message)
	}
	if len(msgs) != 2 || !strings.Contains(msgs[0], "'Example-Site' names no project folder (closest: example-site,") {
		t.Fatalf("doctor reports each invalid entry, case-sensitively: %v", msgs)
	}
	if _, problems, ok := core.CheckDoc(tk.Vault(root), runbook); !ok || len(problems) != 2 {
		t.Fatalf("validate reports them too: %v", problems)
	}
}

func TestNoWriteLeavesARelatedProjectsEntryThatNamesNoFolder(t *testing.T) {
	root := projectVault(t)
	projectDoc(t, root, runbook, "rb-example-runbook", "example-gone")
	l := tk.Loader(root)

	// An unrelated edit leaves the old invalid entry alone; adding one is refused.
	if r := write.SetFrontmatter(l, runbook, write.With("system", "Other Site"), schema.Labs); !r.Bool("ok") {
		t.Fatal(r.Str("error"))
	}
	r := write.SetFrontmatter(l, runbook, write.With("related_projects", []string{"example-gone", "example-sit"}), schema.Labs)
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "'example-sit' names no project folder (closest: example-site,") ||
		strings.Contains(r.Str("error"), "example-gone") {
		t.Fatalf("set_frontmatter: %v", r)
	}
	if r := write.SetFrontmatter(l, runbook, write.With("related_projects", []string{"example-site"}), schema.Labs); !r.Bool("ok") {
		t.Fatal(r.Str("error"))
	}

	created := write.CreateDoc(l, "03 Runbooks/Another.md", "# Another\n", write.With("doc_id", "rb-another", "title", "Another",
		"doc_type", "runbook", "system", "Example Site", "environment", "other", "status", "active",
		"sensitivity", "internal", "last_reviewed", "2026-10-01", "related_projects", []string{"nope"}), schema.Labs)
	if created.Bool("ok") || !strings.Contains(created.Str("error"), "'nope' names no project folder") {
		t.Fatalf("write_doc create: %v", created)
	}
}
