// Package search is find's ranker: Okapi BM25 in its BM25F form (Robertson
// and Zaragoza, "The Probabilistic Relevance Framework: BM25 and Beyond",
// 2009) over section units. Every section of a doc is one unit whose fields
// are the doc's title, its metadata (tags, system, doc_id), the section's
// heading path and the section's body; a doc ranks at its best unit, which is
// also the section a hit points at. A field's term frequency is normalized by
// that field's length against its mean across units, weighted, and summed
// before the k1 saturation; idf is the non-negative Lucene form over units.
package search

import (
	"iter"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// textStopwords are the filler words a text search drops. The list stays
// short because text mode is a literal body search: a word such as "not" or
// "down" is part of what the reader is looking for.
var textStopwords = set("a", "an", "the", "of", "to", "in", "on", "for", "and", "or", "with",
	"my", "is", "it", "do", "i", "into", "as", "at", "by", "from", "this", "that")

// rankStopwords are the query words ranking ignores, so filler never
// manufactures matches: the Snowball English stop list, its contractions as
// the tokenizer splits them.
var rankStopwords = set(strings.Fields(`i me my myself we our ours ourselves you your yours yourself yourselves
he him his himself she her hers herself it its itself they them their theirs themselves what which who whom
this that these those am is are was were be been being have has had having do does did doing would should
could ought a an the and but if or because as until while of at by for with about against between into
through during before after above below to from up down in out on off over under again further then once
here there when where why how all any both each few more most other some such no nor not only own same so
than too very s t d ll m re ve`)...)

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
	for t := range terms(text) {
		s[t] = struct{}{}
	}
	return s
}

// terms yields every [A-Za-z0-9]+ run of text, lowercased, repeats kept. It
// scans bytes rather than matching a regexp because ranking tokenizes every
// section of the vault on each find.
func terms(text string) iter.Seq[string] {
	return func(yield func(string) bool) {
		start := -1
		for i := 0; i <= len(text); i++ {
			if i < len(text) && isAlnum(text[i]) {
				if start < 0 {
					start = i
				}
				continue
			}
			if start >= 0 {
				if !yield(strings.ToLower(text[start:i])) {
					return
				}
				start = -1
			}
		}
	}
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// IsTextStopword reports whether a lowercase word of a text search is filler.
func IsTextStopword(w string) bool {
	_, ok := textStopwords[w]
	return ok
}

// QueryTokens tokenizes a query for ranking and drops its stopwords.
func QueryTokens(query string) Set {
	s := Tokenize(query)
	for w := range rankStopwords {
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

// Searchable reports whether a doc's body may feed ranking, section menus or
// text matches: restricted bodies never do.
func Searchable(doc *vault.Doc) bool { return doc.Sensitivity != gate.Restricted }
