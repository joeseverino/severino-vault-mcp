package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

func TestUpdateFrontmatterMapsAProjectToItsQueue(t *testing.T) {
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/Example Tracker/index.md"),
		"doc_id: project-example-tracker\ntitle: Example Tracker\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal", "# Example Tracker\n")
	code, out, _ := run(t, root, "", "update-frontmatter", "01 Projects/Example Tracker/index.md",
		"--set-repos", "example-tracker", "example-tracker-ext", "--queue-prefix", "ex")
	if code != 0 || !strings.Contains(out, `"changed_fields":["queue_prefix","repos"]`) {
		t.Fatal(out)
	}
	cwd := filepath.Join(t.TempDir(), "example-tracker-ext")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, _ = run(t, root, "", "queue", "add", "--cwd", cwd, "--kind", "next", "--text", "Write the store")
	if code != 0 || !strings.Contains(out, `"id":"ex-1"`) {
		t.Fatal(out)
	}
}
