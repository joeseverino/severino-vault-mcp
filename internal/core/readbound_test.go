package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

// longDoc writes a doc whose body is exactly chars characters, multibyte
// ones among them so a byte count would cross the bound first: a lead, then
// H2 sections, the last padded to the length. It ends without a newline, so
// the index's body is the same text.
func longDoc(t *testing.T, root string, chars int) string {
	t.Helper()
	body := "Lead says what the doc is for.\n\n## Install\n\nStep one, café.\n\n## Operate\n\nDaily work.\n\n## Archive\n\n"
	pad := chars - utf8.RuneCountInString(body)
	body += strings.Repeat("é", pad%10) + strings.Repeat("\nOps notes", pad/10)
	if utf8.RuneCountInString(body) != chars {
		t.Fatalf("body is %d chars, want %d", utf8.RuneCountInString(body), chars)
	}
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Long Ops.md"), `
doc_id: rb-long-ops
title: Long Ops
doc_type: runbook
system: Example
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
`, body)
	return body
}

func TestReadDocReturnsABodyAtTheBoundWhole(t *testing.T) {
	root := tk.FakeVault(t)
	body := longDoc(t, root, core.BoundedBodyChars)
	r := core.ReadDoc(tk.Vault(root), "rb-long-ops", core.ReadArgs{})
	if strings.TrimSuffix(r.Str("body"), "\n") != strings.TrimSuffix(body, "\n") || r.Has("truncated") || r.Has("outline") || r.Has("lead") {
		t.Fatalf("a body of %d chars is read whole: %d chars, truncated=%v", core.BoundedBodyChars, utf8.RuneCountInString(r.Str("body")), r.Has("truncated"))
	}
}

func TestReadDocPastTheBoundAnswersWithTheOutlineAndLead(t *testing.T) {
	root := tk.FakeVault(t)
	body := longDoc(t, root, core.BoundedBodyChars+1)
	v := tk.Vault(root)
	r := core.ReadDoc(v, "rb-long-ops", core.ReadArgs{})
	if r.Has("body") || !r.Bool("truncated") || !r.Bool("body_released") || r.Str("lead") != "Lead says what the doc is for.\n" ||
		r.Str("doc_id") != "rb-long-ops" || r.Str("title") != "Long Ops" {
		t.Fatalf("truncated=%v body=%d chars lead=%q", r.Bool("truncated"), utf8.RuneCountInString(r.Str("body")), r.Str("lead"))
	}
	var slugs []string
	for _, e := range tk.Hits(r, "outline") {
		slugs = append(slugs, e.Str("section"))
		if e.Has("body") {
			t.Fatal("the outline carries no text")
		}
	}
	if len(slugs) < 4 || !slices.Equal(slugs[:4], []string{"overview", "install", "operate", "archive-part-1"}) {
		t.Fatal(slugs)
	}
	steps := strings.Join(strs(r, "next_steps"), "\n")
	if !strings.Contains(steps, `read_doc(doc_id="rb-long-ops", sections=["install"])`) ||
		!strings.Contains(steps, `read_doc(doc_id="rb-long-ops", fields=["body"])`) {
		t.Fatal(steps)
	}
	whole := core.ReadDoc(v, "rb-long-ops", core.ReadArgs{Fields: []string{"body"}})
	if strings.TrimSuffix(whole.Str("body"), "\n") != strings.TrimSuffix(body, "\n") || whole.Has("truncated") {
		t.Fatal("fields=[\"body\"] returns the whole body")
	}
	full := core.ReadDoc(v, "rb-long-ops", core.ReadArgs{Full: true})
	if full.Str("body") != whole.Str("body") || full.Has("truncated") {
		t.Fatal("the CLI's full view returns the whole body")
	}
	part := core.ReadDoc(v, "rb-long-ops", core.ReadArgs{Sections: []string{"3"}})
	if got, _ := tk.Get(part, "sections.0.body").(string); got != "## Operate\n\nDaily work.\n" && got != "## Operate\n\nDaily work." {
		t.Fatal(jsonx.Compact(part))
	}
}

func TestReadDocOutlineSizesAreWhatEachSectionReadReturns(t *testing.T) {
	root := tk.FakeVault(t)
	longDoc(t, root, core.BoundedBodyChars+500)
	v := tk.Vault(root)
	r := core.ReadDoc(v, "rb-long-ops", core.ReadArgs{Fields: []string{"outline"}})
	for _, e := range tk.Hits(r, "outline") {
		s := core.ReadDoc(v, "rb-long-ops", core.ReadArgs{Sections: []string{e.Str("section")}})
		text, _ := tk.Get(s, "sections.0.body").(string)
		if tk.Get(e, "chars") != utf8.RuneCountInString(text) {
			t.Errorf("%s: outline says %v chars, the read returns %d", e.Str("section"), tk.Get(e, "chars"), utf8.RuneCountInString(text))
		}
	}
}

func TestReadDocRefusesAnAmbiguousSectionWithCandidates(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Twin Ops.md"), `
doc_id: rb-twin-ops
title: Twin Ops
doc_type: runbook
system: Example
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-01
`, "## A\n\n### Shape\n\none\n\n## B\n\n### Shape\n\ntwo\n")
	r := core.ReadDoc(tk.Vault(root), "rb-twin-ops", core.ReadArgs{Sections: []string{"Shape"}})
	if r.Has("sections") || r.Bool("body_released") || !slices.Equal(strs(r, "candidates"), []string{"a-shape", "b-shape"}) ||
		!strings.Contains(r.Str("section_error"), `"Shape" names 2 sections`) {
		t.Fatal(jsonx.Compact(r))
	}
	one := core.ReadDoc(tk.Vault(root), "rb-twin-ops", core.ReadArgs{Sections: []string{"b-shape"}})
	if got, _ := tk.Get(one, "sections.0.body").(string); !strings.Contains(got, "two") || strings.Contains(got, "one") {
		t.Fatal(jsonx.Compact(one))
	}
}

func TestReadDocNamesASectionByTextPathSlugPrefixOrPosition(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	v := tk.Vault(root)
	for _, ref := range []string{"Troubleshooting", "troubleshooting", "trouble", "3"} {
		r := core.ReadDoc(v, "rb-backup-ops", core.ReadArgs{Sections: []string{ref}})
		if tk.Get(r, "sections.0.section") != "troubleshooting" || r.Has("section_error") {
			t.Errorf("%q: %s", ref, jsonx.Compact(r))
		}
	}
	r := core.ReadDoc(v, "rb-backup-ops", core.ReadArgs{Sections: []string{"1"}})
	if body, _ := tk.Get(r, "sections.0.body").(string); !strings.HasPrefix(body, "Overview line") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.ReadDoc(v, "rb-backup-ops", core.ReadArgs{Sections: []string{"4"}}); !r.Has("available_sections") || r.Has("sections") {
		t.Fatal(jsonx.Compact(r))
	}
}

// strs is o[k] as strings, a []string or the []any JSON decodes to.
func strs(o *jsonx.Obj, k string) []string {
	v, _ := o.Get(k)
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, _ := e.(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}
