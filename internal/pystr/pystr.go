// Package pystr holds the string behaviors the frontmatter parser and its
// readers are defined by, where the standard library has no equivalent: str()
// and truthiness of parsed values, str.splitlines, and text-mode reads
// (universal newlines, invalid bytes replaced); the one list format for
// messages; and the edit distance behind did-you-mean suggestions.
package pystr

import (
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Str is Python's str() for parsed scalars; a list is its items, Quoted.
func Str(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []string:
		return Quoted(x)
	case []any:
		items := make([]string, len(x))
		for i, item := range x {
			items[i] = Str(item)
		}
		return Quoted(items)
	}
	return ""
}

// Quoted is a list as a message names it: each item quoted, comma-separated
// ("a", "b"). Every list that reaches text goes through here
// (archtest.TestListsReachTextThroughQuoted).
func Quoted(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = strconv.Quote(s)
	}
	return strings.Join(quoted, ", ")
}

// Truthy is Python truthiness for parsed values.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case []any:
		return len(x) > 0
	case []string:
		return len(x) > 0
	}
	return true
}

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// SplitLines is Python's str.splitlines() without keepends.
func SplitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isLineBreak(r) {
			out = append(out, s[start:i])
			i += size
			if r == '\r' && i < len(s) && s[i] == '\n' {
				i++
			}
			start = i
			continue
		}
		i += size
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// Strip is Python's str.strip() with no arguments.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// LStrip is Python's str.lstrip() with no arguments.
func LStrip(s string) string { return strings.TrimLeftFunc(s, IsSpace) }

// RStrip is Python's str.rstrip() with no arguments.
func RStrip(s string) string { return strings.TrimRightFunc(s, IsSpace) }

// IsSpace matches Python's str.isspace for one rune.
func IsSpace(r rune) bool {
	switch r {
	case 0x1c, 0x1d, 0x1e, 0x1f:
		return true
	}
	return unicode.IsSpace(r)
}

// IsAlnum matches Python's str.isalnum for one rune.
func IsAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r)
}

// Prefix returns the first n code points.
func Prefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// ReadText reads a file the way Python's Path.read_text(errors="replace")
// does: invalid UTF-8 replaced, newlines universal.
func ReadText(path string) (string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // callers pass vault paths or operator-supplied files
	if err != nil {
		return "", err
	}
	return Decode(raw), nil
}

// Decode applies text-mode decoding to raw bytes.
func Decode(raw []byte) string {
	s := strings.ToValidUTF8(string(raw), "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// EditDistance is the Levenshtein distance between a and b in bytes.
func EditDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
