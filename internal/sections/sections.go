// Package sections chunks a markdown body into addressable spans: split at
// H2, sub-split an oversized H2 at its H3s, hard-wrap anything still too big.
// Each span gets a slug unique within its doc.
package sections

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
)

// DefaultTokenCap is the size (about 4 chars per token) past which an H2 is split.
const DefaultTokenCap = 400

var (
	fenceRE  = regexp.MustCompile("^[ \t\n\r\f\v]*(```|~~~)")
	atxRE    = regexp.MustCompile(`^(#{1,6})[ \t\n\r\f\v]+(.*?)[ \t\n\r\f\v]*#*[ \t\n\r\f\v]*$`)
	sentence = regexp.MustCompile(`(.+?[.!?])(\s|$)`)
)

// Section is one addressable span of a doc body.
type Section struct {
	Heading     string // H2 text, or "" for the preamble
	Slug        string
	HeadingPath string // e.g. "Routine operations > Backing commands"
	Level       int    // 2, 3 when sub-split, 0 for the preamble
	Body        string // includes its heading line
	StartLine   int    // 1-indexed line in the source file
	Part        int    // 1-based part of a heading hard-wrapped into several, or 0
}

// End is the line after the section: StartLine plus its line count.
func (s Section) End() int { return s.StartLine + len(Lines(s.Body)) }

type block struct {
	heading     string
	level       int
	headingPath string
	lines       []string
	startLine   int
	part        int
}

func estimateTokens(text string) int {
	n := utf8.RuneCountInString(text) / 4
	if n < 1 {
		return 1
	}
	return n
}

// SlugOptions widen Slugify for a slug that names a file.
type SlugOptions struct {
	Unicode bool // keep every letter and digit pystr.IsAlnum accepts, not only [a-z0-9]
	MaxLen  int  // cap in code points; 0 is no cap
}

// Slugify lowercases text and turns each run of characters outside [a-z0-9]
// into one hyphen, with none at either end. It is the only slug function in
// the server; a caller that needs more passes SlugOptions to SlugifyWith.
func Slugify(text string) string { return SlugifyWith(text, SlugOptions{}) }

// SlugifyWith is Slugify with the kept characters and length set by o.
func SlugifyWith(text string, o SlugOptions) string {
	keep := func(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }
	if o.Unicode {
		keep = pystr.IsAlnum
	}
	var sb strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		if !keep(r) {
			dash = true
			continue
		}
		if dash && sb.Len() > 0 {
			sb.WriteByte('-')
		}
		sb.WriteRune(r)
		dash = false
	}
	slug := sb.String()
	if o.MaxLen > 0 {
		slug = strings.TrimRight(pystr.Prefix(slug, o.MaxLen), "-")
	}
	return slug
}

type slugger struct{ counts map[string]int }

func (s *slugger) unique(base string) string {
	if base == "" {
		base = "overview"
	}
	s.counts[base]++
	if n := s.counts[base]; n > 1 {
		return fmt.Sprintf("%s-%d", base, n)
	}
	return base
}

type heading struct {
	level int
	text  string
}

func headingLines(lines []string) map[int]heading {
	out := map[int]heading{}
	for i, code := range Fenced(lines) {
		if code {
			continue
		}
		if hm := atxRE.FindStringSubmatch(lines[i]); hm != nil {
			out[i] = heading{len(hm[1]), pystr.Strip(hm[2])}
		}
	}
	return out
}

// Fenced reports, for each line, whether it is fenced code: a fence line
// itself or a line between an opening fence and its matching close.
func Fenced(lines []string) []bool {
	out := make([]bool, len(lines))
	inFence, fence := false, ""
	for i, line := range lines {
		if m := fenceRE.FindStringSubmatch(line); m != nil {
			tok := m[1]
			if !inFence {
				inFence, fence = true, tok
			} else if tok == fence {
				inFence, fence = false, ""
			}
			out[i] = true
			continue
		}
		out[i] = inFence
	}
	return out
}

// Heading is one ATX heading line outside fenced code.
type Heading struct {
	Line  int // 0-indexed line in the body
	Level int
	Text  string
}

// Headings lists a body's headings in order, split on "\n".
func Headings(body string) []Heading { return headingsOf(Lines(body)) }

func headingsOf(lines []string) []Heading {
	h := headingLines(lines)
	out := make([]Heading, 0, len(h))
	for i, hd := range h {
		out = append(out, Heading{Line: i, Level: hd.level, Text: hd.text})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Line < out[b].Line })
	return out
}

func levelIndexes(h map[int]heading, level int) []int {
	var idx []int
	for i, hd := range h {
		if hd.level == level {
			idx = append(idx, i)
		}
	}
	sort.Ints(idx)
	return idx
}

func hardwrap(b block, cap int) []block {
	var chunks [][]string
	var cur []string
	curTok := 0
	for _, line := range b.lines {
		lt := estimateTokens(line) + 1
		if len(cur) > 0 && curTok+lt > cap {
			chunks = append(chunks, cur)
			cur, curTok = nil, 0
		}
		cur = append(cur, line)
		curTok += lt
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	if len(chunks) <= 1 {
		return []block{b}
	}
	out := make([]block, 0, len(chunks))
	cursor := 0
	for n, chunk := range chunks {
		path := fmt.Sprintf("(part %d)", n+1)
		if b.headingPath != "" {
			path = fmt.Sprintf("%s (part %d)", b.headingPath, n+1)
		}
		out = append(out, block{b.heading, b.level, path, chunk, b.startLine + cursor, n + 1})
		cursor += len(chunk)
	}
	return out
}

func subsplit(b block, cap int) []block {
	if b.level == 0 || estimateTokens(strings.Join(b.lines, "\n")) <= cap {
		return []block{b}
	}
	headings := headingLines(b.lines)
	h3 := levelIndexes(headings, 3)
	if len(h3) == 0 {
		return hardwrap(b, cap)
	}
	var parts []block
	lead := b.lines[:h3[0]]
	for _, line := range lead {
		if pystr.Strip(line) != "" {
			parts = append(parts, block{b.heading, 2, b.headingPath, lead, b.startLine, 0})
			break
		}
	}
	for n, start := range h3 {
		end := len(b.lines)
		if n+1 < len(h3) {
			end = h3[n+1]
		}
		text := headings[start].text
		parts = append(parts, block{text, 3, b.heading + " > " + text, b.lines[start:end], b.startLine + start, 0})
	}
	var out []block
	for _, part := range parts {
		if estimateTokens(strings.Join(part.lines, "\n")) > cap {
			out = append(out, hardwrap(part, cap)...)
		} else {
			out = append(out, part)
		}
	}
	return out
}

// Parse chunks a body that begins at bodyStartLine in its file.
func Parse(body string, bodyStartLine int) []Section {
	return ParseCap(body, bodyStartLine, DefaultTokenCap)
}

// ParseCap is Parse with an explicit token cap.
func ParseCap(body string, bodyStartLine, cap int) []Section {
	lines := Lines(body)
	headings := headingLines(lines)
	h2 := levelIndexes(headings, 2)

	var blocks []block
	first := len(lines)
	if len(h2) > 0 {
		first = h2[0]
	}
	for _, line := range lines[:first] {
		if pystr.Strip(line) != "" {
			blocks = append(blocks, block{"", 0, "", lines[:first], bodyStartLine, 0})
			break
		}
	}
	for n, start := range h2 {
		end := len(lines)
		if n+1 < len(h2) {
			end = h2[n+1]
		}
		text := headings[start].text
		blocks = append(blocks, block{text, 2, text, lines[start:end], bodyStartLine + start, 0})
	}

	s := &slugger{counts: map[string]int{}}
	var out []Section
	for _, b := range blocks {
		for _, sub := range subsplit(b, cap) {
			out = append(out, Section{
				Heading:     sub.heading,
				Slug:        s.unique(Slugify(sub.headingPath)),
				HeadingPath: sub.headingPath,
				Level:       sub.level,
				Body:        strings.Join(sub.lines, "\n"),
				StartLine:   sub.startLine,
				Part:        sub.part,
			})
		}
	}
	return out
}

// Matches are the sections ref names: the one whose slug it is, else every
// section whose heading path or heading it is, case aside, or whose slug is
// ref's slug.
func Matches(secs []Section, ref string) []*Section {
	needle := strings.ToLower(pystr.Strip(ref))
	if needle == "" {
		return nil
	}
	for i := range secs {
		if needle == strings.ToLower(secs[i].Slug) {
			return []*Section{&secs[i]}
		}
	}
	var out []*Section
	nslug := Slugify(needle)
	for i := range secs {
		if needle == strings.ToLower(secs[i].HeadingPath) || needle == strings.ToLower(secs[i].Heading) || nslug == secs[i].Slug {
			out = append(out, &secs[i])
		}
	}
	return out
}

// Summary is the section's first prose sentence, capped at 120 characters.
func Summary(sec Section) string {
	const limit = 120
	lines := Lines(sec.Body)
	start := 0
	if sec.Heading != "" {
		start = 1
	}
	var buf []string
	if start <= len(lines) {
		for _, line := range lines[start:] {
			stripped := pystr.Strip(line)
			if stripped == "" || strings.HasPrefix(stripped, "#") || fenceRE.MatchString(line) {
				if len(buf) > 0 {
					break
				}
				continue
			}
			buf = append(buf, stripped)
			if strings.ContainsAny(stripped, ".!?") {
				break
			}
		}
	}
	text := strings.Join(buf, " ")
	result := text
	if m := sentence.FindStringSubmatch(text); m != nil {
		result = m[1]
	}
	if utf8.RuneCountInString(result) > limit {
		result = pystr.RStrip(pystr.Prefix(result, limit-1)) + "…"
	}
	return result
}
