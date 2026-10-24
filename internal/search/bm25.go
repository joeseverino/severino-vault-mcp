package search

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
)

// The BM25 parameters: k1 at the top of the usual 1.2 to 2.0 range, the
// textbook b, and the field weights (the boost a term gets in that field)
// with the title over metadata and heading, each over the body. They sit on
// the plateau of a grid over k1, b and the weights scored by internal/evals;
// change them only with those numbers.
const (
	k1 = 2.0
	b  = 0.75
)

var weight = [nFields]float64{fTitle: 5, fMeta: 2, fHeading: 2, fBody: 1}

const (
	fTitle = iota
	fMeta
	fHeading
	fBody
	nFields
)

// Hit is a doc ranked by its best section: Section is the unit that scored
// (nil for a doc without sections or with a restricted body, whose one unit
// is its title and metadata alone).
type Hit struct {
	Doc     *vault.Doc
	Section *sections.Section
	Score   float64
}

type unit struct {
	doc *vault.Doc
	sec *sections.Section
	tf  [nFields]map[string]int
	len [nFields]int
}

func (u *unit) add(f int, text string) {
	if u.tf[f] == nil {
		u.tf[f] = map[string]int{}
	}
	for t := range terms(text) {
		u.tf[f][t]++
		u.len[f]++
	}
}

// units splits docs into section units. A restricted body feeds nothing, so
// such a doc is one unit of title and metadata.
func units(docs []*vault.Doc) []*unit {
	var out []*unit
	for _, d := range docs {
		meta := strings.Join(d.Tags, " ") + " " + d.System + " " + d.DocID
		base := func(sec *sections.Section) *unit {
			u := &unit{doc: d, sec: sec}
			u.add(fTitle, d.Title)
			u.add(fMeta, meta)
			return u
		}
		if !Searchable(d) || len(d.Sections) == 0 {
			u := base(nil)
			if Searchable(d) {
				u.add(fBody, d.Body)
			}
			out = append(out, u)
			continue
		}
		for i := range d.Sections {
			sec := &d.Sections[i]
			u := base(sec)
			u.add(fHeading, sec.HeadingPath)
			body := sec.Body
			if sec.Heading != "" {
				// The body opens with its heading line, which the heading field holds.
				_, body, _ = strings.Cut(body, "\n")
			}
			u.add(fBody, body)
			out = append(out, u)
		}
	}
	return out
}

// Rank scores every section unit of docs for query and returns each doc
// with a positive score once, at its best section, best first: score, then
// most recently reviewed, then title, descending.
func Rank(docs []*vault.Doc, query string, limit int) []Hit {
	return RankTerms(docs, QueryTokens(query), limit)
}

// RankTerms is Rank over terms already chosen, for a caller whose every
// word counts, such as a text search, where "not" is part of the query.
func RankTerms(docs []*vault.Doc, q Set, limit int) []Hit {
	if len(q) == 0 {
		return nil
	}
	us := units(docs)
	if len(us) == 0 {
		return nil
	}
	var avg [nFields]float64
	df := map[string]int{}
	for _, u := range us {
		for f := range nFields {
			avg[f] += float64(u.len[f])
		}
		for t := range q {
			for f := range nFields {
				if u.tf[f][t] > 0 {
					df[t]++
					break
				}
			}
		}
	}
	n := float64(len(us))
	for f := range avg {
		avg[f] /= n
	}
	best := map[*vault.Doc]int{}
	var hits []Hit
	for _, u := range us {
		s := 0.0
		for t := range q {
			tf := 0.0
			for f := range nFields {
				c := u.tf[f][t]
				if c == 0 {
					continue
				}
				norm := 1.0
				if avg[f] > 0 {
					norm = 1 - b + b*float64(u.len[f])/avg[f]
				}
				tf += weight[f] * float64(c) / norm
			}
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[t])+0.5)/(float64(df[t])+0.5))
			s += idf * tf / (k1 + tf)
		}
		if s <= 0 {
			continue
		}
		if i, ok := best[u.doc]; ok {
			if s > hits[i].Score {
				hits[i].Score, hits[i].Section = s, u.sec
			}
			continue
		}
		best[u.doc] = len(hits)
		hits = append(hits, Hit{u.doc, u.sec, s})
	}
	slices.SortStableFunc(hits, func(x, y Hit) int {
		if c := cmp.Compare(y.Score, x.Score); c != 0 {
			return c
		}
		if c := strings.Compare(y.Doc.LastReviewed, x.Doc.LastReviewed); c != 0 {
			return c
		}
		return strings.Compare(y.Doc.Title, x.Doc.Title)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
