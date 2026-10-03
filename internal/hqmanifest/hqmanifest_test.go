package hqmanifest_test

import (
	"path/filepath"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/hqmanifest"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func doc(t *testing.T, p, id, notes string) {
	tk.Write(t, p, "---\ndoc_id: "+id+"\ntitle: Example\ndoc_type: runbook\nsystem: Example\nenvironment: other\nstatus: active\n"+
		"sensitivity: internal\n"+notes+"---\n\n# Example\n")
}

func TestManifestUsesTheSharedMultilineParser(t *testing.T) {
	root := tk.Dir(t)
	doc(t, filepath.Join(root, "03 Runbooks", "Example.md"), "rb-example", "notes: >-\n  First line.\n  Second line.\n")
	r := hqmanifest.Build(root, []string{"03 Runbooks"})
	if !r.Bool("ok") || tk.Get(r, "entries.0.notes") != "First line. Second line." {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestManifestFailsClosedOnDuplicateDocIDs(t *testing.T) {
	root := tk.Dir(t)
	doc(t, filepath.Join(root, "02 Infrastructure", "Example.md"), "infra-example", "")
	doc(t, filepath.Join(root, "03 Runbooks", "Duplicate.md"), "infra-example", "")
	r := hqmanifest.Build(root, []string{"02 Infrastructure", "03 Runbooks"})
	dups, _ := r.Get("duplicates")
	if r.Bool("ok") || jsonx.Compact(dups) != `[{"doc_id":"infra-example","first":"02 Infrastructure/Example.md","second":"03 Runbooks/Duplicate.md"}]` {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestSlimWriteupEntries(t *testing.T) {
	root := tk.Dir(t)
	tk.Write(t, filepath.Join(root, "05 Writeups", "my-post", "index.md"), "---\ntitle: My Post\npublished: true\ndescription: d\ntechnologies: [go]\n---\n\nbody\n")
	r := hqmanifest.Build(root, hqmanifest.DefaultDirs([]string{"03 Runbooks"}))
	e := tk.Hits(r, "entries")
	if !r.Bool("ok") || len(e) != 1 || e[0].Str("doc_id") != "writeup-my-post" || e[0].Str("external_url") != "https://jseverino.com/portfolio/my-post/" ||
		e[0].Str("topic") != "d" || e[0].Str("status") != "active" {
		t.Fatal(jsonx.Compact(r))
	}
}
