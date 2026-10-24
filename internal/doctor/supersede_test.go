package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/doctor"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

const (
	oldReport = "04 Reference/Reports/Example Audit — 2026-08-01.md"
	newReport = "04 Reference/Reports/Example Audit — 2026-10-01.md"
)

// report writes a report at rel; docType replaces it, for a doc that is not
// one.
func report(t *testing.T, root, rel, id, status string, docType ...string) {
	t.Helper()
	title := strings.TrimSuffix(filepath.Base(rel), ".md")
	kind := "doc_type: report\nkind: audit"
	if len(docType) > 0 {
		kind = "doc_type: " + docType[0]
	}
	tk.Doc(t, filepath.Join(root, rel), "doc_id: "+id+"\ntitle: "+title+"\n"+kind+"\nsystem: Example Site\n"+
		"environment: other\nstatus: "+status+"\nsensitivity: internal\nlast_reviewed: 2026-10-01", "# "+title+"\n")
}

func findings(root string) []doctor.Finding {
	return doctor.Validate(config.Load("", tk.Env(root)), schema.Labs, false).Findings
}

func TestASupersededReportPointsAtItsSuccessor(t *testing.T) {
	root := tk.Dir(t)
	report(t, root, oldReport, "report-example-audit-2026-08-01", "deprecated")
	report(t, root, newReport, "report-example-audit-2026-10-01", "active")
	got := findings(root)
	if len(got) != 1 || got[0].RelativePath != oldReport || got[0].Severity != "warning" || !strings.Contains(got[0].Message, "superseded_by") {
		t.Fatalf("a deprecated report without a successor: %+v", got)
	}

	l := tk.Loader(root)
	refused := write.SetFrontmatter(l, oldReport, write.With("superseded_by", "report-example-audit-2026-11-01"), schema.Labs)
	if refused.Bool("ok") || !strings.Contains(refused.Str("error"), "names no doc in the vault") {
		t.Fatalf("set_frontmatter took a successor that does not exist: %v", refused.Str("error"))
	}
	if r := write.SetFrontmatter(l, oldReport, write.With("superseded_by", "report-example-audit-2026-10-01"), schema.Labs); !r.Bool("ok") {
		t.Fatal(r.Str("error"))
	}
	if got := findings(root); len(got) != 0 {
		t.Fatalf("doctor should be clean once the old report names its successor: %+v", got)
	}

	// The successor deprecated in turn leaves the old report pointing at a
	// dead end.
	if r := write.SetFrontmatter(l, newReport, write.With("status", "deprecated"), schema.Labs); !r.Bool("ok") {
		t.Fatal(r.Str("error"))
	}
	byFile := map[string]string{}
	for _, f := range findings(root) {
		byFile[f.RelativePath] += f.Severity + ": " + f.Message
	}
	if !strings.Contains(byFile[oldReport], "is deprecated, not live") || !strings.Contains(byFile[newReport], "names no superseded_by") {
		t.Fatal(byFile)
	}
}

func TestASupersededByNamingNoDocIsAnError(t *testing.T) {
	root := tk.Dir(t)
	report(t, root, oldReport, "report-example-audit-2026-08-01", "deprecated")
	tk.Doc(t, filepath.Join(root, newReport), "doc_id: report-example-audit-2026-10-01\ntitle: Example Audit\ndoc_type: report\nkind: audit\n"+
		"system: Example Site\nenvironment: other\nstatus: active\nsensitivity: internal\nsuperseded_by: report-gone", "# Example Audit\n")
	got := findings(root)
	var errs []string
	for _, f := range got {
		if f.Severity == "error" {
			errs = append(errs, f.RelativePath+": "+f.Message)
		}
	}
	if len(errs) != 1 || errs[0] != newReport+": superseded_by \"report-gone\" names no doc in the vault" {
		t.Fatalf("%+v", got)
	}
}

// The series rule keys on the type: a deprecated report needs a successor
// whatever its title says, and a deprecated doc of another type needs none,
// a date in its title included.
func TestTheSeriesRuleKeysOnTheReportType(t *testing.T) {
	root := tk.Dir(t)
	report(t, root, "04 Reference/Reports/Old Example Audit.md", "report-old-example-audit", "deprecated")
	report(t, root, "04 Reference/Example Plan — 2026-08-01.md", "note-example-plan", "deprecated", "architecture_note")
	got := findings(root)
	if len(got) != 1 || got[0].RelativePath != "04 Reference/Reports/Old Example Audit.md" || !strings.Contains(got[0].Message, "names no superseded_by") {
		t.Fatalf("%+v", got)
	}
}
