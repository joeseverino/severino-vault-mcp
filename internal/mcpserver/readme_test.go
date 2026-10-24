package mcpserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vaults"
)

const (
	readmeBegin = "<!-- tools:begin, generated from the registered tools by TestReadmeToolsTable -->\n"
	readmeEnd   = "<!-- tools:end -->\n"
)

// cell escapes the characters a Markdown table cell would read as markup.
var cell = strings.NewReplacer("|", `\|`, "<", "&lt;")

// toolsTable is the README's tool table: each registered shared tool with the
// first paragraph of its description.
func toolsTable(t *testing.T) string {
	t.Helper()
	root := tk.Dir(t)
	env := tk.Env(root, "SVMC_EDU_CONFIG", eduConfig(t, root))
	all := tools(t, connect(t, vaults.Build(context.Background(), env, io.Discard)))
	var sb strings.Builder
	sb.WriteString("| Tool | Does |\n|---|---|\n")
	for _, name := range names(all) {
		para, _, _ := strings.Cut(all[name].Description, "\n\n")
		sb.WriteString("| `" + name + "` | " + cell.Replace(strings.Join(strings.Fields(para), " ")) + " |\n")
	}
	return sb.String()
}

// The README's tool table derives from the registered tools; it never drifts
// from them. SVMC_UPDATE_README=1 rewrites it.
func TestReadmeToolsTable(t *testing.T) {
	path := filepath.Join("..", "..", "README.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	head, rest, ok := strings.Cut(text, readmeBegin)
	_, tail, ok2 := strings.Cut(rest, readmeEnd)
	if !ok || !ok2 {
		t.Fatalf("README.md has no %q ... %q block", readmeBegin, readmeEnd)
	}
	want := head + readmeBegin + toolsTable(t) + readmeEnd + tail
	if want == text {
		return
	}
	if os.Getenv("SVMC_UPDATE_README") == "1" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("README.md's tool table drifted from the registered tools; regenerate with mise run readme\n\nwant:\n%s", toolsTable(t))
}
