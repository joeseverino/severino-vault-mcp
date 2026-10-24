package sections

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Target is the span of a body's lines, [Start, End), that one section read
// or edit covers. Line is the heading line it opens with, or -1 for the text
// before the first heading or a later part of a hard-wrapped heading; Path
// names it the way read_doc's outline does, and Slug is its first section's
// slug, "" for a heading no section opens.
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

// layout is one parse of a body: its sections, its headings, and the slug of
// the first section each heading line opens. Every span a read or edit names
// is cut from it.
type layout struct {
	lines    []string
	secs     []Section
	headings []Heading
	byLine   map[int]Heading
	slugs    map[int]string
}

func newLayout(body string) *layout {
	l := &layout{lines: Lines(body), secs: Parse(body, 0), byLine: map[int]Heading{}, slugs: map[int]string{}}
	l.headings = headingsOf(l.lines)
	for _, h := range l.headings {
		l.byLine[h.Line] = h
	}
	for _, s := range l.secs {
		if s.Level == 0 {
			continue
		}
		if h, _ := l.under(s.StartLine + 1); l.slugs[h.Line] == "" {
			l.slugs[h.Line] = s.Slug
		}
	}
	return l
}

// under is the nearest heading above line at, as Body.Under.
func (l *layout) under(at int) (Heading, bool) {
	i := sort.Search(len(l.headings), func(i int) bool { return l.headings[i].Line >= at })
	if i == 0 {
		return Heading{}, false
	}
	return l.headings[i-1], true
}

// block is the heading at line and everything up to the next heading of its
// level or higher, as Body.BlockEnd.
func (l *layout) block(line int) Target {
	h := l.byLine[line]
	end := len(l.lines)
	for _, o := range l.headings {
		if o.Line > line && o.Level <= h.Level {
			end = o.Line
			break
		}
	}
	return Target{Start: line, End: end, Line: line, Level: h.Level, Text: h.Text, Path: PathOf(l.headings, h), Slug: l.slugs[line]}
}

// part is just the lines of one section: a hard-wrapped part, or the text
// before the first heading.
func (l *layout) part(s Section) Target {
	line := -1
	if h, ok := l.byLine[s.StartLine]; ok && s.Level > 0 {
		line = h.Line
	}
	return Target{Start: s.StartLine, End: s.End(), Line: line, Level: s.Level, Text: s.Heading, Path: s.HeadingPath, Slug: s.Slug}
}

// read is the span a read of section s by its own slug returns: its
// heading's whole block, or just its lines for a part or the preamble.
func (l *layout) read(s Section) Target {
	if s.Level == 0 || s.Part > 0 {
		return l.part(s)
	}
	h, _ := l.under(s.StartLine + 1)
	return l.block(h.Line)
}

// Targets are the spans ref names in body, which starts at line 0. A heading
// named by its text, its heading path ("Parent > Child") or its section's
// slug is a block in the Markdown sense: the heading and everything up to the
// next heading of the same or a higher level, so its subheadings come with
// it. The token cap that splits a long H2 into parts for find is no
// boundary here; only a part's own slug or path (a "(part N)" outline entry)
// names just that part. More than one Target means ref is ambiguous.
func Targets(body, ref string) []Target { return newLayout(body).targets(ref) }

func (l *layout) targets(ref string) []Target {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return nil
	}
	var out []Target
	seen := map[int]bool{}
	add := func(line int) {
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, l.block(line))
	}
	nslug := Slugify(needle)
	for _, m := range Matches(l.secs, ref) {
		if m.Level == 0 {
			continue
		}
		// A slug given as written names one section; heading text that
		// lowercases to a slug may still name several.
		named := strings.TrimSpace(ref) == m.Slug
		if m.Part > 0 && (named || needle == strings.ToLower(m.Slug) || needle == strings.ToLower(m.HeadingPath) || nslug == m.Slug) {
			return []Target{l.part(*m)}
		}
		h, _ := l.under(m.StartLine + 1)
		add(h.Line)
		if named {
			return out
		}
	}
	for _, h := range l.headings {
		path := PathOf(l.headings, h)
		if needle == strings.ToLower(h.Text) || needle == strings.ToLower(path) || nslug == Slugify(path) {
			add(h.Line)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// OutlineEntry is one line of a doc's outline: a section and the span
// reading it by its slug returns.
type OutlineEntry struct {
	Section Section
	Target  Target
}

// Outline is body's sections in order, each with the span a read of it
// returns; an entry's 1-based position is a name Address accepts.
func Outline(body string) []OutlineEntry {
	l := newLayout(body)
	out := make([]OutlineEntry, len(l.secs))
	for i, s := range l.secs {
		out[i] = OutlineEntry{s, l.read(s)}
	}
	return out
}

// Address is the spans ref names for a read, the one resolver read_doc
// uses. It tries, in order: the slug of the text before the first heading;
// every form Targets takes (heading text, heading path, section slug, a
// part's slug); a 1-based position in Outline; a prefix of section slugs.
// One Target is found, none is missing, several are ambiguous. A prefix
// that matches a section and the sections inside it names the outer one.
func Address(body, ref string) []Target {
	needle := strings.TrimSpace(ref)
	if needle == "" {
		return nil
	}
	l := newLayout(body)
	for _, s := range l.secs {
		if s.Level == 0 && strings.EqualFold(needle, s.Slug) {
			return []Target{l.part(s)}
		}
	}
	if out := l.targets(ref); len(out) > 0 {
		return out
	}
	if strings.Trim(needle, "0123456789") == "" {
		n, err := strconv.Atoi(needle)
		if err != nil || n < 1 || n > len(l.secs) {
			return nil
		}
		return []Target{l.read(l.secs[n-1])}
	}
	prefix := Slugify(needle)
	if prefix == "" {
		return nil
	}
	var out []Target
	for _, s := range l.secs {
		if !strings.HasPrefix(s.Slug, prefix) {
			continue
		}
		t := l.read(s)
		if !slices.ContainsFunc(out, func(o Target) bool { return o.Start == t.Start && o.End == t.End }) {
			out = append(out, t)
		}
	}
	for _, t := range out {
		if !slices.ContainsFunc(out, func(o Target) bool { return o.Start < t.Start || o.End > t.End }) {
			return []Target{t}
		}
	}
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
