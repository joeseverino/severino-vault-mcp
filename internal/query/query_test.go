package query_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/query"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func note(t *testing.T, root, name, id, body string) {
	t.Helper()
	tk.Doc(t, filepath.Join(root, "03 Runbooks", name+".md"), "doc_id: "+id+"\ntitle: "+name+
		"\ndoc_type: runbook\nsystem: Example\nenvironment: other\nstatus: active\nsensitivity: internal\n"+
		"last_reviewed: 2026-01-01\ntags: [example]", "# "+name+"\n\n## Steps\n\n"+body+"\n")
}

// Text mode is a literal body search: a word ranking would drop as filler,
// such as "not", is still required.
func TestATextSearchRequiresTheWordNot(t *testing.T) {
	root := tk.FakeVault(t)
	note(t, root, "Trusted Certificate", "rb-trusted", "The certificate is trusted by every client.")
	note(t, root, "Untrusted Certificate", "rb-untrusted", "The certificate is not trusted by the phone.")
	got := tk.IDs(tk.Hits(query.SearchBody(tk.Loader(root), "certificate not trusted", 10, 0, false, nil), "hits_by_doc"))
	if !slices.Equal(got, []string{"rb-untrusted"}) {
		t.Fatalf("want only the doc holding \"not\", got %v", got)
	}
}
