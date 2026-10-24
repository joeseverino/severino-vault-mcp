package core

import (
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/query"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// A query that is a doc's name is a request for that doc, so find ranks it
// first in either mode: agents pass a doc_id or a title they were given, and
// a body search or a token ranking would bury it under docs that mention it.

// exactNames are the ways a query can name a doc, strongest first. A name
// two docs share names neither, so a weaker kind is tried instead.
var exactNames = []struct {
	kind  string
	names func(idx *vault.Index, d *vault.Doc) []string
}{
	{"doc_id", func(_ *vault.Index, d *vault.Doc) []string { return []string{d.DocID} }},
	{"alias", func(idx *vault.Index, d *vault.Doc) []string {
		var out []string
		for alias, id := range idx.Aliases {
			if id == d.DocID {
				out = append(out, alias)
			}
		}
		if d.Frontmatter != nil {
			v, _ := d.Frontmatter.Get("aliases")
			out = append(out, vault.CoerceList(v)...)
		}
		return out
	}},
	{"title", func(_ *vault.Index, d *vault.Doc) []string { return []string{d.Title} }},
	{"file_name", func(_ *vault.Index, d *vault.Doc) []string { return []string{d.Stem()} }},
}

// exactDoc is the one doc among those keep accepts that q names, and how it
// names it; nil when q names none, or names several alike.
func exactDoc(idx *vault.Index, q string, keep query.Keep) (*vault.Doc, string) {
	want := vault.NormalizeAlias(strings.Trim(strings.TrimSpace(q), "`\"'[]"))
	if want == "" {
		return nil, ""
	}
	docs := idx.WithArchived()
	for _, n := range exactNames {
		var found []*vault.Doc
		for _, d := range docs {
			if keep != nil && !keep(d) {
				continue
			}
			if slices.ContainsFunc(n.names(idx, d), func(s string) bool { return vault.NormalizeAlias(s) == want }) {
				found = append(found, d)
			}
		}
		if len(found) == 1 {
			return found[0], n.kind
		}
		if len(found) > 1 {
			return nil, ""
		}
	}
	return nil, ""
}

// rankExactFirst puts the doc q names at the top of out's hits, from lower
// down or, when the search missed it, as its relevance hit, keeping the hit
// count within limit. It reports the match under exact_match and leads
// next_steps with reading the doc.
func rankExactFirst(v *Vault, out *jsonx.Obj, q string, a FindArgs, keep query.Keep) {
	d, kind := exactDoc(v.Loader.Index(false), q, keep)
	if d == nil {
		return
	}
	raw, _ := out.Get("hits")
	hits, _ := raw.([]*jsonx.Obj)
	i := slices.IndexFunc(hits, func(h *jsonx.Obj) bool { return h.Str("doc_id") == d.DocID })
	var top *jsonx.Obj
	if i >= 0 {
		top = hits[i]
		hits = slices.Delete(slices.Clone(hits), i, i+1)
	} else {
		only := func(c *vault.Doc) bool { return c == d }
		r, _ := query.FindSections(v.Loader, q, 1, only).Get("hits")
		if found := r.([]*jsonx.Obj); len(found) > 0 {
			top = found[0]
		} else {
			top = jsonx.New("score", 0).Merge(query.Hit(d))
		}
	}
	hits = append([]*jsonx.Obj{top}, hits...)
	if limit := max(a.Limit, 1); len(hits) > limit {
		hits = hits[:limit]
	}
	out.Set("hits", hits)
	out.Set("match_count", len(hits))
	out.Set("exact_match", jsonx.New("doc_id", d.DocID, "matched", kind))
	if rec, ok := out.Get("recommended"); ok && rec.(*jsonx.Obj).Str("target_doc_id") != d.DocID {
		out.Delete("recommended")
	}
	steps, _ := out.Get("next_steps")
	prior, _ := steps.([]string)
	out.Set("next_steps", append([]string{Call("read_doc", "doc_id", d.DocID) + " to read the doc the query names"}, prior...))
}
