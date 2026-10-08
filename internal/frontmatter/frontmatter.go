// Package frontmatter parses and serializes the vault's constrained YAML
// subset: scalars, inline and block lists, and folded or literal multiline
// scalars. Dates and numbers stay strings. One serializer backs every write,
// so escaping rules never fork between writers.
package frontmatter

import (
	"regexp"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

var (
	blockListItem = regexp.MustCompile(`^[ \t\n\r\f\v]+-[ \t\n\r\f\v]+`)
	keyLine       = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)[ \t\n\r\f\v]*:[ \t\n\r\f\v]*(.*)$`)
	keyStart      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*[ \t\n\r\f\v]*:`)
)

var blockScalars = map[string]bool{">": true, ">-": true, ">+": true, "|": true, "|-": true, "|+": true}

// Scalar parses one token: quotes stripped, null/~/empty to nil, true/false
// to bool, everything else a string.
func Scalar(token string) any {
	token = pystr.Strip(token)
	if len(token) >= 2 && token[0] == token[len(token)-1] && (token[0] == '"' || token[0] == '\'') {
		token = token[1 : len(token)-1]
	}
	switch token {
	case "null", "~", "":
		return nil
	case "true":
		return true
	case "false":
		return false
	}
	return token
}

// SplitInlineList splits the inside of a [a, b, c] list, respecting nesting.
func SplitInlineList(inner string) []string {
	if inner == "" {
		return nil
	}
	var out []string
	depth := 0
	var buf strings.Builder
	for _, ch := range inner {
		if ch == ',' && depth == 0 {
			out = append(out, pystr.Strip(buf.String()))
			buf.Reset()
			continue
		}
		buf.WriteRune(ch)
		switch ch {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		}
	}
	out = append(out, pystr.Strip(buf.String()))
	kept := out[:0]
	for _, v := range out {
		if v != "" {
			kept = append(kept, v)
		}
	}
	return kept
}

// ParseBlock parses the text between the frontmatter fences.
func ParseBlock(block string) *jsonx.Obj {
	data := jsonx.New()
	currentList := ""
	lines := pystr.SplitLines(block)
	for i := 0; i < len(lines); {
		raw := lines[i]
		i++
		if pystr.Strip(raw) == "" || strings.HasPrefix(pystr.LStrip(raw), "#") {
			currentList = ""
			continue
		}
		if currentList != "" && blockListItem.MatchString(raw) {
			item := ""
			if stripped := pystr.Strip(raw); len(stripped) > 2 {
				item = pystr.Strip(stripped[2:])
			}
			existing, ok := data.Get(currentList)
			list, isList := existing.([]any)
			if !ok || !isList {
				list = []any{}
			}
			data.Set(currentList, append(list, Scalar(item)))
			continue
		}
		m := keyLine.FindStringSubmatch(raw)
		if m == nil {
			currentList = ""
			continue
		}
		key, value := m[1], pystr.Strip(m[2])
		if blockScalars[value] {
			var blockLines []string
			for i < len(lines) {
				candidate := lines[i]
				if keyStart.MatchString(candidate) {
					break
				}
				blockLines = append(blockLines, pystr.Strip(candidate))
				i++
			}
			if strings.HasPrefix(value, "|") {
				data.Set(key, pystr.Strip(strings.Join(blockLines, "\n")))
			} else {
				var parts []string
				for _, line := range blockLines {
					if line != "" {
						parts = append(parts, line)
					}
				}
				data.Set(key, pystr.Strip(strings.Join(parts, " ")))
			}
			currentList = ""
			continue
		}
		if value == "" {
			currentList = key
			data.Set(key, []any{})
			continue
		}
		currentList = ""
		if inner, ok := cutInlineList(value); ok {
			items := []any{}
			for _, item := range SplitInlineList(pystr.Strip(inner)) {
				items = append(items, Scalar(item))
			}
			data.Set(key, items)
			continue
		}
		data.Set(key, Scalar(value))
	}
	return data
}

// Split returns the frontmatter (nil when absent), the body, and the
// 1-indexed line where the body starts.
func Split(text string) (*jsonx.Obj, string, int) {
	if !strings.HasPrefix(pystr.LStrip(text), "---") {
		return nil, text, 1
	}
	lines := pystr.SplitLines(text)
	start := -1
	for i, line := range lines {
		if pystr.Strip(line) == "---" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, text, 1
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if pystr.Strip(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, text, 1
	}
	fm := ParseBlock(strings.Join(lines[start+1:end], "\n"))
	body := strings.TrimLeft(strings.Join(lines[end+1:], "\n"), "\n")
	return fm, body, end + 2
}

// BodyOffset is the byte offset where the body starts in text: past the
// closing fence line and any blank lines after it. 0 when there is no block.
func BodyOffset(text string) int {
	if !strings.HasPrefix(pystr.LStrip(text), "---") {
		return 0
	}
	fences, pos := 0, 0
	for pos < len(text) {
		end := strings.IndexByte(text[pos:], '\n')
		line := text[pos:]
		next := len(text)
		if end >= 0 {
			line = text[pos : pos+end]
			next = pos + end + 1
		}
		if pystr.Strip(line) == "---" {
			fences++
			if fences == 2 {
				for next < len(text) && text[next] == '\n' {
					next++
				}
				return next
			}
		}
		pos = next
	}
	return 0
}

// Read parses one file's frontmatter (nil when absent).
func Read(path string) (*jsonx.Obj, error) {
	text, err := pystr.ReadText(path)
	if err != nil {
		return nil, err
	}
	fm, _, _ := Split(text)
	return fm, nil
}

var knownKeyOrder = []string{
	"doc_id", "title", "doc_type", "system", "environment", "status",
	"sensitivity", "last_reviewed", "related_projects", "related_assets",
	"tags", "notes",
	// Task-profile fields; other doc types never carry them.
	"effort", "priority", "created", "closed",
}

const yamlSpecialChars = ":#@|>{}[],&*!%`"

// Escape quotes a scalar only when the YAML subset requires it.
func Escape(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, yamlSpecialChars) {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	if pystr.Strip(value) != value {
		return `"` + value + `"`
	}
	return value
}

// Serialize renders a frontmatter block: known keys first in canonical
// order, then the rest in insertion order. Ends with the closing fence and
// one blank line.
func Serialize(data *jsonx.Obj) string {
	lines := []string{"---"}
	seen := map[string]bool{}
	keys := append(append([]string{}, knownKeyOrder...), data.Keys()...)
	for _, key := range keys {
		if seen[key] || !data.Has(key) {
			continue
		}
		seen[key] = true
		value, _ := data.Get(key)
		switch x := value.(type) {
		case []any:
			if len(x) == 0 {
				lines = append(lines, key+": []")
				continue
			}
			lines = append(lines, key+":")
			for _, item := range x {
				lines = append(lines, "  - "+Escape(pystr.Str(item)))
			}
		case []string:
			if len(x) == 0 {
				lines = append(lines, key+": []")
				continue
			}
			lines = append(lines, key+":")
			for _, item := range x {
				lines = append(lines, "  - "+Escape(item))
			}
		case nil:
			lines = append(lines, key+": null")
		case bool:
			if x {
				lines = append(lines, key+": true")
			} else {
				lines = append(lines, key+": false")
			}
		default:
			lines = append(lines, key+": "+Escape(pystr.Str(x)))
		}
	}
	lines = append(lines, "---", "")
	return strings.Join(lines, "\n") + "\n"
}

func cutInlineList(value string) (string, bool) {
	inner, ok := strings.CutPrefix(value, "[")
	if !ok {
		return "", false
	}
	return strings.CutSuffix(inner, "]")
}
