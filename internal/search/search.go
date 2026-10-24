// Package search is the keyword ranker: a weighted bag-of-words score over
// a doc's metadata with a small, capped body signal, plus section scoring to
// pick which span of a matched doc answers the query.
package search

import (
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/gate"
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
	// titleCover is per query word, once a title holds every word of the query:
	// a doc named for the question outranks docs that only mention its words.
	titleCover = 3
	// procedureVerb favours a procedure named for the query's action: its title
	// opens with a query word, as "Deploy X" does for "deploy x".
	procedureVerb = 3
)

// procedureTypes are the doc types that say how to do something.
var procedureTypes = set("runbook", "deployment_guide", "troubleshooting_guide")

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

// IsStopword reports whether a lowercase query word is filler.
func IsStopword(w string) bool {
	_, ok := queryStopwords[w]
	return ok
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

// Searchable reports whether a doc's body may feed ranking, section menus or
// text matches: restricted bodies never do.
func Searchable(doc *vault.Doc) bool { return doc.Sensitivity != gate.Restricted }

// Score ranks one doc for a query on its metadata plus a capped body signal.
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
	if inTitle := Intersect(q, title); inTitle > 0 {
		if inTitle == len(q) {
			s += titleCover * len(q)
		}
		if _, ok := procedureTypes[doc.DocType]; ok {
			if _, ok := q[strings.ToLower(tokenRE.FindString(doc.Title))]; ok {
				s += procedureVerb
			}
		}
	}

	meta := union(tag, sys, title, id, env)
	body := Set{}
	if Searchable(doc) {
		body = Tokenize(doc.Body)
	}
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

// BestSection is the highest-scoring section, falling back to the first; nil
// for a doc without sections or with a restricted body.
func BestSection(doc *vault.Doc, query string) (*sections.Section, int) {
	return bestSection(doc, QueryTokens(query))
}

func bestSection(doc *vault.Doc, q Set) (*sections.Section, int) {
	if len(doc.Sections) == 0 || !Searchable(doc) {
		return nil, 0
	}
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
// recently reviewed, then title, all descending. A doc's score adds its best
// section's, so a doc whose section answers the query outranks a hub whose
// title and system merely name the same subject.
func Rank(docs []*vault.Doc, query string, limit int) []Hit {
	q := QueryTokens(query)
	var hits []Hit
	for _, d := range docs {
		if sc := Score(d, q); sc > 0 {
			_, secScore := bestSection(d, q)
			hits = append(hits, Hit{d, sc + secScore})
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
