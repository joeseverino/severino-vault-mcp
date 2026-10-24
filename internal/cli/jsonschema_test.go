package cli

import (
	"strings"
	"testing"

	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestSchemaEmitsEachVaultsJSONSchema(t *testing.T) {
	root := tk.FakeVault(t)
	for vault, title := range map[string]string{"": `"title": "labs frontmatter"`, "edu": `"title": "education frontmatter"`} {
		args := []string{"schema", "--json-schema"}
		if vault != "" {
			args = append(args, "--vault", vault)
		}
		code, out, _ := run(t, root, "", args...)
		if code != 0 || !strings.Contains(out, title) || !strings.Contains(out, "draft/2020-12") {
			t.Fatalf("%s: %d %.200s", vault, code, out)
		}
	}
	if code, _, errOut := run(t, root, "", "schema", "--json-schema", "--vault", "nope"); code != 2 || !strings.Contains(errOut, "\"nope\" not in \"edu\", \"labs\"") {
		t.Fatalf("%d %s", code, errOut)
	}
}
