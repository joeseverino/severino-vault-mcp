package sections

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Target is the span of a body's lines, [Start, End), that one section edit
// covers. Line is the heading line it opens with, or -1 for a later part of
// a hard-wrapped heading; Path names it the way read_doc's outline does,
// and Slug is its first section's slug, "" for a heading no section opens.
type Target struct {
	Start, End int
	Line       int
	Level      int
	Text       string
	Path       string
	Slug       string
}

// Targets are the spans ref names in body, which starts at line 0. A section
// read_doc returns is the span an edit covers: ref resolves through Matches
// over Parse, so each matched section is exactly what read_doc gives for it.
// A heading resolved by its text or path covers every part it was
// hard-wrapped into; a part's own slug or path covers that part. A heading
// Parse keeps inside a larger section (an H3 of an H2 small enough to stay
// whole, an H4) is found by its text or heading path, "Parent > Child", and
// spans to the next heading of any level. More than one Target means ref is
// ambiguous.
func Targets(body, ref string) []Target {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return nil
	}
	b := NewBody(body)
	secs := Parse(body, 0)
	headings := b.Headings()
	byLine := map[int]Heading{}
	for _, h := range headings {
		byLine[h.Line] = h
	}
	// A heading's span as a section runs to the end of the last section
	// opening under it, the parts it was hard-wrapped into included.
	ends := map[int]int{}
	paths := map[int]string{}
	slugs := map[int]string{}
	for _, s := range secs {
		if s.Level == 0 {
			continue
		}
		h, _ := b.Under(s.StartLine + 1)
		ends[h.Line] = max(ends[h.Line], s.End())
		if _, ok := paths[h.Line]; !ok {
			paths[h.Line] = strings.TrimSuffix(s.HeadingPath, fmt.Sprintf(" (part %d)", s.Part))
			slugs[h.Line] = s.Slug
		}
	}
	var out []Target
	seen := map[int]bool{}
	add := func(line int) {
		if seen[line] {
			return
		}
		seen[line] = true
		h := byLine[line]
		end, ok := ends[line]
		path := paths[line]
		if !ok {
			end, path = b.NextHeading(line), pathOf(headings, h)
		}
		out = append(out, Target{Start: line, End: end, Line: line, Level: h.Level, Text: h.Text, Path: path, Slug: slugs[line]})
	}
	nslug := Slugify(needle)
	matches := Matches(secs, ref)
	for _, m := range matches {
		if m.Level == 0 {
			continue
		}
		// A slug given as written names one section; heading text that
		// lowercases to a slug may still name several.
		named := strings.TrimSpace(ref) == m.Slug
		if m.Part > 0 && (named || needle == strings.ToLower(m.Slug) || needle == strings.ToLower(m.HeadingPath) || nslug == m.Slug) {
			line := -1
			if h, ok := byLine[m.StartLine]; ok {
				line = h.Line
			}
			return []Target{{Start: m.StartLine, End: m.End(), Line: line, Level: m.Level, Text: m.Heading, Path: m.HeadingPath, Slug: m.Slug}}
		}
		h, _ := b.Under(m.StartLine + 1)
		add(h.Line)
		if named {
			return out
		}
	}
	for _, h := range headings {
		path := paths[h.Line]
		if path == "" {
			path = pathOf(headings, h)
		}
		if needle == strings.ToLower(h.Text) || needle == strings.ToLower(path) || nslug == Slugify(path) {
			add(h.Line)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// pathOf is h's heading path: the text of each enclosing heading below H1,
// outermost first, then its own, joined with " > ".
func pathOf(headings []Heading, h Heading) string {
	parts := []string{h.Text}
	level := h.Level
	for _, o := range slices.Backward(headings) {
		if o.Line >= h.Line || o.Level >= level || o.Level < 2 {
			continue
		}
		parts = append([]string{o.Text}, parts...)
		level = o.Level
	}
	return strings.Join(parts, " > ")
}
