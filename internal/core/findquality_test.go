package core_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

// mentionsDoc adds a doc whose body says name over and over, so a body or
// token ranking would put it above the doc name names.
func mentionsDoc(t *testing.T, root, file, docID, name string) {
	t.Helper()
	tk.Doc(t, filepath.Join(root, "02 Infrastructure", file), `
doc_id: `+docID+`
title: Notes About Many Things
doc_type: architecture_note
system: Example
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-09-01
`, "## Mentions\n\n"+strings.Repeat(name+" and "+name+" again.\n", 8))
}

func steps(r *jsonx.Obj) []string {
	v, _ := r.Get("next_steps")
	s, _ := v.([]string)
	return s
}

// A query that is a doc's doc_id, alias or title gets that doc first, in
// either mode, with the read to make next.
func TestAQueryNamingADocRanksItFirst(t *testing.T) {
	root := tk.FakeVault(t)
	mentionsDoc(t, root, "Mentions.md", "infra-mentions", "rb-add-nginx-proxy-host Add Nginx Proxy Host https proxy")
	v := tk.Vault(root)
	for _, tc := range []struct{ q, by, kind string }{
		{"rb-add-nginx-proxy-host", "text", "doc_id"},
		{"rb-add-nginx-proxy-host", "relevance", "doc_id"},
		{"Add Nginx Proxy Host", "text", "title"},
		{"add nginx proxy host", "relevance", "title"},
		{"https proxy", "text", "alias"},
	} {
		r := findBy(v, tc.q, tc.by)
		hits := tk.IDs(tk.Hits(r, "hits"))
		if len(hits) == 0 || hits[0] != "rb-add-nginx-proxy-host" {
			t.Errorf("%s %q: hits %v", tc.by, tc.q, hits)
		}
		if got := jsonx.Compact(tk.Get(r, "exact_match")); got != `{"doc_id":"rb-add-nginx-proxy-host","matched":"`+tc.kind+`"}` {
			t.Errorf("%s %q: exact_match %s", tc.by, tc.q, got)
		}
		if s := steps(r); len(s) == 0 || s[0] != `read_doc(doc_id="rb-add-nginx-proxy-host") to read the doc the query names` {
			t.Errorf("%s %q: next_steps %v", tc.by, tc.q, s)
		}
		if n, _ := r.Get("match_count"); n != len(hits) {
			t.Errorf("%s %q: match_count %v for %d hits", tc.by, tc.q, n, len(hits))
		}
	}
}

// A name two docs share names neither, and a filter that leaves the named
// doc out leaves it out.
func TestAnExactMatchIsUniqueAndHonorsTheFilters(t *testing.T) {
	root := tk.FakeVault(t)
	mentionsDoc(t, root, "One.md", "infra-one", "x")
	mentionsDoc(t, root, "Two.md", "infra-two", "x")
	v := tk.Vault(root)
	if r := find(v, "Notes About Many Things"); r.Has("exact_match") {
		t.Fatalf("a shared title matched one doc: %s", jsonx.Compact(r))
	}
	r := core.Find(v, "rb-add-nginx-proxy-host", core.FindArgs{By: "text", Limit: 10, System: "Example"})
	if r.Has("exact_match") || slices.Contains(tk.IDs(tk.Hits(r, "hits")), "rb-add-nginx-proxy-host") {
		t.Fatalf("the filter was ignored: %s", jsonx.Compact(r))
	}
}

// A find result never spends more than FindBudget characters: snippets go
// first, then hits from the bottom, and the result says what it took out
// and how to get it.
func TestFindStaysWithinItsBudget(t *testing.T) {
	root := tk.FakeVault(t)
	long := strings.Repeat("zebra crossing ", 60)
	for i := range 60 {
		tk.Doc(t, filepath.Join(root, "02 Infrastructure", fmt.Sprintf("Zebra %02d.md", i)), fmt.Sprintf(`
doc_id: infra-zebra-%02d
title: Zebra %02d
doc_type: architecture_note
system: Example
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-09-01
`, i, i), "## Lines\n\n"+strings.Repeat(long+"\n", 12))
	}
	v := tk.Vault(root)
	for _, tc := range []struct {
		q, by   string
		limit   int
		dropped bool
	}{
		{"zebra", "text", 5, false},
		{"zebra crossing", "text", 50, true},
		{"zebra", "relevance", 25, true},
	} {
		r := core.Find(v, tc.q, core.FindArgs{By: tc.by, Limit: tc.limit, ContextLines: 2, Full: true})
		if n := len(core.Text(r)); n > core.FindBudget {
			t.Errorf("%s %q limit %d: %d characters", tc.by, tc.q, tc.limit, n)
		}
		if !r.Has("truncated") {
			t.Errorf("%s %q limit %d: nothing says the result was cut: %.300s", tc.by, tc.q, tc.limit, core.Text(r))
			continue
		}
		if d, _ := tk.Get(r, "truncated.hits_dropped").(int); (d > 0) != tc.dropped || len(tk.Hits(r, "hits")) == 0 {
			t.Errorf("%s %q limit %d: truncated %s", tc.by, tc.q, tc.limit, jsonx.Compact(tk.Get(r, "truncated")))
		}
		if s := steps(r); len(s) == 0 || !strings.HasPrefix(s[0], "read_doc(") {
			t.Errorf("%s %q: next_steps %v", tc.by, tc.q, s)
		}
	}
	if r := find(v, "zebra"); r.Has("truncated") {
		t.Errorf("a lean result was cut: %s", core.Text(r))
	}
}

// A filter value no doc carries comes back with the same find set to the
// closest values the vault does hold.
func TestAFilterNoDocMatchesOffersTheClosestRealValues(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Deploy the Site.md"), `
doc_id: rb-deploy-site
title: Deploy the Site
doc_type: runbook
system: Example Site
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-09-01
related_projects: [example-site]
`, "## Steps\n\nMerge to main.\n")
	v := tk.Vault(root)
	for _, tc := range []struct {
		args core.FindArgs
		step string
	}{
		{core.FindArgs{Project: "example.com"}, `find(query="deploy", project="example-site") (no doc's project matches "example.com"`},
		{core.FindArgs{System: "example-site", DocType: "runbook"}, `find(query="deploy", system="Example Site", doc_type="runbook")`},
		{core.FindArgs{DocType: "runbooks"}, `find(query="deploy", doc_type="runbook")`},
	} {
		a := tc.args
		a.By, a.Limit = "relevance", 10
		r := core.Find(v, "deploy", a)
		if s := steps(r); len(s) == 0 || !strings.HasPrefix(s[0], tc.step) {
			t.Errorf("%+v: want a first step starting %s, got %v", tc.args, tc.step, s)
		}
	}
	// A filter some doc matches is not the reason a search is empty.
	r := core.Find(v, "zzqx", core.FindArgs{By: "relevance", Limit: 10, Project: "example-site"})
	if s := steps(r); len(s) > 0 && strings.Contains(s[0], "closest") {
		t.Errorf("a real filter value was second-guessed: %v", s)
	}
}

// A refused field name is answered with the field it most plausibly meant,
// or the few it may have meant, and never by echoing it.
func TestARefusedFieldNamesWhatItMeant(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	for fields, want := range map[string]string{
		"path":       `fields[0] is likely one of "obsidian_path", "heading_path"`,
		"kind":       `fields[0] is likely "snippets.kind"`,
		"doc_ids":    `fields[0] is likely "doc_id"`,
		"snippets.x": `valid fields there are "line_number", "kind", "text"`,
	} {
		r := core.Find(v, "nginx", core.FindArgs{By: "relevance", Limit: 10, Fields: []string{fields}})
		if r.Bool("ok") || !strings.Contains(r.Str("error"), want) {
			t.Errorf("%s: %s", fields, jsonx.Compact(r))
		}
	}
}
