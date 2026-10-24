package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/doctor"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// checkout writes a repository checkout holding one Go test, one Python
// test, a file task, a TOML task with an alias, and ignored build output.
func checkout(t *testing.T) string {
	repo := filepath.Join(tk.Dir(t), "example-repo")
	tk.Write(t, filepath.Join(repo, "internal", "real", "real_test.go"), "package real\n\nfunc TestReal(t *testing.T) {}\n")
	tk.Write(t, filepath.Join(repo, "tests", "test_real.py"), "def test_real_python():\n    pass\n")
	tk.Write(t, filepath.Join(repo, "mise.toml"), "[tasks.check]\nrun = \"true\"\nalias = \"ci\"\n")
	tk.Write(t, filepath.Join(repo, "mise-tasks", "lint", "go"), "#!/bin/sh\n")
	tk.Write(t, filepath.Join(repo, ".gitignore"), "dist/\n")
	tk.Write(t, filepath.Join(repo, "docs", "guide.md"), "# Guide\n")
	return repo
}

func projectIndex(t *testing.T, root, project, projectPath string) {
	tk.Doc(t, filepath.Join(root, "01 Projects", project, "index.md"),
		"doc_id: project-"+strings.ToLower(project)+"\ntitle: "+project+"\ndoc_type: architecture_note\nsystem: "+project+
			"\nenvironment: other\nstatus: active\nsensitivity: internal\nproject_path: "+projectPath, "# "+project+"\n")
}

func noteFront(id, status, docType string, related ...string) string {
	front := "doc_id: " + id + "\ntitle: " + id + "\ndoc_type: " + docType + "\nsystem: Example\nenvironment: other\nstatus: " + status + "\nsensitivity: internal"
	if docType == "report" {
		front += "\nkind: audit"
	}
	if len(related) > 0 {
		front += "\nrelated_projects:\n  - " + strings.Join(related, "\n  - ")
	}
	return front
}

// refsBody names, in code spans, a path, a folder, a test, a Python test and
// tasks that the checkout has, and one of each it lacks. The rest are near
// misses doctor leaves alone: prose, fenced code, URLs, home and absolute
// paths, vault paths with spaces, a lone top folder, ignored output, a
// note beside the doc, a path after a cd, a task run in another folder, and
// a path that starts with the repository's own name.
const refsBody = "# Runbook\n\n" +
	"Run `go test -run TestReal ./internal/real/...` and `pytest tests/test_real.py::test_real_python`.\n" +
	"Gate with `mise run check ::: lint:go`, or `mise run ci`; read `docs/guide.md` and `internal/real/`.\n" +
	"Gone: `internal/gone.go`, `go test -run TestGone`, `pytest -k test_gone_python`.\n" +
	"Also gone: `mise run vanished` and `docs/old/`.\n" +
	"Prose names internal/prose.go and TestProse.\n" +
	"Fine: `https://example.com/x/y.go`, `~/Code/x/y.go`, `/etc/x/y.conf`, `07 Backlog/Queue.md`, `99 Archive/Old Stuff/stale.md`, `images/`, `dist/app.js`.\n" +
	"Fine: `Notes/Sibling.md`, `cd elsewhere && scripts/gone.sh`, `mise -C elsewhere run nope`, `example-repo/docs/guide.md`.\n\n" +
	"```\nscripts/fenced.sh\nmise run fenced\n```\n"

func referenceWarnings(root, rel string) []string {
	var out []string
	for _, f := range findings(root) {
		if f.RelativePath == rel && f.Severity == "warning" && strings.Contains(f.Message, "names code") {
			out = append(out, f.Message)
		}
	}
	return out
}

func TestDoctorWarnsOnCodeALivingDocNamesThatItsCheckoutLacks(t *testing.T) {
	root := tk.FakeVault(t)
	repo := checkout(t)
	projectIndex(t, root, "Example", repo)
	tk.Doc(t, filepath.Join(root, "01 Projects", "Example", "Runbook.md"), noteFront("rb-example-refs", "active", "runbook"), refsBody)
	tk.Doc(t, filepath.Join(root, "01 Projects", "Example", "Notes", "Sibling.md"), noteFront("note-sibling", "active", "runbook"), "# Sibling\n")

	got := referenceWarnings(root, "01 Projects/Example/Runbook.md")
	if len(got) != 1 {
		t.Fatalf("want one warning for the doc, got %q", got)
	}
	msg := got[0]
	for _, gone := range []string{"'internal/gone.go' (body line 5)", "'TestGone' (body line 5)", "'test_gone_python' (body line 5)",
		"'mise run vanished' (body line 6)", "'docs/old'", "'" + repo + "'"} {
		if !strings.Contains(msg, gone) {
			t.Errorf("warning does not name %s: %s", gone, msg)
		}
	}
	for _, there := range []string{"TestReal", "test_real", "'mise run check'", "'mise run ci'", "lint:go", "docs/guide.md", "'internal/real'",
		"prose", "TestProse", "example.com", "~/Code", "/etc", "Backlog", "stale", "images", "dist", "Sibling", "scripts", "nope", "fenced"} {
		if strings.Contains(msg, there) {
			t.Errorf("warning names %s, which is there or not a reference: %s", there, msg)
		}
	}
}

// A report or a deprecated doc records the past, so the code it names is
// not checked; a draft and a doc tied by related_projects are.
func TestOnlyLivingDocsOfAProjectAreChecked(t *testing.T) {
	root := tk.FakeVault(t)
	projectIndex(t, root, "Example", checkout(t))
	gone := "# Note\n\nSee `internal/gone.go`.\n"
	tk.Doc(t, filepath.Join(root, "01 Projects", "Example", "Reports", "Audit.md"), noteFront("report-example-audit", "active", "report"), gone)
	tk.Doc(t, filepath.Join(root, "01 Projects", "Example", "Old.md"), noteFront("rb-example-old", "deprecated", "runbook"), gone)
	tk.Doc(t, filepath.Join(root, "01 Projects", "Example", "Draft.md"), noteFront("rb-example-draft", "draft", "runbook"), gone)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Related.md"), noteFront("rb-related", "active", "runbook", "Example"), gone)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Unrelated.md"), noteFront("rb-unrelated", "active", "runbook"), gone)

	for rel, want := range map[string]int{
		"01 Projects/Example/Reports/Audit.md": 0, "01 Projects/Example/Old.md": 0,
		"01 Projects/Example/Draft.md": 1, "03 Runbooks/Related.md": 1, "03 Runbooks/Unrelated.md": 0,
	} {
		if got := referenceWarnings(root, rel); len(got) != want {
			t.Errorf("%s: want %d warnings, got %q", rel, want, got)
		}
	}
}

// A project whose checkout is not on this machine is said once, at its
// index, and its docs are not checked.
func TestAMissingCheckoutIsReportedOnce(t *testing.T) {
	root := tk.FakeVault(t)
	missing := filepath.Join(tk.Dir(t), "not-cloned")
	projectIndex(t, root, "Example", missing)
	for _, name := range []string{"One", "Two"} {
		tk.Doc(t, filepath.Join(root, "01 Projects", "Example", name+".md"), noteFront("rb-example-"+strings.ToLower(name), "active", "runbook"),
			"# "+name+"\n\nSee `internal/gone.go`.\n")
	}
	var told []doctor.Finding
	for _, f := range findings(root) {
		if strings.Contains(f.Message, "names code") {
			t.Errorf("a doc of a missing checkout was checked: %+v", f)
		}
		if strings.Contains(f.Message, "is not a folder on this machine") {
			told = append(told, f)
		}
	}
	if len(told) != 1 || told[0].RelativePath != "01 Projects/Example/index.md" || !strings.Contains(told[0].Message, missing) {
		t.Fatalf("want one note at the project's index naming %s, got %+v", missing, told)
	}
}
