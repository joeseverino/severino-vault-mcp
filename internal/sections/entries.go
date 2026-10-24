package sections

import (
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// A log note is append-only: each top-level bullet in its body is one entry,
// and an entry is closed once its line carries the resolution mark followed
// by text.

// ResolutionMark separates an entry from its resolution.
const ResolutionMark = " → "

// Entry is one log entry: its index in the body's lines, its 1-based line in
// the file, its text without the bullet, and its resolution when closed.
type Entry struct {
	Index      int
	Line       int
	Text       string
	Resolution string
	Closed     bool
}

// Entries reads a log body whose first line is line first of the file.
func Entries(body string, first int) []Entry {
	var out []Entry
	for i, raw := range Lines(body) {
		rest, ok := strings.CutPrefix(raw, "- ")
		if !ok {
			rest, ok = strings.CutPrefix(raw, "* ")
		}
		if !ok {
			continue
		}
		text := strings.TrimRight(rest, " \t\r")
		e := Entry{Index: i, Line: first + i, Text: text}
		if _, after, found := strings.Cut(text, ResolutionMark); found && pystr.Strip(after) != "" {
			e.Closed, e.Resolution = true, pystr.Strip(after)
		}
		out = append(out, e)
	}
	return out
}
