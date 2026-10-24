package core

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
)

// DocRef is the doc reference every tool and subcommand that names an
// existing doc takes, all resolved by resolveDoc.
const DocRef = "its doc_id (from `find`), its title, or its vault path"

// DocRefHelp is a doc reference parameter's description; what is the doc
// it names.
func DocRefHelp(what string) string { return what + ": " + DocRef + "." }

// exactNames are the ways a name can match a doc, strongest first: find
// ranks the doc a query names by them, and resolveDoc resolves a reference
// by them after an exact doc_id or path.
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
		slices.Sort(out)
		if d.Frontmatter != nil {
			v, _ := d.Frontmatter.Get("aliases")
			out = append(out, vault.CoerceList(v)...)
		}
		return out
	}},
	{"title", func(_ *vault.Index, d *vault.Doc) []string { return []string{d.Title} }},
	{"file_name", func(_ *vault.Index, d *vault.Doc) []string { return []string{d.Stem()} }},
	{"path", func(_ *vault.Index, d *vault.Doc) []string {
		return []string{d.RelativePath, strings.TrimSuffix(d.RelativePath, ".md")}
	}},
}

// normalName is a name as exactNames compares it: case and spacing folded,
// and the quotes, backticks and brackets a caller may wrap it in dropped.
func normalName(s string) string {
	return vault.NormalizeAlias(strings.Trim(strings.TrimSpace(s), "`\"'[]"))
}

// matchedName is the first of names that is want, or "".
func matchedName(names []string, want string) string {
	for _, n := range names {
		if normalName(n) == want {
			return n
		}
	}
	return ""
}

// unresolved is why a doc reference names no one doc: paths are the files a
// duplicate doc_id is in, candidates the docs an ambiguous name fits.
type unresolved struct {
	msg        string
	paths      []string
	candidates []*vault.Doc
}

// resolveDoc is the one doc ref names, the only way a doc reference a caller
// gives becomes a doc: an exact doc_id, then an exact vault path (with or
// without .md), then each of exactNames in turn, case and spacing folded. A
// name several docs share is refused with them as candidates, a live doc
// winning over archived ones. alias says how a reference other than an
// exact doc_id matched.
func resolveDoc(idx *vault.Index, ref string) (d *vault.Doc, alias *jsonx.Obj, why *unresolved) {
	ref = pystr.Strip(ref)
	if ref == "" {
		return nil, nil, &unresolved{msg: "name the doc by its doc_id, title or vault path"}
	}
	if d, ok := idx.ByDocID[ref]; ok {
		return d, nil, nil
	}
	if dups, ok := idx.DuplicateDocIDs[ref]; ok {
		return nil, nil, &unresolved{msg: fmt.Sprintf("duplicate doc_id %q at %s; resolve the duplicate first", ref, pystr.Quoted(dups)), paths: slices.Clone(dups)}
	}
	matched := func(d *vault.Doc, name, kind string) *jsonx.Obj {
		return jsonx.New("input", ref, "matched_alias", name, "target_doc_id", d.DocID, "source", kind)
	}
	docs := idx.WithArchived()
	clean := path.Clean(strings.TrimLeft(ref, "/"))
	for _, d := range docs {
		if d.RelativePath == clean || d.RelativePath == clean+".md" {
			return d, matched(d, d.RelativePath, "path"), nil
		}
	}
	want := normalName(ref)
	for _, n := range exactNames {
		var found []*vault.Doc
		for _, d := range docs {
			if matchedName(n.names(idx, d), want) != "" {
				found = append(found, d)
			}
		}
		if live := slices.DeleteFunc(slices.Clone(found), func(d *vault.Doc) bool { return !slices.Contains(idx.Docs, d) }); len(found) > 1 && len(live) == 1 {
			found = live
		}
		switch {
		case len(found) == 1:
			return found[0], matched(found[0], matchedName(n.names(idx, found[0]), want), n.kind), nil
		case len(found) > 1:
			paths := make([]string, len(found))
			for i, c := range found {
				paths[i] = c.RelativePath
			}
			return nil, nil, &unresolved{msg: fmt.Sprintf("%q is the %s of %d docs, at %s; name one by its doc_id", ref, n.kind, len(found), pystr.Quoted(paths)), candidates: found}
		}
	}
	return nil, nil, &unresolved{msg: fmt.Sprintf("no doc has the doc_id, title or vault path %q; call `find` to get one", ref)}
}

// steps are the ways past an unresolved reference: retry with each
// candidate's doc_id, track a duplicate doc_id, or find the doc.
func (u *unresolved) steps(ref string, retry func(docID string) string) []string {
	switch {
	case len(u.candidates) > 0:
		out := make([]string, len(u.candidates))
		for i, c := range u.candidates {
			out[i] = retry(c.DocID) + " for " + c.RelativePath
		}
		return out
	case len(u.paths) > 0:
		return []string{Call("queue", "action", "add", "project", vault.BacklogProject, "kind", "follow-up", "text", "Resolve duplicate doc_id "+ref) +
			" and tell the user; neither doc can be used until one file's doc_id changes"}
	}
	return []string{Call("find", "query", strings.TrimSuffix(path.Base(pystr.Strip(ref)), ".md")) + " to get the doc's doc_id, then retry with it"}
}

// candidateHits are an ambiguous reference's candidates as find hits name
// them.
func (u *unresolved) candidateHits() []*jsonx.Obj {
	out := make([]*jsonx.Obj, len(u.candidates))
	for i, c := range u.candidates {
		out[i] = jsonx.New("doc_id", c.DocID, "title", c.Title, "relative_path", c.RelativePath)
	}
	return out
}
