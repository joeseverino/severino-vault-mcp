package core

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/search"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
)

// A filter value no doc carries is a guess at the vault's vocabulary (a
// site's domain for its project slug, a doc_type the profile retired), and
// the search it narrows comes back empty whatever the query. The way out is
// the real values closest to the guess.

// maxClosest is how many real values a step offers for one filter.
const maxClosest = 3

// filterSteps are the retries for each of a's filters that no doc in the
// vault matches on its own: the same find with the filter set to each of
// the closest values the vault holds.
func (v *Vault) filterSteps(q string, a FindArgs) []string {
	idx := v.Loader.Index(false)
	docs := idx.WithArchived()
	var steps []string
	for _, f := range []struct {
		name, given string
		matches     func(d *vault.Doc, value string) bool
		values      func() []string
	}{
		{"system", a.System, func(d *vault.Doc, s string) bool { return containsFold(d.System, s) }, func() []string {
			return distinct(docs, func(d *vault.Doc) []string { return []string{d.System} })
		}},
		{"project", a.Project, func(d *vault.Doc, s string) bool {
			return slices.ContainsFunc(d.RelatedProjects, func(p string) bool { return containsFold(p, s) })
		}, func() []string {
			return append(slices.Clone(idx.Projects), distinct(docs, func(d *vault.Doc) []string { return d.RelatedProjects })...)
		}},
		{"doc_type", a.DocType, func(d *vault.Doc, s string) bool { return d.DocType == s }, func() []string {
			var declared []string
			if v.Profile != nil {
				declared = v.Profile.DocTypes
			}
			return append(slices.Clone(declared), distinct(docs, func(d *vault.Doc) []string { return []string{d.DocType} })...)
		}},
	} {
		given := pystr.Strip(f.given)
		if given == "" || slices.ContainsFunc(docs, func(d *vault.Doc) bool { return f.matches(d, given) }) {
			continue
		}
		why := " (no doc's " + f.name + " matches " + strconv.Quote(given) + "; this is one of the closest the vault holds)"
		for _, c := range closest(given, f.values(), maxClosest) {
			steps = append(steps, Call("find", findArgsWith(q, a, f.name, c)...)+why)
		}
	}
	return steps
}

// findArgsWith is a's call arguments with one filter replaced.
func findArgsWith(q string, a FindArgs, name, value string) []any {
	args := []any{"query", q}
	if a.By != FindModes[0] {
		args = append(args, "by", a.By)
	}
	for _, f := range []struct{ name, value string }{{"system", a.System}, {"project", a.Project}, {"doc_type", a.DocType}} {
		if f.name == name {
			f.value = value
		}
		if f.value != "" {
			args = append(args, f.name, f.value)
		}
	}
	if a.IncludeArchived {
		args = append(args, "include_archived", true)
	}
	return args
}

// distinct is every non-empty value of docs, once each.
func distinct(docs []*vault.Doc, values func(*vault.Doc) []string) []string {
	var out []string
	for _, d := range docs {
		for _, s := range values(d) {
			if s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// closest ranks candidates by likeness to given: the words they share, then
// whether one holds the other once punctuation is gone, then edit distance,
// and returns the first n, each once.
func closest(given string, candidates []string, n int) []string {
	g := search.Tokenize(given)
	squash := func(s string) string {
		return strings.Join(strings.Fields(strings.Map(alnumOrSpace, strings.ToLower(s))), "")
	}
	gs := squash(given)
	type scored struct {
		value         string
		shared, holds int
		dist          int
	}
	var all []scored
	for _, c := range candidates {
		if slices.ContainsFunc(all, func(s scored) bool { return strings.EqualFold(s.value, c) }) {
			continue
		}
		cs := squash(c)
		holds := 0
		if cs != "" && gs != "" && (strings.Contains(cs, gs) || strings.Contains(gs, cs)) {
			holds = 1
		}
		all = append(all, scored{c, search.Intersect(g, search.Tokenize(c)), holds, pystr.EditDistance(gs, cs)})
	}
	slices.SortStableFunc(all, func(a, b scored) int {
		if a.shared != b.shared {
			return b.shared - a.shared
		}
		if a.holds != b.holds {
			return b.holds - a.holds
		}
		if c := cmp.Compare(a.dist, b.dist); c != 0 {
			return c
		}
		return strings.Compare(a.value, b.value)
	})
	out := make([]string, 0, n)
	for _, s := range all[:min(n, len(all))] {
		out = append(out, s.value)
	}
	return out
}

func alnumOrSpace(r rune) rune {
	if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
		return r
	}
	return ' '
}

// filterFields are names agents ask find's fields for that hits don't
// carry, because find filters by them instead.
var filterFields = map[string]string{"related_projects": "project"}

// fieldSteps recover a refused find field mask: the mask with each refused
// name replaced by the field it meant, the filter a name stands for, and
// the default.
func fieldSteps(q string, a FindArgs, err error) []string {
	var steps []string
	if fe, ok := errors.AsType[*FieldsError](err); ok && fe.Fixed != nil {
		steps = append(steps, Call("find", append(findArgsWith(q, a, "", ""), "fields", fe.Fixed)...)+" with the fields the error names")
	}
	for _, f := range a.Fields {
		if filter, ok := filterFields[f]; ok {
			steps = append(steps, Call("find", findArgsWith(q, a, filter, "...")...)+
				" to keep the docs of one "+filter+"; hits do not carry "+f)
		}
	}
	return append(steps, withoutFields("find", findArgsWith(q, a, "", "")...))
}
