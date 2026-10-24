package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func masked(v *core.Vault, q, by string, fields ...string) *jsonx.Obj {
	return core.Find(v, q, core.FindArgs{By: by, Limit: 10, ContextLines: 1, Fields: fields})
}

func sensitiveDoc(t *testing.T, root string) {
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Rotate Example Token.md"), `
doc_id: rb-rotate-example-token
title: Rotate Example Token
doc_type: runbook
system: Example API
environment: other
status: active
sensitivity: sensitive
last_reviewed: 2026-05-01
tags: [token, rotation]
`, "## Rotate\n\nRotate the example token from the console.\n\n## Verify\n\nCall the example API with the new token.\n")
}

func TestFindDefaultHitIsTheLeanSet(t *testing.T) {
	r := find(tk.Vault(tk.FakeVault(t)), "nginx proxy")
	hit := tk.Hits(r, "hits")[0]
	if want := []string{"score", "doc_id", "title", "section", "heading_path", "section_summary"}; !slices.Equal(hit.Keys(), want) {
		t.Fatalf("hit keys %v, want %v", hit.Keys(), want)
	}
	for _, k := range []string{"vault", "query", "indexed_doc_count", "quick_index_matches"} {
		if r.Has(k) {
			t.Fatalf("lean envelope kept %s: %s", k, jsonx.Compact(r))
		}
	}
	if !r.Has("match_count") || !r.Has("mode") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestFindFieldsSelectHitFields(t *testing.T) {
	r := masked(tk.Vault(tk.FakeVault(t)), "nginx proxy", "relevance", "tags", "obsidian_path")
	hit := tk.Hits(r, "hits")[0]
	if !slices.Equal(hit.Keys(), []string{"obsidian_path", "tags"}) {
		t.Fatalf("%v: fields keep the record's own order and nothing else", hit.Keys())
	}
}

func TestFindFieldsThroughAListKeepEveryElement(t *testing.T) {
	requireRG(t)
	v := tk.Vault(tk.FakeVault(t))
	full := tk.Hits(findFull(v, "npm https", "text"), "hits")[0]
	want, _ := full.Get("snippets")
	r := masked(v, "npm https", "text", "snippets.text")
	got, _ := tk.Hits(r, "hits")[0].Get("snippets")
	if len(got.([]*jsonx.Obj)) != len(want.([]*jsonx.Obj)) || len(want.([]*jsonx.Obj)) == 0 {
		t.Fatalf("list length changed: %s", jsonx.Compact(r))
	}
	for i, s := range got.([]*jsonx.Obj) {
		if !slices.Equal(s.Keys(), []string{"text"}) || s.Str("text") != want.([]*jsonx.Obj)[i].Str("text") {
			t.Fatal(jsonx.Compact(s))
		}
	}
}

func TestFindFieldsMergePrefixesAndAWholeValueWins(t *testing.T) {
	requireRG(t)
	v := tk.Vault(tk.FakeVault(t))
	merged := masked(v, "npm https", "text", "snippets.text", "snippets.kind")
	if s := tk.Hits(tk.Hits(merged, "hits")[0], "snippets")[0]; !slices.Equal(s.Keys(), []string{"kind", "text"}) {
		t.Fatal(jsonx.Compact(s))
	}
	for _, order := range [][]string{{"snippets", "snippets.text"}, {"snippets.text", "snippets"}} {
		r := masked(v, "npm https", "text", order...)
		if s := tk.Hits(tk.Hits(r, "hits")[0], "snippets")[0]; !slices.Equal(s.Keys(), []string{"line_number", "kind", "text"}) {
			t.Fatalf("%v: %s", order, jsonx.Compact(s))
		}
	}
}

func TestFindFieldsRefusalsNameValidFieldsAndNeverEchoTheInput(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	cases := map[string]struct {
		fields []string
		want   string
	}{
		"unknown top":   {[]string{"title", "zzcanary"}, "fields[1]: unknown field under the top level; valid fields there are \"score\", \"doc_id\""},
		"unknown inner": {[]string{"snippets.zzcanary"}, "fields[0]: unknown field under snippets; valid fields there are \"line_number\", \"kind\", \"text\""},
		"into a leaf":   {[]string{"tags.zzcanary"}, "fields[0]: tags is kept whole"},
		"empty":         {[]string{}, "fields: give at least one field"},
		"double dot":    {[]string{"snippets..zzcanary"}, "fields[0]: a path is field names joined by single dots"},
		"leading dot":   {[]string{".zzcanary"}, "fields[0]: a path"},
		"trailing dot":  {[]string{"zzcanary."}, "fields[0]: a path"},
	}
	for name, c := range cases {
		r := masked(v, "nginx", "relevance", c.fields...) // an empty literal stays a non-nil, empty list
		msg := r.Str("error")
		if r.Bool("ok") || r.Has("hits") || !strings.Contains(msg, c.want) || strings.Contains(msg, "zzcanary") {
			t.Errorf("%s: %s", name, jsonx.Compact(r))
		}
	}
}

func TestFindAdvisoryTravelsWithASensitiveHitWhateverTheMask(t *testing.T) {
	requireRG(t)
	root := tk.FakeVault(t)
	sensitiveDoc(t, root)
	r := masked(tk.Vault(root), "example token console", "text", "doc_id")
	hit := tk.Hits(r, "hits")[0]
	if hit.Str("doc_id") != "rb-rotate-example-token" || hit.Str("advisory") == "" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocDefaultTrimsFrontmatterButKeepsTheGate(t *testing.T) {
	root := tk.FakeVault(t)
	sensitiveDoc(t, root)
	r := core.ReadDoc(tk.Vault(root), "rb-rotate-example-token", core.ReadArgs{})
	if want := []string{"doc_id", "found", "title", "status", "last_reviewed", "body", "body_released", "advisory"}; !slices.Equal(r.Keys(), want) {
		t.Fatalf("keys %v, want %v", r.Keys(), want)
	}
	only := core.ReadDoc(tk.Vault(root), "rb-rotate-example-token", core.ReadArgs{Fields: []string{"tags"}})
	if only.Has("body") || only.Str("advisory") == "" || !only.Bool("found") || only.Has("title") {
		t.Fatal(jsonx.Compact(only))
	}
}

func TestReadDocOutlineListsSectionsWithoutText(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	r := core.ReadDoc(tk.Vault(root), "rb-backup-ops", core.ReadArgs{Fields: []string{"outline"}})
	var slugs []string
	for _, s := range tk.Hits(r, "outline") {
		slugs = append(slugs, s.Str("section"))
		if s.Has("body") {
			t.Fatal(jsonx.Compact(s))
		}
	}
	if r.Has("body") || !slices.Contains(slugs, "overview") || !slices.Contains(slugs, "troubleshooting") {
		t.Fatal(jsonx.Compact(r))
	}
	if core.ReadDoc(tk.Vault(root), "rb-backup-ops", core.ReadArgs{}).Has("outline") {
		t.Fatal("the outline is opt-in")
	}
}

func TestReadDocWithholdsTheOutlineOfARestrictedDoc(t *testing.T) {
	r := core.ReadDoc(tk.Vault(tk.FakeVault(t)), "infra-local-pki", core.ReadArgs{Fields: []string{"outline", "body"}})
	if r.Has("outline") || r.Has("body") || r.Bool("body_released") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadDocSectionsReturnsEachInOrder(t *testing.T) {
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	v := tk.Vault(root)
	r := core.ReadDoc(v, "rb-backup-ops", core.ReadArgs{Sections: []string{"troubleshooting", "Routine operations"}})
	secs := tk.Hits(r, "sections")
	if r.Has("body") || len(secs) != 2 || secs[0].Str("section") != "troubleshooting" || secs[1].Str("section") != "routine-operations" ||
		!strings.Contains(secs[1].Str("body"), "daily job") {
		t.Fatal(jsonx.Compact(r))
	}
	if e := core.ReadDoc(v, "rb-backup-ops", core.ReadArgs{Sections: []string{}}); !strings.Contains(e.Str("error"), "sections: give at least one") {
		t.Fatal(jsonx.Compact(e))
	}
}
