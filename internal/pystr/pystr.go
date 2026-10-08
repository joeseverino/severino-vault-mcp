// Package pystr reproduces the handful of Python string behaviors the vault's
// outputs depend on: repr() in error messages, str() of parsed values,
// str.splitlines, and text-mode reads (universal newlines, invalid bytes
// replaced).
package pystr

import (
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Repr is Python's repr() for a str.
func Repr(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == '\\':
			sb.WriteString(`\\`)
		case r == rune(quote):
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			sb.WriteString(`\x`)
			sb.WriteString(strconv.FormatInt(int64(r)+0x100, 16)[1:])
		case !unicode.IsPrint(r) && r > 0x7f:
			switch {
			case r <= 0xff:
				sb.WriteString(`\x` + strconv.FormatInt(int64(r)+0x100, 16)[1:])
			case r <= 0xffff:
				sb.WriteString(`\u` + strconv.FormatInt(int64(r)+0x10000, 16)[1:])
			default:
				sb.WriteString(`\U` + strconv.FormatInt(int64(r)+0x100000000, 16)[1:])
			}
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte(quote)
	return sb.String()
}

// ReprList is Python's repr() for a list of str.
func ReprList(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = Repr(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ReprAny is repr() for the values the frontmatter parser produces.
func ReprAny(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return Repr(x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = ReprAny(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		return ReprList(x)
	}
	return Str(v)
}

// Str is Python's str() for parsed values.
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
	case []any, []string:
		return ReprAny(x)
	}
	return ""
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

// Len counts code points, as Python's len() does for str.
func Len(s string) int { return utf8.RuneCountInString(s) }

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
