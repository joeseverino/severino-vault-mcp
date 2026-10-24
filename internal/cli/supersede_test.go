package cli

import (
	"path/filepath"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

func TestValidateReportsASupersededByThatNamesNoDoc(t *testing.T) {
	root := tk.FakeVault(t)
	rel := "03 Runbooks/Example Audit — 2026-08-01.md"
	tk.Doc(t, filepath.Join(root, rel), "doc_id: report-example-audit-2026-08-01\ntitle: Example Audit — 2026-08-01\n"+
		"doc_type: decision_record\nsystem: Example\nenvironment: other\nstatus: deprecated\nsensitivity: internal\n"+
		"superseded_by: report-example-audit-2026-10-01", "# Example Audit\n")
	code, out, _ := run(t, root, "", "validate", rel)
	if want := rel + ": superseded_by \"report-example-audit-2026-10-01\" names no doc in the vault\n"; code != 1 || out != want {
		t.Fatalf("%d %q", code, out)
	}
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Example Audit — 2026-10-01.md"), "doc_id: report-example-audit-2026-10-01\n"+
		"title: Example Audit — 2026-10-01\ndoc_type: decision_record\nsystem: Example\nenvironment: other\nstatus: active\n"+
		"sensitivity: internal", "# Example Audit\n")
	if code, out, _ := run(t, root, "", "validate", rel); code != 0 || out != "" {
		t.Fatalf("%d %q", code, out)
	}
}
