package mcpserver

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vaults"
)

func TestMoveDryRunThroughTheMCPClient(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/example-wiki/index.md"), "doc_id: project-example-wiki\ntitle: Example Wiki\ndoc_type: architecture_note\nsystem: Example Wiki\nsensitivity: internal", "Project overview.\n")
	s := connect(t, vaults.Build(context.Background(), tk.Env(root), io.Discard))
	for _, c := range []struct {
		tool string
		args map[string]any
		file string
	}{
		{"move_doc", map[string]any{"doc_id": "rb-add-nginx-proxy-host", "to": "02 Infrastructure/", "dry_run": true}, "03 Runbooks/Add Nginx Proxy Host.md"},
		{"move_folder", map[string]any{"from": "01 Projects/example-wiki", "to": "01 Projects/renamed-wiki", "dry_run": true}, "01 Projects/example-wiki/index.md"},
	} {
		t.Run(c.tool, func(t *testing.T) {
			before := tk.Read(t, filepath.Join(root, c.file))
			r, isErr := call(t, s, c.tool, c.args)
			if isErr || !r.Bool("ok") || !r.Bool("dry_run") || !r.Has("moves") {
				t.Fatal(r)
			}
			if tk.Read(t, filepath.Join(root, c.file)) != before {
				t.Fatal("MCP preview changed a note")
			}
		})
	}
}
