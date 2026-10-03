package brief_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/brief"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestBriefFlagsStaleDocsAndInbox(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Write(t, filepath.Join(root, "00 Inbox", "idea.md"), "---\ndoc_id: inbox-20260620-000000\ncreated: 2026-06-20 00:00:00\n---\n\nthought\n")
	pki := filepath.Join(root, "02 Infrastructure", "Local PKI.md")
	fresh := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	tk.Write(t, pki, strings.Replace(tk.Read(t, pki), "last_reviewed: 2026-04-01", "last_reviewed: "+fresh, 1))

	r := brief.Vault(tk.Loader(root), 7, 180, 15)
	var review []string
	for _, d := range tk.Hits(r, "docs_to_review.docs") {
		review = append(review, d.Str("doc_id"))
	}
	if !r.Bool("ok") || !slices.Contains(review, "rb-add-nginx-proxy-host") || slices.Contains(review, "infra-local-pki") ||
		tk.Get(r, "inbox.count") != 1 || tk.Get(r, "recent_changes.count") != 0 {
		t.Fatal(jsonx.Compact(r))
	}
}
