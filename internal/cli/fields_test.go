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
// on a doc of a type that may carry it, and the docs then validate.
func TestEveryDeclaredFieldIsSettableThroughTheCLI(t *testing.T) {
	root := tk.FakeVault(t)
	rel := "01 Projects/Example/index.md"
	tk.Write(t, filepath.Join(root, rel), "---\ntitle: Example\n---\n\n# Example\n")
	tk.RefTargets(t, root, "03 Runbooks", schema.Labs)
	docs := []string{rel}
	for _, f := range schema.Labs.Settable() {
		target := rel
		if c := tk.Carrier(t, root, "04 Reference", schema.Labs, f); c != "" {
			target = c
			docs = append(docs, c)
		}
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
		code, out, errOut := run(t, root, "", append([]string{"update-frontmatter", target, flag}, values...)...)
		if code != 0 || !strings.Contains(out, `"ok":true`) {
			t.Fatalf("%s: %d %s %s", f.Name, code, out, errOut)
		}
	}
	for _, doc := range docs {
		if _, problems, ok := core.CheckDoc(tk.Vault(root), doc); !ok || len(problems) > 0 {
			t.Fatal(doc, problems)
		}
	}
}
