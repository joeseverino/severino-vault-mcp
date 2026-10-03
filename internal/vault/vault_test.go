package vault_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/search"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

func ids(idx *vault.Index) []string {
	var out []string
	for _, d := range idx.Docs {
		out = append(out, d.DocID)
	}
	slices.Sort(out)
	return out
}

func TestLoaderIndexesOnlyTaggedDocs(t *testing.T) {
	idx := tk.Loader(tk.FakeVault(t)).Index(false)
	if got := ids(idx); !slices.Equal(got, []string{"infra-local-pki", "rb-add-nginx-proxy-host", "report-playbook-mcp-index"}) {
		t.Fatal(got)
	}
}

func TestLoaderIndexesLocalAliases(t *testing.T) {
	idx := tk.Loader(tk.FakeVault(t)).Index(false)
	if idx.Aliases["https proxy"] != "rb-add-nginx-proxy-host" || idx.Aliases["offline ca"] != "infra-local-pki" ||
		idx.InvalidAliases["missing target"] != "rb-does-not-exist" {
		t.Fatalf("%v %v", idx.Aliases, idx.InvalidAliases)
	}
}

func configured(t *testing.T, indexed string) (string, *vault.Loader) {
	root := tk.Dir(t)
	_ = os.MkdirAll(filepath.Join(root, "Notes"), 0o755)
	cfg := filepath.Join(root, "config.toml")
	tk.Write(t, cfg, "[vault]\npath = \""+root+"\"\nindexed_dirs = "+indexed+"\n")
	return root, vault.NewLoader(config.Load("", config.Env{"SVMC_CONFIG": cfg, "SVMC_CACHE_SECONDS": "0"}))
}

const docFM = "---\ndoc_id: note-%s\ntitle: N\ndoc_type: runbook\nsystem: x\nenvironment: other\nstatus: active\nsensitivity: internal\n%s---\n\n# N\n"

func TestUnconsumedKeysLandInExtra(t *testing.T) {
	root, l := configured(t, `["Notes"]`)
	tk.Write(t, filepath.Join(root, "Notes", "a.md"), "---\ndoc_id: note-a\ntitle: A\ndoc_type: runbook\nsystem: x\n"+
		"environment: other\nstatus: active\nsensitivity: internal\nexpires: 2027-05-31\nlead_days: 30\n---\n\n# A\n")
	docs := l.Index(false).Docs
	if len(docs) != 1 || jsonx.Compact(docs[0].Extra) != `{"expires":"2027-05-31","lead_days":"30"}` {
		t.Fatal(jsonx.Compact(docs[0].Extra))
	}
}

func TestCoreFieldsNeverDuplicatedIntoExtra(t *testing.T) {
	root, l := configured(t, `["Notes"]`)
	tk.Write(t, filepath.Join(root, "Notes", "b.md"), "---\ndoc_id: note-b\ntitle: B\ndoc_type: runbook\nsystem: x\n"+
		"environment: other\nstatus: active\nsensitivity: internal\ntags: [t]\n---\n\n# B\n")
	if d := l.Index(false).Docs[0]; d.Extra.Len() != 0 {
		t.Fatal(jsonx.Compact(d.Extra))
	}
}

func TestDotIndexesVaultRootNonRecursively(t *testing.T) {
	root, l := configured(t, `[".", "Notes"]`)
	_ = os.MkdirAll(filepath.Join(root, "Hidden"), 0o755)
	tk.Write(t, filepath.Join(root, "Root.md"), "---\ndoc_id: note-root\ntitle: N\n---\n\n# N\n")
	tk.Write(t, filepath.Join(root, "Notes", "a.md"), "---\ndoc_id: note-a\ntitle: N\n---\n\n# N\n")
	tk.Write(t, filepath.Join(root, "Hidden", "b.md"), "---\ndoc_id: note-hidden\ntitle: N\n---\n\n# N\n")
	idx := l.Index(false)
	if _, ok := idx.ByDocID["note-root"]; !ok {
		t.Fatal("root doc missing")
	}
	if _, ok := idx.ByDocID["note-a"]; !ok {
		t.Fatal("named dir doc missing")
	}
	if _, ok := idx.ByDocID["note-hidden"]; ok {
		t.Fatal("unindexed subtree joined")
	}
}

func TestReferenceDocsGetASynthesizedID(t *testing.T) {
	root, l := configured(t, `["Notes"]`)
	tk.Write(t, filepath.Join(root, "Notes", "My Ref_Doc.md"), "---\ntype: reference\ntags: [x]\n---\n\nbody\n")
	d, ok := l.Index(false).ByDocID["ref-my-ref-doc"]
	if !ok || d.DocType != "reference" || d.Sensitivity != "public" {
		t.Fatalf("%+v", d)
	}
}

func TestSkipRulesForTemplatesAndUnderscoreFiles(t *testing.T) {
	root, l := configured(t, `["Notes"]`)
	tk.Write(t, filepath.Join(root, "Notes", "Templates", "t.md"), "---\ndoc_id: note-tpl\n---\n")
	tk.Write(t, filepath.Join(root, "Notes", "_hidden.md"), "---\ndoc_id: note-hidden\n---\n")
	tk.Write(t, filepath.Join(root, "Notes", "ok.md"), "---\ndoc_id: note-ok\n---\n")
	if got := ids(l.Index(false)); !slices.Equal(got, []string{"note-ok"}) {
		t.Fatal(got)
	}
}

func TestPathOrderingFollowsComponentsLikePython(t *testing.T) {
	if vault.ComparePaths("a/b.md", "a b/x.md") >= 0 {
		t.Fatal("component order: a < a b")
	}
}

func TestBestSectionPicksTheQueryMatchingSpan(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	d := tk.Loader(root).Index(false).ByDocID["rb-backup-ops"]
	sec, score := search.BestSection(d, "resolver latency")
	if sec.Slug != "troubleshooting" || score <= 0 {
		t.Fatal(sec.Slug, score)
	}
}
