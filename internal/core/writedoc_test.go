package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
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
	if got, _ := fm.Get("last_reviewed"); got != clock.TodayISO() {
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
		"status \"proposed\" not in \"active\", \"archived\", \"deprecated\", \"draft\"": func(a *core.WriteDocArgs) { a.New = a.New.Set("status", "proposed") },
		"body starts with a `---` block":                    func(a *core.WriteDocArgs) { a.Body = "---\ndoc_id: rb-x\n---\n\nText.\n" },
		"creating a doc needs system":                       func(a *core.WriteDocArgs) { a.New = a.New.Set("system", "") },
		"doc_id \"runbook-x\" must start with":              func(a *core.WriteDocArgs) { a.DocID = "runbook-x" },
		"doc_id \"rb-add-nginx-proxy-host\" already exists": func(a *core.WriteDocArgs) { a.DocID = "rb-add-nginx-proxy-host" },
		"under the indexed dirs":                            func(a *core.WriteDocArgs) { a.RelativePath = "Elsewhere/Rotate.md" },
		"path escapes vault root":                           func(a *core.WriteDocArgs) { a.RelativePath = "../Rotate.md" },
		"heading only applies":                              func(a *core.WriteDocArgs) { a.Heading = new("Steps") },
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

	mustOK(t, core.WriteDoc(v, sectionArgs("replace_section", "Routine operations", "Run it weekly.\n\n### Nightly\n\nAt 03:00.")))
	text := tk.Read(t, path)
	head, body := splitHead(t, text)
	origHead, _ := splitHead(t, original)
	if head != strings.Replace(origHead, "last_reviewed: 2026-05-01", "last_reviewed: "+clock.TodayISO(), 1) {
		t.Fatalf("frontmatter changed beyond last_reviewed:\n%s", head)
	}
	want := "Overview line before any heading.\n\n## Routine operations\n\nRun it weekly.\n\n### Nightly\n\nAt 03:00.\n\n## Troubleshooting\n\nCheck the resolver logs first when latency spikes.\n"
	if body != want {
		t.Fatalf("%q", body)
	}

	mustOK(t, core.WriteDoc(v, sectionArgs("append_section", "Troubleshooting", "Then restart it.\n")))
	if _, body := splitHead(t, tk.Read(t, path)); !strings.HasSuffix(body, "latency spikes.\n\nThen restart it.\n") {
		t.Fatalf("%q", body)
	}

	mustOK(t, core.WriteDoc(v, sectionArgs("append_section", "Routine operations", "Or on demand.")))
	if _, body := splitHead(t, tk.Read(t, path)); !strings.Contains(body, "At 03:00.\n\nOr on demand.\n\n## Troubleshooting") {
		t.Fatalf("%q", body)
	}
}

func TestWriteDocHeadingInsideAWholeSectionStopsAtTheNextHeading(t *testing.T) {
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
		"no heading \"Goal\" in 03 Runbooks/Backup Ops.md; pass a heading path or section slug from read_doc's outline, or one of \"Routine operations\", \"Troubleshooting\", \"Troubleshooting\" exactly": sectionArgs("replace_section", "Goal", "x"),
		"heading \"Troubleshooting\" appears 2 times": sectionArgs("replace_section", "Troubleshooting", "x"),
		"mode \"append_section\" requires heading":    {Mode: "append_section", Body: "x", DocID: "rb-backup-ops"},
		"body is empty": sectionArgs("append_section", "Routine operations", "\n"),
		"frontmatter fields only apply to mode \"create\"":                                              {Mode: "replace_section", Heading: new("Routine operations"), Body: "x", DocID: "rb-backup-ops", New: write.With("status", "draft")},
		"no doc has the doc_id, title or vault path \"rb-nope\"":                                        {Mode: "replace_section", Heading: new("Goal"), Body: "x", DocID: "rb-nope"},
		"no registered doc at 01 Projects/untagged.md":                                                  {Mode: "replace_section", Heading: new("Untagged"), Body: "x", RelativePath: "01 Projects/untagged.md"},
		"unknown mode \"rewrite\"":                                                                      {Mode: "rewrite", Body: "x"},
		"infra-local-pki is restricted; rerun with include_restricted=True":                             {Mode: "replace_section", Heading: new("Local PKI"), Body: "x", DocID: "infra-local-pki"},
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
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "status \"proposed\" not in") || !strings.Contains(r.Str("error"), "set_frontmatter") {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, path) != before {
		t.Fatal("wrote")
	}
}

// filler is prose long enough that two of them push an H2 past the section
// token cap, so Parse splits it at its H3s, while one stays under it.
var filler = strings.Repeat("Steady words fill this paragraph for the cap. ", 22) + "\n"

func sectionDoc(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(root, "03 Runbooks", "Section Ops.md")
	tk.Doc(t, path, "doc_id: rb-section-ops\ntitle: Section Ops\ndoc_type: runbook\nsystem: Backup\nenvironment: homelab\n"+
		"status: active\nsensitivity: internal\nlast_reviewed: 2026-05-01", body)
	return path
}

func sectionOps(mode, heading, body string) core.WriteDocArgs {
	return core.WriteDocArgs{Mode: mode, Heading: &heading, Body: body, DocID: "rb-section-ops"}
}

// big is an H2 past the token cap, so find splits it at its H3s; a heading
// still names the whole of it.
var big = "## Stages\n\n- one\n\n### Contract\n\n" + filler + "\n### Other\n\n" + filler + "\n## After\n\nkept\n"

func TestReplaceSectionReplacesExactlyWhatReadDocReturns(t *testing.T) {
	for name, tc := range map[string]struct {
		body, ref string
		keep      string // the subsection headings the new body carries again
	}{
		"small H2 with an H3":                       {"## Stages\n\n- one\n\n### Contract\n\nshort\n\n## After\n\nkept\n", "stages", "\n\n### Contract"},
		"oversized H2 with H3s, by heading text":    {big, "Stages", "\n\n### Contract\n\n### Other"},
		"oversized H2 with H3s, by its slug":        {big, "stages", "\n\n### Contract\n\n### Other"},
		"an H3 of an oversized H2, by heading path": {big, "Stages > Contract", ""},
	} {
		t.Run(name, func(t *testing.T) {
			root := tk.FakeVault(t)
			path := sectionDoc(t, root, tc.body)
			v := tk.Vault(root)
			secs := tk.Hits(core.ReadDoc(v, "rb-section-ops", core.ReadArgs{Sections: []string{tc.ref}}), "sections")
			if len(secs) != 1 {
				t.Fatalf("read_doc sections: %v", secs)
			}
			read := secs[0].Str("body")
			_, before := splitHead(t, tk.Read(t, path))
			if block, _, _ := strings.Cut(before, "## After"); tc.keep != "" && read != strings.TrimSuffix(block, "\n") {
				t.Fatalf("read_doc section %q is not the whole H2 block", read)
			}
			at := strings.Index(before, read)
			if at < 0 || strings.Count(before, read) != 1 {
				t.Fatalf("section body not found once in %q", before)
			}
			heading, _, _ := strings.Cut(read, "\n")
			mustOK(t, core.WriteDoc(v, sectionOps("replace_section", tc.ref, "NEW"+tc.keep)))
			_, after := splitHead(t, tk.Read(t, path))
			if want := before[:at] + heading + "\n\nNEW" + tc.keep + "\n\n" + strings.TrimPrefix(before[at+len(read):], "\n"); after != want {
				t.Fatalf("replace_section %s covered other than read_doc's section:\nread %q\ngot  %q\nwant %q", tc.ref, read, after, want)
			}
		})
	}
}

func TestReplaceSectionRefusesToDropSubsections(t *testing.T) {
	for name, body := range map[string]string{
		"small H2":     "## Stages\n\n- one\n\n### Contract\n\nshort\n\n### Other\n\nshort\n\n## After\n\nkept\n",
		"oversized H2": big,
	} {
		t.Run(name, func(t *testing.T) {
			root := tk.FakeVault(t)
			path := sectionDoc(t, root, body)
			before := tk.Read(t, path)
			v := tk.Vault(root)
			r := core.WriteDoc(v, sectionOps("replace_section", "Stages", "new lead\n\n### Contract\n\nkept text"))
			if r.Bool("ok") || !strings.Contains(r.Str("error"), "would drop its subsections \"Stages > Other\"") {
				t.Fatal(jsonx.Compact(r))
			}
			steps := jsonx.Compact(r)
			if !strings.Contains(steps, `read_doc(doc_id=\"rb-section-ops\", sections=[\"Stages\"])`) ||
				!strings.Contains(steps, `heading=\"Stages > Other\") to replace only that subsection`) {
				t.Fatal(steps)
			}
			if tk.Read(t, path) != before {
				t.Fatal("a refused replace wrote")
			}
			// The subsection the refusal names takes the edit on its own.
			mustOK(t, core.WriteDoc(v, sectionOps("replace_section", "Stages > Other", "NEW")))
			if _, got := splitHead(t, tk.Read(t, path)); !strings.Contains(got, "### Other\n\nNEW\n\n## After") || !strings.Contains(got, "- one\n\n### Contract") {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestAPartSlugTargetsOnlyThatPart(t *testing.T) {
	line := strings.Repeat("Part words fill one long line. ", 30)
	body := "## Stages\n\n" + line + "A\n" + line + "B\n" + line + "C\n\n## After\n\nkept\n"
	root := tk.FakeVault(t)
	path := sectionDoc(t, root, body)
	v := tk.Vault(root)
	outline := jsonx.Compact(core.ReadDoc(v, "rb-section-ops", core.ReadArgs{Fields: []string{"outline"}}))
	if !strings.Contains(outline, `"stages-part-2"`) {
		t.Fatalf("the doc does not wrap: %s", outline)
	}
	part := tk.Hits(core.ReadDoc(v, "rb-section-ops", core.ReadArgs{Sections: []string{"stages-part-2"}}), "sections")
	if len(part) != 1 || part[0].Str("body") != line+"B" {
		t.Fatalf("read_doc part: %v", part)
	}
	whole := tk.Hits(core.ReadDoc(v, "rb-section-ops", core.ReadArgs{Sections: []string{"Stages"}}), "sections")
	if len(whole) != 1 || !strings.Contains(whole[0].Str("body"), line+"A") || !strings.Contains(whole[0].Str("body"), line+"C") {
		t.Fatalf("read_doc heading: %v", whole)
	}
	mustOK(t, core.WriteDoc(v, sectionOps("replace_section", "stages-part-2", "NEW")))
	_, got := splitHead(t, tk.Read(t, path))
	if !strings.Contains(got, line+"A\nNEW\n") || !strings.Contains(got, line+"C\n\n## After") || strings.Contains(got, line+"B") {
		t.Fatalf("%q", got)
	}
}

func TestAppendSectionAddsAtTheEndOfTheWholeBlock(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"small H2":     {"## Stages\n\n- one\n\n### Contract\n\nshort\n\n## After\n\nkept\n", "## Stages\n\n- one\n\n### Contract\n\nshort\n\n- two\n\n## After\n\nkept\n"},
		"oversized H2": {big, strings.Replace(big, filler+"\n## After", filler+"\n- two\n\n## After", 1)},
	} {
		t.Run(name, func(t *testing.T) {
			root := tk.FakeVault(t)
			path := sectionDoc(t, root, tc.body)
			mustOK(t, core.WriteDoc(tk.Vault(root), sectionOps("append_section", "Stages", "- two")))
			if _, body := splitHead(t, tk.Read(t, path)); body != tc.want {
				t.Fatalf("%q", body)
			}
		})
	}
}

func TestSectionEditRefusesABodyThatRepeatsItsHeading(t *testing.T) {
	root := tk.FakeVault(t)
	path := sectionDoc(t, root, "## Stages\n\n- one\n\n## After\n\nkept\n")
	before := tk.Read(t, path)
	v := tk.Vault(root)
	for _, mode := range []string{"replace_section", "append_section"} {
		r := core.WriteDoc(v, sectionOps(mode, "Stages", "\n## Stages\n\n- two\n"))
		if r.Bool("ok") || !strings.Contains(r.Str("error"), "send the body without it") {
			t.Errorf("%s: %s", mode, jsonx.Compact(r))
		}
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused edit wrote")
	}
	// A body opening with a deeper heading of the same text is new content.
	mustOK(t, core.WriteDoc(v, sectionOps("replace_section", "Stages", "### Stages\n\n- two")))
}

func TestADuplicatedHeadingIsTargetedByItsHeadingPath(t *testing.T) {
	for name, fill := range map[string]string{"whole H2s": "short\n", "H2s split at their H3s": filler} {
		t.Run(name, func(t *testing.T) {
			root := tk.FakeVault(t)
			body := "## One\n\n### Target shape\n\nfirst\n\n### Pad\n\n" + fill + "\n## Two, owned by CI\n\n### Target shape\n\nsecond\n\n### Pad\n\n" + fill + "\n## End\n\nend\n"
			path := sectionDoc(t, root, body)
			v := tk.Vault(root)
			r := core.WriteDoc(v, sectionOps("replace_section", "Target shape", "x"))
			if r.Bool("ok") || !strings.Contains(r.Str("error"), "appears 2 times") ||
				!strings.Contains(r.Str("error"), "\"One > Target shape\", \"Two, owned by CI > Target shape\"") {
				t.Fatal(jsonx.Compact(r))
			}
			if steps := jsonx.Compact(r); !strings.Contains(steps, `heading=\"Two, owned by CI > Target shape\"`) || !strings.Contains(steps, `heading=\"One > Target shape\"`) {
				t.Fatal(steps)
			}
			mustOK(t, core.WriteDoc(v, sectionOps("replace_section", "Two, owned by CI > Target shape", "replaced")))
			_, got := splitHead(t, tk.Read(t, path))
			if want := strings.Replace(body, "second", "replaced", 1); got != want {
				t.Fatalf("%q", got)
			}
			mustOK(t, core.WriteDoc(v, sectionOps("append_section", "one-target-shape", "more")))
			if _, got := splitHead(t, tk.Read(t, path)); !strings.Contains(got, "first\n\nmore\n\n### Pad") {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestDeleteSectionRemovesTheWholeBlock(t *testing.T) {
	const doc = "Lead text.\n\n## Stages\n\n- one\n\n### Contract\n\nshort\n\n### Other\n\nmore\n\n## After\n\nkept\n"
	for name, tc := range map[string]struct{ ref, want string }{
		"an H2 with its subsections": {"Stages", "Lead text.\n\n## After\n\nkept\n"},
		"an H2 by its slug":          {"stages", "Lead text.\n\n## After\n\nkept\n"},
		"an H3 by heading path":      {"Stages > Contract", "Lead text.\n\n## Stages\n\n- one\n\n### Other\n\nmore\n\n## After\n\nkept\n"},
		"the last section":           {"After", "Lead text.\n\n## Stages\n\n- one\n\n### Contract\n\nshort\n\n### Other\n\nmore\n"},
	} {
		t.Run(name, func(t *testing.T) {
			root := tk.FakeVault(t)
			path := sectionDoc(t, root, doc)
			r := mustOK(t, core.WriteDoc(tk.Vault(root), sectionOps("delete_section", tc.ref, "")))
			text := tk.Read(t, path)
			if _, got := splitHead(t, text); got != tc.want {
				t.Fatalf("%q", got)
			}
			if r.Str("doc_id") != "rb-section-ops" || tk.Get(r, "wrote_bytes") != len(text) || !strings.Contains(text, "last_reviewed: "+clock.TodayISO()) {
				t.Fatal(jsonx.Compact(r))
			}
		})
	}
}

func TestDeleteSectionRefusals(t *testing.T) {
	line := strings.Repeat("Part words fill one long line. ", 30)
	wrapped := "## Stages\n\n" + line + "A\n" + line + "B\n" + line + "C\n\n## After\n\nkept\n"
	for name, tc := range map[string]struct{ body, ref, bodyArg, msg string }{
		"the preamble":         {"Lead.\n\n## A\n\nx\n\n## B\n\ny\n", "", "", "not a section"},
		"a missing heading":    {"## A\n\nx\n\n## B\n\ny\n", "Nope", "", "no heading \"Nope\""},
		"the only section":     {"## A\n\nx\n\n### Sub\n\ny\n", "A", "", "only section"},
		"a part slug":          {wrapped, "stages-part-2", "", "one part"},
		"a body":               {"## A\n\nx\n\n## B\n\ny\n", "A", "text", "takes no body"},
		"a duplicated heading": {"## A\n\nx\n\n## B\n\n### Same\n\ny\n\n## C\n\n### Same\n\nz\n", "Same", "", "appears 2 times"},
	} {
		t.Run(name, func(t *testing.T) {
			root := tk.FakeVault(t)
			path := sectionDoc(t, root, tc.body)
			before := tk.Read(t, path)
			r := core.WriteDoc(tk.Vault(root), sectionOps("delete_section", tc.ref, tc.bodyArg))
			if r.Bool("ok") || !strings.Contains(r.Str("error"), tc.msg) {
				t.Fatal(jsonx.Compact(r))
			}
			if tk.Read(t, path) != before {
				t.Fatal("a refused delete wrote")
			}
		})
	}
}
