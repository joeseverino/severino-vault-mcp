package sections

import (
	"slices"
	"strings"
)

// Body is a markdown body as lines, split on "\n" so String gives back the
// same bytes. Every reader and editor of a body's lines and headings works on
// one (archtest.TestOneBodySplitter).
type Body struct{ Lines []string }

// Lines splits a body as every editor indexes it.
func Lines(text string) []string { return strings.Split(text, "\n") }

// NewBody splits text into a Body.
func NewBody(text string) *Body { return &Body{Lines: Lines(text)} }

func (b *Body) String() string { return strings.Join(b.Lines, "\n") }

// Headings lists the body's ATX headings outside fenced code, in order.
func (b *Body) Headings() []Heading { return headingsOf(b.Lines) }

// Find is the first heading at level with text, or false.
func (b *Body) Find(level int, text string) (Heading, bool) {
	hs := b.Headings()
	i := slices.IndexFunc(hs, func(h Heading) bool { return h.Level == level && h.Text == text })
	if i < 0 {
		return Heading{}, false
	}
	return hs[i], true
}

// NextHeading is the line of the first heading of any level after line at,
// or len(Lines).
func (b *Body) NextHeading(at int) int {
	for _, h := range b.Headings() {
		if h.Line > at {
			return h.Line
		}
	}
	return len(b.Lines)
}

// BlockEnd is the line of the first heading after the heading at line at of
// its level or higher, or len(Lines): where the block it opens ends.
func (b *Body) BlockEnd(at int) int {
	hs := b.Headings()
	level := 7
	for _, h := range hs {
		if h.Line == at {
			level = h.Level
		}
		if h.Line > at && h.Level <= level {
			return h.Line
		}
	}
	return len(b.Lines)
}

// Under is the nearest heading above line at, of any level, or false.
func (b *Body) Under(at int) (Heading, bool) {
	var out Heading
	found := false
	for _, h := range b.Headings() {
		if h.Line >= at {
			break
		}
		out, found = h, true
	}
	return out, found
}

func blank(l string) bool { return strings.TrimSpace(l) == "" }

// AppendTo adds line after the last non-blank line before the next heading
// following the heading at level with text, creating that heading at the end
// of the body when it is missing. The section keeps one blank line after its
// heading and one before the next.
func (b *Body) AppendTo(level int, text, line string) {
	h, ok := b.Find(level, text)
	at := h.Line
	if !ok {
		n := len(b.Lines)
		for n > 0 && blank(b.Lines[n-1]) {
			n--
		}
		b.Lines = append(b.Lines[:n], "", strings.Repeat("#", level)+" "+text, "")
		at = n + 1
	}
	end := b.NextHeading(at)
	pos := end
	for pos > at+1 && blank(b.Lines[pos-1]) {
		pos--
	}
	var ins []string
	if pos == at+1 {
		ins = append(ins, "")
	}
	ins = append(ins, line)
	if pos == end {
		ins = append(ins, "")
	}
	b.Lines = slices.Insert(b.Lines, pos, ins...)
}

// Remove deletes the line at, and a blank line it leaves doubled.
func (b *Body) Remove(at int) {
	b.Lines = slices.Delete(b.Lines, at, at+1)
	if at > 0 && at < len(b.Lines) && blank(b.Lines[at-1]) && blank(b.Lines[at]) {
		b.Lines = slices.Delete(b.Lines, at, at+1)
	}
}
