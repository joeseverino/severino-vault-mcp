package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/doctor"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

const driftFront = "doc_id: rb-drift\ntitle: Drift\ndoc_type: runbook\nsystem: Drift\nenvironment: other\nstatus: active\nsensitivity: internal"

// driftBody mentions every piece of the labs profile's retired structure,
// links one note by a path that names no file, and holds the near misses
// doctor leaves alone: a link by name, a path link that resolves (from the
// root, by a partial path, and from the note's folder), links in code, and
// a word that only ends in tasks/.
const driftBody = "# Drift\n\n" +
	"Reports used to go in 00 Reporting.\n" +
	"See 01 Projects/Example/tasks/old.md and the Backlog.base view.\n" +
	"Done in [[task-old-work]].\n" +
	"Broken: [[03 Runbooks/Gone Note|gone]] and ![[04 Reference/missing.png]].\n" +
	"Fine: [[Add Nginx Proxy Host]], [[03 Runbooks/Add Nginx Proxy Host#Steps|steps]], ![[Sub/diagram.png]], [[./Add Nginx Proxy Host]].\n" +
	"Code: `[[03 Runbooks/In Code]]` and the subtasks/ folder.\n\n" +
	"```\n[[03 Runbooks/Fenced]]\n```\n"

func TestDoctorFlagsRetiredStructureAndDanglingPathLinksInLiveDocs(t *testing.T) {
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
	for _, term := range []string{"'00 Reporting'", "'tasks/'", "'Backlog.base'", "'[[task-'"} {
		if !has(warnings, "mentions retired structure "+term) {
			t.Errorf("no warning for %s: %v", term, warnings)
		}
	}
	if len(warnings) != 4 {
		t.Errorf("want one warning per retired term, got %v", warnings)
	}
	if len(errs) != 2 || !has(errs, "'[[03 Runbooks/Gone Note]]'") || !has(errs, "'[[04 Reference/missing.png]]'") {
		t.Errorf("want the two dangling path links as errors, got %v", errs)
	}
	if fs := got["99 Archive/Old/Drift.md"]; len(fs) != 0 {
		t.Errorf("an archived doc was checked: %+v", fs)
	}
}

// The retired list is the profile's: a profile without one flags nothing.
func TestRetiredStructureComesFromTheProfile(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Drift.md"), driftFront, driftBody)
	p := *schema.Labs
	p.Retired = []schema.Retired{{Pattern: `Fine:`, Help: "say it another way"}}
	var warnings []string
	for _, f := range doctor.Validate(config.Load("", tk.Env(root)), &p, false).Findings {
		if f.RelativePath == "03 Runbooks/Drift.md" && f.Severity == "warning" {
			warnings = append(warnings, f.Message)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "'Fine:' (body line 7): say it another way") {
		t.Fatal(warnings)
	}
}

// History mentions what was: a report, a deprecated or archived doc and a
// closed log entry keep their retired structure without a warning, while an
// open entry is still live.
func TestRetiredStructureIsFlaggedOnlyInLivingText(t *testing.T) {
	root := tk.FakeVault(t)
	quiet := map[string]string{
		"03 Runbooks/Deprecated.md":          strings.Replace(driftFront, "status: active", "status: deprecated", 1),
		"03 Runbooks/Archived.md":            strings.Replace(driftFront, "status: active", "status: archived", 1),
		"03 Runbooks/Reports/Drift Audit.md": strings.Replace(driftFront, "doc_type: runbook", "doc_type: report\nkind: audit", 1),
	}
	n := 0
	for rel, front := range quiet {
		n++
		tk.Doc(t, filepath.Join(root, rel), strings.Replace(front, "rb-drift", "rb-drift-"+strings.Repeat("x", n), 1), driftBody)
	}
	tk.Doc(t, filepath.Join(root, "02 Infrastructure", "Lessons.md"), strings.Replace(driftFront, "rb-drift", "infra-lessons", 1),
		"# Lessons\n\n- Moved the tasks/ folder. → fixed: queue it\n- Still writing to tasks/ by hand.\n")
	var warned []string
	for _, f := range findings(root) {
		if f.Severity == "warning" {
			warned = append(warned, f.RelativePath+": "+f.Message)
		}
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "02 Infrastructure/Lessons.md: mentions retired structure 'tasks/' (body line 4)") {
		t.Fatal(warned)
	}
}
