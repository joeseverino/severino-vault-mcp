package query_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/query"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

func note(t *testing.T, root, name, id, body string) {
	t.Helper()
	tk.Doc(t, filepath.Join(root, "03 Runbooks", name+".md"), "doc_id: "+id+"\ntitle: "+name+
		"\ndoc_type: runbook\nsystem: Example\nenvironment: other\nstatus: active\nsensitivity: internal\n"+
		"last_reviewed: 2026-01-01\ntags: [example]", "# "+name+"\n\n## Steps\n\n"+body+"\n")
}

// Text mode is a literal body search: a word ranking would drop as filler,
// such as "not", still counts, so the doc holding it ranks first and the one
// lacking it says so.
func TestATextSearchCountsTheWordNot(t *testing.T) {
	root := tk.FakeVault(t)
	note(t, root, "Trusted Certificate", "rb-trusted", "The certificate is trusted by every client.")
	note(t, root, "Untrusted Certificate", "rb-untrusted", "The certificate is not trusted by the phone.")
	hits := tk.Hits(query.SearchBody(tk.Loader(root), "certificate not trusted", 10, 0, false, nil), "hits_by_doc")
	if got := tk.IDs(hits); len(got) < 2 || got[0] != "rb-untrusted" || got[1] != "rb-trusted" {
		t.Fatalf("want the doc holding \"not\" first, got %v", got)
	}
	if hits[0].Has("missing_words") || jsonx.Compact(tk.Get(hits[1], "missing_words")) != `["not"]` {
		t.Fatalf("missing_words: %s / %s", jsonx.Compact(hits[0]), jsonx.Compact(hits[1]))
	}
}

// A long phrase no doc holds whole still finds the doc holding most of it,
// where requiring every word found nothing.
func TestATextSearchForALongPhraseFindsThePartialMatch(t *testing.T) {
	root := tk.FakeVault(t)
	note(t, root, "Bump an Image", "rb-bump", "Change the image tag in compose.yaml, then run docker compose pull.")
	r := query.SearchBody(tk.Loader(root), "bump image version pin compose pull", 10, 0, false, nil)
	if got := tk.IDs(tk.Hits(r, "hits_by_doc")); len(got) == 0 || got[0] != "rb-bump" {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(r.Str("ranking"), "missing_words") {
		t.Fatalf("ranking does not say how partial hits rank: %s", jsonx.Compact(r))
	}
}
