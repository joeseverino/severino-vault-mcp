package vault_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/search"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
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
	if got := ids(idx); !slices.Equal(got, []string{"daily-20260619", "infra-local-pki", "rb-add-nginx-proxy-host", "report-playbook-mcp-index"}) {
		t.Fatal(got)
	}
}

func TestReadOnlyFoldersAreIndexedAndMarked(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Write(t, filepath.Join(root, "05 Writeups", "example-article", "index.md"),
		"---\ntitle: An Example Article\npublished: true\n---\n\n# An Example Article\n\nbody\n")
	tk.Write(t, filepath.Join(root, "00 Inbox", "Rotate the example key.md"),
		"---\ndoc_id: inbox-20260101-120000\ncreated: 2026-01-01 12:00:00\n---\n\nRotate it.\n")
	idx := tk.Loader(root).Index(false)
	w, ok := idx.ByDocID["writeup-example-article"]
	if !ok || w.ReadOnlyDir != "05 Writeups" || w.Title != "An Example Article" || w.Sensitivity != "public" {
		t.Fatalf("%+v", w)
	}
	n := idx.ByDocID["inbox-20260101-120000"]
	if n == nil || n.ReadOnlyDir != "00 Inbox" || n.Title != "Rotate the example key" {
		t.Fatalf("%+v", n)
	}
	if idx.ByDocID["rb-add-nginx-proxy-host"].ReadOnlyDir != "" {
		t.Fatal("an indexed doc marked read-only")
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

func TestABlockWithoutDocIDIsNotIndexed(t *testing.T) {
	root, l := configured(t, `["Notes"]`)
	tk.Write(t, filepath.Join(root, "Notes", "My Ref_Doc.md"), "---\ntype: reference\ntags: [x]\n---\n\nbody\n")
	tk.Write(t, filepath.Join(root, "Notes", "ok.md"), "---\ndoc_id: note-ok\n---\n")
	if got := ids(l.Index(false)); !slices.Equal(got, []string{"note-ok"}) {
		t.Fatal(got)
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

func TestIndexMembershipDoesNotDependOnPATHOrGitIgnores(t *testing.T) {
	root, l := configured(t, `["Notes"]`)
	for rel, id := range map[string]string{
		"visible.md": "visible", "ignored.md": "ignored", "sub/visible.md": "nested",
		".hidden.md": "hidden", ".private/note.md": "private", "Templates/note.md": "template",
		"source/note.md": "source", "_hidden.md": "underscore",
	} {
		tk.Doc(t, filepath.Join(root, "Notes", rel), "doc_id: "+id, "body\n")
	}
	tk.Write(t, filepath.Join(root, "Notes", ".ignore"), "ignored.md\n")
	if err := os.Mkdir(filepath.Join(root, "Notes", "directory.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	withTools := ids(l.Index(true))
	t.Setenv("PATH", t.TempDir())
	withoutTools := ids(l.Index(true))
	want := []string{"ignored", "nested", "visible"}
	if !slices.Equal(withTools, want) || !slices.Equal(withoutTools, want) {
		t.Fatalf("with tools %v; without tools %v; want %v", withTools, withoutTools, want)
	}
}

func TestRootIndexSkipsHiddenFilesDirectoriesAndNestedNotes(t *testing.T) {
	root, l := configured(t, `["."]`)
	for _, name := range []string{"visible.md", ".hidden.md", "_hidden.md", "nested/inside.md"} {
		tk.Doc(t, filepath.Join(root, name), "doc_id: "+name, "body\n")
	}
	if err := os.Mkdir(filepath.Join(root, "directory.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ids(l.Index(true)); !slices.Equal(got, []string{"visible.md"}) {
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
	hits := search.Rank([]*vault.Doc{d}, "resolver latency", 1)
	if len(hits) != 1 || hits[0].Section == nil || hits[0].Section.Slug != "troubleshooting" {
		t.Fatalf("%+v", hits)
	}
}

func TestComparePathsOrdersByComponent(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"a/b", "a/b", 0},
		{"a", "a/b", -1},
		{"a/b", "a", 1},
		{"a-b/c", "a/b", 1},
		{"a/b", "a-b/c", -1},
		{"", "a", -1},
		{"a//b", "a/b", -1},
	} {
		if got := vault.ComparePaths(c.a, c.b); got != c.want {
			t.Errorf("ComparePaths(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func FuzzComparePaths(f *testing.F) {
	for _, seed := range [][2]string{{"a/b", "a"}, {"a-b/c", "a/b"}, {"", "/"}, {"x//y", "x/y"}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		want := slices.Compare(strings.Split(a, "/"), strings.Split(b, "/"))
		if got := vault.ComparePaths(a, b); got != want {
			t.Fatalf("ComparePaths(%q, %q) = %d, want %d", a, b, got, want)
		}
	})
}

func BenchmarkIndexBuild(b *testing.B) {
	root := tk.FakeVault(b)
	tk.MultisectionDoc(b, root)
	loader := tk.Loader(root)
	for b.Loop() {
		loader.Index(true)
	}
}

// Callers sharing a loader build and read one index at a time (run under
// -race).
func TestLoaderIsSafeForConcurrentUse(t *testing.T) {
	l := tk.Loader(tk.FakeVault(t))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if idx := l.Index(i%2 == 0); len(idx.Docs) == 0 {
				t.Error("empty index")
			}
			_ = l.Err()
		})
	}
	wg.Wait()
}
