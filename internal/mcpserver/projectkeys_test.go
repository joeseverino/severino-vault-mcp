package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestSetFrontmatterMapsAProjectToItsQueue(t *testing.T) {
	reg, root := labsOnly(t)
	rel := "01 Projects/Example Tracker/index.md"
	tk.Doc(t, filepath.Join(root, rel),
		"doc_id: project-example-tracker\ntitle: Example Tracker\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal", "# Example Tracker\n")
	s := connect(t, reg)
	r, isErr := call(t, s, "set_frontmatter", map[string]any{"relative_path": rel,
		"repos": []any{"example-tracker", "example-tracker-ext"}, "queue_prefix": "ex"})
	if isErr || !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	cwd := filepath.Join(t.TempDir(), "example-tracker-ext")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	r, isErr = call(t, s, "queue", map[string]any{"action": "add", "cwd": cwd, "kind": "next", "text": "Write the store"})
	if isErr || r.Str("id") != "ex-1" {
		t.Fatal(jsonx.Compact(r))
	}
	other := "03 Runbooks/Add Nginx Proxy Host.md"
	r, _ = call(t, s, "set_frontmatter", map[string]any{"relative_path": other, "queue_prefix": "zz"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "only applies to docs at \"01 Projects/*/index.md\"") {
		t.Fatal(jsonx.Compact(r))
	}
}
