package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

const rotateRel = "03 Runbooks/Rotate Example Token.md"

func rotateArgs() core.WriteDocArgs {
	return core.WriteDocArgs{Mode: "create", RelativePath: rotateRel, Body: "## Steps\n\n1. Run `x`.\n",
		DocID: "rb-rotate-example-token",
		New:   write.With("title", "Rotate Example Token", "doc_type", "runbook", "system", "homelab")}
}

func mustOK(t *testing.T, r *jsonx.Obj) *jsonx.Obj {
	t.Helper()
	if !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	return r
}

func TestWriteDocCreatesAValidRegisteredDoc(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	r := mustOK(t, core.WriteDoc(v, rotateArgs()))
	text := tk.Read(t, filepath.Join(root, rotateRel))
	if r.Str("doc_id") != "rb-rotate-example-token" || r.Str("relative_path") != rotateRel || tk.Get(r, "wrote_bytes") != len(text) {
		t.Fatal(jsonx.Compact(r))
	}
	fm, _, _ := frontmatter.Split(text)
	if _, body := splitHead(t, text); body != "# Rotate Example Token\n\n## Steps\n\n1. Run `x`.\n" {
		t.Fatalf("%q", body)
	}
	if got, _ := fm.Get("last_reviewed"); got != write.Today() {
		t.Fatal(got)
	}
	if rel, problems, ok := core.CheckDoc(v, rotateRel); !ok || rel != rotateRel || len(problems) != 0 {
		t.Fatal(ok, problems)
	}
	if hit := core.ReadDoc(v, "rb-rotate-example-token", core.ReadArgs{}); !hit.Bool("found") {
		t.Fatal(jsonx.Compact(hit))
	}
	// A second create of the same path refuses and leaves the file alone.
	again := core.WriteDoc(v, rotateArgs())
	if again.Bool("ok") || !strings.Contains(again.Str("error"), "already exists") || tk.Read(t, filepath.Join(root, rotateRel)) != text {
		t.Fatal(jsonx.Compact(again))
	}
}

func TestWriteDocCreateKeepsAnExistingH1(t *testing.T) {
	root := tk.FakeVault(t)
	a := rotateArgs()
	a.Body = "# Own Title\n\nText.\n"
	mustOK(t, core.WriteDoc(tk.Vault(root), a))
	if _, body := splitHead(t, tk.Read(t, filepath.Join(root, rotateRel))); body != a.Body {
		t.Fatalf("%q", body)
	}
}

func TestWriteDocCreateRefusesWithoutWriting(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	cases := map[string]func(*core.WriteDocArgs){
		"status 'proposed' not in ['active', 'archived', 'deprecated', 'draft']": func(a *core.WriteDocArgs) { a.New = a.New.Set("status", "proposed") },
		"body starts with a `---` block":                                         func(a *core.WriteDocArgs) { a.Body = "---\ndoc_id: rb-x\n---\n\nText.\n" },
		"creating a doc needs system":                                            func(a *core.WriteDocArgs) { a.New = a.New.Set("system", "") },
		"doc_id 'runbook-x' must start with":                                     func(a *core.WriteDocArgs) { a.DocID = "runbook-x" },
		"doc_id 'rb-add-nginx-proxy-host' already exists":                        func(a *core.WriteDocArgs) { a.DocID = "rb-add-nginx-proxy-host" },
		"under the indexed dirs":                                                 func(a *core.WriteDocArgs) { a.RelativePath = "Elsewhere/Rotate.md" },
		"path escapes vault root":                                                func(a *core.WriteDocArgs) { a.RelativePath = "../Rotate.md" },
		"heading only applies":                                                   func(a *core.WriteDocArgs) { a.Heading = new("Steps") },
	}
	for want, mutate := range cases {
		a := rotateArgs()
		mutate(&a)
		r := core.WriteDoc(v, a)
		if r.Bool("ok") || !strings.Contains(r.Str("error"), want) {
			t.Errorf("%s: %s", want, jsonx.Compact(r))
		}
		if _, err := os.Stat(filepath.Join(root, rotateRel)); err == nil {
			t.Fatalf("%s: refused create wrote the file", want)
		}
	}
}

func sectionArgs(mode, heading, body string) core.WriteDocArgs {
	return core.WriteDocArgs{Mode: mode, Heading: &heading, Body: body, DocID: "rb-backup-ops"}
}

func splitHead(t *testing.T, text string) (string, string) {
	t.Helper()
	cut := frontmatter.BodyOffset(text)
	return text[:cut], text[cut:]
}

func TestWriteDocReplaceSectionKeepsFrontmatterButLastReviewed(t *testing.T) {
	root := tk.FakeVault(t)
	v := tk.Vault(root)
	mustOK(t, core.WriteDoc(v, rotateArgs()))
	path := filepath.Join(root, rotateRel)
	before := tk.Read(t, path)
	r := mustOK(t, core.WriteDoc(v, core.WriteDocArgs{Mode: "replace_section", RelativePath: rotateRel, Heading: new("Steps"), Body: "1. Run `y`.\n"}))
	after := tk.Read(t, path)
	if r.Str("doc_id") != "rb-rotate-example-token" || tk.Get(r, "wrote_bytes") != len(after) {
		t.Fatal(jsonx.Compact(r))
	}
	if _, body := splitHead(t, after); body != "# Rotate Example Token\n\n## Steps\n\n1. Run `y`.\n" {
		t.Fatalf("%q", body)
	}
	beforeHead, _ := splitHead(t, before)
	afterHead, _ := splitHead(t, after)
	if beforeHead != afterHead { // created today, so last_reviewed is already today
		t.Fatalf("%q\n%q", beforeHead, afterHead)
	}
}

func TestWriteDocSectionModesSpliceOnlyTheirSection(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	path := filepath.Join(root, "03 Runbooks", "Backup Ops.md")
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "## Troubleshooting", "### Nightly\n\nAt 02:00.\n\n## Troubleshooting", 1))
	original := tk.Read(t, path)
	v := tk.Vault(root)

	mustOK(t, core.WriteDoc(v, sectionArgs("replace_section", "Routine operations", "Run it weekly.")))
	text := tk.Read(t, path)
	head, body := splitHead(t, text)
	origHead, _ := splitHead(t, original)
	if head != strings.Replace(origHead, "last_reviewed: 2026-05-01", "last_reviewed: "+write.Today(), 1) {
		t.Fatalf("frontmatter changed beyond last_reviewed:\n%s", head)
	}
	want := "Overview line before any heading.\n\n## Routine operations\n\nRun it weekly.\n\n## Troubleshooting\n\nCheck the resolver logs first when latency spikes.\n"
	if body != want {
		t.Fatalf("%q", body)
	}

	mustOK(t, core.WriteDoc(v, sectionArgs("append_section", "Troubleshooting", "Then restart it.\n")))
	if _, body := splitHead(t, tk.Read(t, path)); !strings.HasSuffix(body, "latency spikes.\n\nThen restart it.\n") {
		t.Fatalf("%q", body)
	}

	mustOK(t, core.WriteDoc(v, sectionArgs("append_section", "Routine operations", "Or on demand.")))
	if _, body := splitHead(t, tk.Read(t, path)); !strings.Contains(body, "Run it weekly.\n\nOr on demand.\n\n## Troubleshooting") {
		t.Fatalf("%q", body)
	}
}

func TestWriteDocSectionStopsAtTheNextHeadingOfItsLevel(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	path := filepath.Join(root, "03 Runbooks", "Backup Ops.md")
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "## Troubleshooting", "### Nightly\n\nAt 02:00.\n\n```sh\n# not a heading\n```\n\n## Troubleshooting", 1))
	mustOK(t, core.WriteDoc(tk.Vault(root), sectionArgs("replace_section", "Nightly", "At 03:00.")))
	_, body := splitHead(t, tk.Read(t, path))
	if !strings.Contains(body, "current.\n\n### Nightly\n\nAt 03:00.\n\n## Troubleshooting") || strings.Contains(body, "not a heading") {
		t.Fatalf("%q", body)
	}
}

func TestWriteDocSectionRefusals(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	path := filepath.Join(root, "03 Runbooks", "Backup Ops.md")
	tk.Write(t, path, tk.Read(t, path)+"\n## Troubleshooting\n\nAgain.\n")
	pki := filepath.Join(root, "02 Infrastructure", "Local PKI.md")
	before, pkiBefore := tk.Read(t, path), tk.Read(t, pki)
	v := tk.Vault(root)
	for want, a := range map[string]core.WriteDocArgs{
		"no heading 'Goal' in 03 Runbooks/Backup Ops.md; pass one of ['Routine operations', 'Troubleshooting', 'Troubleshooting'] exactly": sectionArgs("replace_section", "Goal", "x"),
		"heading 'Troubleshooting' appears 2 times":                         sectionArgs("replace_section", "Troubleshooting", "x"),
		"mode 'append_section' requires heading":                            {Mode: "append_section", Body: "x", DocID: "rb-backup-ops"},
		"body is empty":                                                     sectionArgs("append_section", "Routine operations", "\n"),
		"frontmatter fields only apply to mode 'create'":                    {Mode: "replace_section", Heading: new("Routine operations"), Body: "x", DocID: "rb-backup-ops", New: write.With("status", "draft")},
		"unknown doc_id 'rb-nope'":                                          {Mode: "replace_section", Heading: new("Goal"), Body: "x", DocID: "rb-nope"},
		"no registered doc at 01 Projects/untagged.md":                      {Mode: "replace_section", Heading: new("Untagged"), Body: "x", RelativePath: "01 Projects/untagged.md"},
		"unknown mode 'rewrite'":                                            {Mode: "rewrite", Body: "x"},
		"infra-local-pki is restricted; rerun with include_restricted=True": {Mode: "replace_section", Heading: new("Local PKI"), Body: "x", DocID: "infra-local-pki"},
		"infra-local-pki is restricted and the unlock was refused: Restricted unlock is not configured": {Mode: "replace_section", Heading: new("Local PKI"), Body: "x", IncludeRestricted: true, DocID: "infra-local-pki"},
	} {
		r := core.WriteDoc(v, a)
		if r.Bool("ok") || !strings.Contains(r.Str("error"), want) {
			t.Errorf("%s: %s", want, jsonx.Compact(r))
		}
	}
	if tk.Read(t, path) != before || tk.Read(t, pki) != pkiBefore {
		t.Fatal("a refused edit wrote")
	}
}

func TestWriteDocSectionRefusesFrontmatterThatNoLongerValidates(t *testing.T) {
	root := tk.FakeVault(t)
	path := filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "status: active", "status: proposed", 1))
	before := tk.Read(t, path)
	r := core.WriteDoc(tk.Vault(root), core.WriteDocArgs{Mode: "replace_section", Heading: new("Goal"), Body: "x", DocID: "rb-add-nginx-proxy-host"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "status 'proposed' not in") || !strings.Contains(r.Str("error"), "set_frontmatter") {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, path) != before {
		t.Fatal("wrote")
	}
}
