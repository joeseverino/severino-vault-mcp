package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// Every field the labs profile declares is set through update-frontmatter,
// and the doc then validates.
func TestEveryDeclaredFieldIsSettableThroughTheCLI(t *testing.T) {
	root := tk.FakeVault(t)
	rel := "01 Projects/Example/index.md"
	tk.Write(t, filepath.Join(root, rel), "---\ntitle: Example\n---\n\n# Example\n")
	tk.RefTargets(t, root, "03 Runbooks", schema.Labs)
	for _, f := range schema.Labs.Settable() {
		flag := "--" + strings.ReplaceAll(f.Name, "_", "-")
		var values []string
		switch v := tk.Sample(f).(type) {
		case []any:
			flag = "--set-" + strings.ReplaceAll(f.Name, "_", "-")
			for _, item := range v {
				values = append(values, fmt.Sprint(item))
			}
		default:
			values = []string{fmt.Sprint(v)}
		}
		code, out, errOut := run(t, root, "", append([]string{"update-frontmatter", rel, flag}, values...)...)
		if code != 0 || !strings.Contains(out, `"ok":true`) {
			t.Fatalf("%s: %d %s %s", f.Name, code, out, errOut)
		}
	}
	if _, problems, ok := core.CheckDoc(tk.Vault(root), rel); !ok || len(problems) > 0 {
		t.Fatal(problems)
	}
}
