package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// new_text "" is given, and removes old_text; left out, the call is refused.
func TestWriteDocReplaceTextEmptyNewTextRemoves(t *testing.T) {
	reg, root := labsOnly(t)
	tk.MultisectionDoc(t, root)
	s := connect(t, reg)
	path := filepath.Join(root, "03 Runbooks", "Backup Ops.md")
	call := func(args map[string]any) string {
		res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "write_doc", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(*mcp.TextContent).Text
	}
	base := map[string]any{"mode": "replace_text", "doc_id": "rb-backup-ops", "old_text": " to keep things current"}
	if out := call(base); !strings.Contains(out, "requires new_text") || !strings.Contains(tk.Read(t, path), "to keep things current") {
		t.Fatal(out)
	}
	base["new_text"] = ""
	out := call(base)
	if text := tk.Read(t, path); !strings.Contains(text, "Run the daily job.\n") {
		t.Fatalf("%s\n%q", out, text)
	}
}
