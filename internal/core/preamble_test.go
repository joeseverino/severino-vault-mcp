package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

// heading "" names the text before the first heading, so a note opening
// with a blockquote above its first heading can be edited in place.
func TestWriteDocEditsThePreambleWithAnEmptyHeading(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	v := tk.Vault(root)
	path := filepath.Join(root, "03 Runbooks", "Backup Ops.md")
	mustOK(t, core.WriteDoc(v, sectionArgs("replace_section", "", "> Retired.\n")))
	_, body := splitHead(t, tk.Read(t, path))
	if !strings.HasPrefix(strings.TrimLeft(body, "\n"), "> Retired.\n\n## Routine operations\n") || strings.Contains(body, "Overview line") {
		t.Fatalf("%q", body)
	}
	mustOK(t, core.WriteDoc(v, sectionArgs("append_section", "", "Second line.\n")))
	_, body = splitHead(t, tk.Read(t, path))
	if !strings.HasPrefix(strings.TrimLeft(body, "\n"), "> Retired.\n\nSecond line.\n\n## Routine operations\n") {
		t.Fatalf("%q", body)
	}
}
