package cli

import (
	"path/filepath"
	"strings"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

func TestLogEntriesClosesOneEntryAndExitsOnItsResult(t *testing.T) {
	root := tk.FakeVault(t)
	rel := "02 Infrastructure/Example Log.md"
	tk.Doc(t, filepath.Join(root, rel), "doc_id: infra-example-log\ntitle: Example Log\ndoc_type: architecture_note\n"+
		"system: Example\nenvironment: other\nstatus: active\nsensitivity: internal\nlast_reviewed: 2025-01-01",
		"# Example Log\n\n- repo-a · lesson one\n- repo-b · lesson two\n")
	if code, out, _ := run(t, root, "", "log-entries", "close", "infra-example-log", "--match", "lesson", "--resolution", "x"); code != 1 ||
		!strings.Contains(out, `"candidates"`) {
		t.Fatal(code, out)
	}
	if code, out, _ := run(t, root, "", "log-entries", "close", "infra-example-log", "--match", "repo-a", "--resolution", "fixed: def456"); code != 0 {
		t.Fatal(code, out)
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, rel)), "\n- repo-a · lesson one → fixed: def456\n") {
		t.Fatal(tk.Read(t, filepath.Join(root, rel)))
	}
	code, out, _ := run(t, root, "", "log-entries", "list", "infra-example-log", "--fields", "text")
	if code != 0 || !strings.Contains(out, `"count":1`) || strings.Contains(out, "lesson one") {
		t.Fatal(code, out)
	}
	if code, out, _ := run(t, root, "", "log-entries", "annotate", "infra-example-log", "--match", "repo-a", "--reopen"); code != 0 ||
		!strings.Contains(tk.Read(t, filepath.Join(root, rel)), "\n- repo-a · lesson one\n") {
		t.Fatal(code, out, tk.Read(t, filepath.Join(root, rel)))
	}
}
