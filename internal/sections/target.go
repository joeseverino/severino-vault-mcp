package sections

import (
	"slices"
	"sort"
	"strings"
)

// Target is the span of a body's lines, [Start, End), that one section read
// or edit covers. Line is the heading line it opens with, or -1 for a later
// part of a hard-wrapped heading; Path names it the way read_doc's outline
// does, and Slug is its first section's slug, "" for a heading no section
// opens.
type Target struct {
	Start, End int
	Line       int
	Level      int
	Text       string
	Path       string
	Slug       string
}

// Span is the target's text in body, the body Targets was given.
func (t Target) Span(body string) string {
	return strings.Join(Lines(body)[t.Start:t.End], "\n")
}

// Name is how the target is named back to read_doc and write_doc: its slug,
// or its heading path's slug for a heading no section opens.
func (t Target) Name() string {
	if t.Slug != "" {
		return t.Slug
	}
	return Slugify(t.Path)
}

// Targets are the spans ref names in body, which starts at line 0. A heading
// named by its text, its heading path ("Parent > Child") or its section's
// slug is a block in the Markdown sense: the heading and everything up to the
// next heading of the same or a higher level, so its subheadings come with
// it. The token cap that splits a long H2 into parts for find is no
// boundary here; only a part's own slug or path (a "(part N)" outline entry)
// names just that part. More than one Target means ref is ambiguous.
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
	slugs := map[int]string{}
	for _, s := range secs {
		if s.Level == 0 {
			continue
		}
		if h, _ := b.Under(s.StartLine + 1); slugs[h.Line] == "" {
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
		out = append(out, Target{Start: line, End: b.BlockEnd(line), Line: line, Level: h.Level, Text: h.Text, Path: PathOf(headings, h), Slug: slugs[line]})
	}
	nslug := Slugify(needle)
	for _, m := range Matches(secs, ref) {
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
		path := PathOf(headings, h)
		if needle == strings.ToLower(h.Text) || needle == strings.ToLower(path) || nslug == Slugify(path) {
			add(h.Line)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// PathOf is h's heading path: the text of each enclosing heading below H1,
// outermost first, then its own, joined with " > ".
func PathOf(headings []Heading, h Heading) string {
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
