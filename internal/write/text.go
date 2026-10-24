package write

import (
	"fmt"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
)

// ReplaceText replaces old with new ("" removes it) in an existing doc's
// body, or only within the section heading names when heading is given (""
// for the text before the first heading). old must occur exactly once there,
// so the edit lands where the caller read it; a refusal for a count other
// than one carries the count and, when the matches fall under different
// headings, their heading paths as sections. The frontmatter is never
// searched or edited, except last_reviewed, which becomes today.
func ReplaceText(l *vault.Loader, relativePath string, heading *string, old, new string, p *schema.Profile) *jsonx.Obj {
	if old == "" {
		return jsonx.Fail("old_text is empty; give the exact text to replace, copied from read_doc")
	}
	if old == new {
		return jsonx.Fail("new_text is the same as old_text; nothing to change")
	}
	extra := []any{"mode", "replace_text"}
	if heading != nil {
		extra = append(extra, "heading", pystr.Strip(*heading))
	}
	return editBody(l, relativePath, p, func(head string, _ *jsonx.Obj, body, rel string) (string, []HeadField, *jsonx.Obj) {
		out, f := replaceText(head, body, heading, old, new, rel)
		return out, nil, f
	}, true, extra...)
}

func replaceText(head, body string, heading *string, old, new, rel string) (string, *jsonx.Obj) {
	from, to, where := 0, len(body), rel
	if heading != nil {
		var f *jsonx.Obj
		if from, to, f = scope(body, pystr.Strip(*heading), rel); f != nil {
			return "", f
		}
		where = fmt.Sprintf("section %q of %s", pystr.Strip(*heading), rel)
		if pystr.Strip(*heading) == "" {
			where = "the text before the first heading of " + rel
		}
	}
	at := occurrences(body[from:to], old)
	switch len(at) {
	case 1:
		i := from + at[0]
		return body[:i] + new + body[i+len(old):], nil
	case 0:
		msg := "old_text does not occur in " + where
		if elsewhere := len(occurrences(body, old)); elsewhere > 0 {
			msg += fmt.Sprintf("; it occurs %d times elsewhere in the body, so give the heading that holds it or leave heading out", elsewhere)
		} else if strings.Contains(head, old) {
			msg += "; only the frontmatter has it, and frontmatter is changed with set_frontmatter"
		} else {
			msg += "; copy it exactly from read_doc, whitespace and line breaks included"
		}
		return "", jsonx.Fail(msg).Set("count", 0)
	}
	f := jsonx.Fail(fmt.Sprintf("old_text occurs %d times in %s; give a longer old_text that only the one you mean contains, or a heading to narrow it", len(at), where)).
		Set("count", len(at))
	if paths := sectionsOf(body, from, at); paths != nil {
		f.Set("sections", jsonx.Strings(paths))
	}
	return "", f
}

// scope is the byte span of body that heading names: the block read_doc
// returns for it, heading line included, or the text before the first
// heading for "".
func scope(body, heading, rel string) (from, to int, failure *jsonx.Obj) {
	b := sections.NewBody(body)
	all := b.Headings()
	if heading == "" {
		if len(all) == 0 {
			return 0, len(body), nil
		}
		return 0, lineOffset(b.Lines, all[0].Line), nil
	}
	if len(all) == 0 {
		return 0, 0, jsonx.Fail(rel + " has no headings; leave heading out to search the whole body")
	}
	t, f := oneTarget(strings.TrimSuffix(body, "\n"), heading, rel, all)
	if f != nil {
		return 0, 0, f
	}
	return lineOffset(b.Lines, t.Start), min(lineOffset(b.Lines, t.End), len(body)), nil
}

// lineOffset is the byte offset in the text lines were split from where line
// n starts, or the text's length for n past the last line.
func lineOffset(lines []string, n int) int {
	off := 0
	for i := 0; i < n && i < len(lines); i++ {
		off += len(lines[i]) + 1
	}
	return off
}

// occurrences are the offsets in s of every match of sub, overlapping ones
// included, so text that matches twice in a run counts twice.
func occurrences(s, sub string) []int {
	var at []int
	for i := 0; i <= len(s); {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			break
		}
		at = append(at, i+j)
		i += j + 1
	}
	return at
}

// sectionsOf are the distinct heading paths of the headings the matches at
// offsets from+at fall under, in order, or nil when they all share one: a
// heading that tells them apart is a way to name just one. "" is the text
// before the first heading.
func sectionsOf(body string, from int, at []int) []string {
	b := sections.NewBody(body)
	all := b.Headings()
	var paths []string
	seen := map[string]bool{}
	for _, a := range at {
		line := strings.Count(body[:from+a], "\n")
		path := ""
		if h, ok := b.Under(line + 1); ok {
			path = sections.PathOf(all, h)
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if len(paths) < 2 {
		return nil
	}
	return paths
}
