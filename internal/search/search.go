// Package search is the keyword ranker: a weighted bag-of-words score over
// a doc's metadata with a small, capped body signal, plus section scoring to
// pick which span of a matched doc answers the query.
package search

import (
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

var tokenRE = regexp.MustCompile(`[A-Za-z0-9]+`)

// Query-only stopwords, so filler words never manufacture matches.
var queryStopwords = set("a", "an", "the", "of", "to", "in", "on", "for", "and", "or", "with",
	"my", "is", "it", "do", "i", "into", "as", "at", "by", "from", "this", "that")

const (
	bodyMatchWeight = 1
	bodyMatchCap    = 3
	headingWeight   = 3
	sectionBody     = 1
)

// Set is a token set.
type Set map[string]struct{}

func set(items ...string) Set {
	s := Set{}
	for _, i := range items {
		s[i] = struct{}{}
	}
	return s
}

// Tokenize lowercases every [A-Za-z0-9]+ run.
func Tokenize(text string) Set {
	s := Set{}
	for _, m := range tokenRE.FindAllString(text, -1) {
		s[strings.ToLower(m)] = struct{}{}
	}
	return s
}

// QueryTokens tokenizes a query and drops stopwords.
func QueryTokens(query string) Set {
	s := Tokenize(query)
	for w := range queryStopwords {
		delete(s, w)
	}
	return s
}

// Intersect counts the tokens a and b share.
func Intersect(a, b Set) int {
	if len(b) < len(a) {
		a, b = b, a
	}
	n := 0
	for k := range a {
		if _, ok := b[k]; ok {
			n++
		}
	}
	return n
}

func union(sets ...Set) Set {
	out := Set{}
	for _, s := range sets {
		for k := range s {
			out[k] = struct{}{}
		}
	}
	return out
}

// Score ranks one doc for a query.
func Score(doc *vault.Doc, q Set) int {
	if len(q) == 0 {
		return 0
	}
	tag := Tokenize(strings.Join(doc.Tags, " "))
	sys := Tokenize(doc.System)
	title := Tokenize(doc.Title)
	id := Tokenize(strings.ReplaceAll(doc.DocID, "-", " "))
	env := Tokenize(doc.Environment)

	s := 5*Intersect(q, tag) + 3*Intersect(q, sys) + 3*Intersect(q, title) + Intersect(q, id) + Intersect(q, env)

	meta := union(tag, sys, title, id, env)
	body := Tokenize(doc.Body)
	bodyOnly := 0
	for k := range q {
		_, inBody := body[k]
		_, inMeta := meta[k]
		if inBody && !inMeta {
			bodyOnly++
		}
	}
	s += bodyMatchWeight * min(bodyOnly, bodyMatchCap)
	if s > 0 && doc.Status == "active" {
		s++
	}
	return s
}

// ScoreSection ranks one section: heading terms outweigh body-only terms.
func ScoreSection(sec sections.Section, q Set) int {
	if len(q) == 0 {
		return 0
	}
	head := Tokenize(sec.HeadingPath)
	body := Tokenize(sec.Body)
	s := headingWeight * Intersect(q, head)
	for k := range q {
		_, inBody := body[k]
		_, inHead := head[k]
		if inBody && !inHead {
			s += sectionBody
		}
	}
	return s
}

// BestSection is the highest-scoring section, falling back to the first.
func BestSection(doc *vault.Doc, query string) (*sections.Section, int) {
	if len(doc.Sections) == 0 {
		return nil, 0
	}
	q := QueryTokens(query)
	best, bestScore := &doc.Sections[0], 0
	for i := range doc.Sections {
		if sc := ScoreSection(doc.Sections[i], q); sc > bestScore {
			best, bestScore = &doc.Sections[i], sc
		}
	}
	return best, bestScore
}

// Hit is a ranked doc.
type Hit struct {
	Doc   *vault.Doc
	Score int
}

// Rank returns docs with a positive score, best first: score, then most
// recently reviewed, then title, all descending.
func Rank(docs []*vault.Doc, query string, limit int) []Hit {
	q := QueryTokens(query)
	var hits []Hit
	for _, d := range docs {
		if sc := Score(d, q); sc > 0 {
			hits = append(hits, Hit{d, sc})
		}
	}
	slices.SortStableFunc(hits, func(a, b Hit) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if c := strings.Compare(b.Doc.LastReviewed, a.Doc.LastReviewed); c != 0 {
			return c
		}
		return strings.Compare(b.Doc.Title, a.Doc.Title)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
