package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/doctor"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

const driftFront = "doc_id: rb-drift\ntitle: Drift\ndoc_type: runbook\nsystem: Drift\nenvironment: other\nstatus: active\nsensitivity: internal"

// driftBody links one note by a path that names no file, and holds the near
// misses doctor leaves alone: a link by name, a path link that resolves
// (from the root, by a partial path, and from the note's folder), and links
// in code.
const driftBody = "# Drift\n\n" +
	"Broken: [[03 Runbooks/Gone Note|gone]] and ![[04 Reference/missing.png]].\n" +
	"Fine: [[Add Nginx Proxy Host]], [[03 Runbooks/Add Nginx Proxy Host#Steps|steps]], ![[Sub/diagram.png]], [[./Add Nginx Proxy Host]].\n" +
	"Code: `[[03 Runbooks/In Code]]` \n\n" +
	"```\n[[03 Runbooks/Fenced]]\n```\n"

func TestDoctorFlagsDanglingPathLinksInLiveDocs(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Drift.md"), driftFront, driftBody)
	tk.Write(t, filepath.Join(root, "04 Reference", "Sub", "diagram.png"), "png")
	tk.Doc(t, filepath.Join(root, "99 Archive", "Old", "Drift.md"), strings.Replace(driftFront, "rb-drift", "rb-old-drift", 1)+"\n", driftBody)

	got := map[string][]doctor.Finding{}
	for _, f := range findings(root) {
		got[f.RelativePath] = append(got[f.RelativePath], f)
	}
	var warnings, errs []string
	for _, f := range got["03 Runbooks/Drift.md"] {
		switch f.Severity {
		case "warning":
			warnings = append(warnings, f.Message)
		case "error":
			errs = append(errs, f.Message)
		}
	}
	if len(warnings) != 0 {
		t.Errorf("want no warnings, got %v", warnings)
	}
	if len(errs) != 2 || !has(errs, "\"[[03 Runbooks/Gone Note]]\"") || !has(errs, "\"[[04 Reference/missing.png]]\"") {
		t.Errorf("want the two dangling path links as errors, got %v", errs)
	}
	if fs := got["99 Archive/Old/Drift.md"]; len(fs) != 0 {
		t.Errorf("an archived doc was checked: %+v", fs)
	}
}
