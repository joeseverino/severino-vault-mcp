package write

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// CreateDoc writes a new doc, frontmatter and body, in one atomic create.
// The block is built and checked the way set_frontmatter creates one, so a
// created doc always passes validate.
func CreateDoc(l *vault.Loader, relativePath, body string, c Changes, p *schema.Profile) *jsonx.Obj {
	if msg := c.undeclared(p); msg != "" {
		return fail(msg)
	}
	if absent := absentForCreate(c); len(absent) > 0 {
		return fail("creating a doc needs " + strings.Join(absent, ", "))
	}
	if strings.HasPrefix(pystr.LStrip(body), "---") {
		return fail("body starts with a `---` block; pass the body without frontmatter and give its fields as arguments")
	}
	fm := newFrontmatter(c)
	v, err := fsx.OpenVault(l.Config.VaultPath)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	defer v.Close()
	rel, err := v.Rel(strings.TrimLeft(relativePath, "/"))
	if err != nil {
		return fail("path escapes vault root: " + relativePath)
	}
	if refused := readOnly(l, rel); refused != nil {
		return refused
	}
	if !vault.IsDocPath(rel, l.Config.IndexedDirs) {
		return fail("relative_path must be a .md file under the indexed dirs " + pystr.ReprList(l.Config.IndexedDirs) + ": " + relativePath)
	}
	exists := fail("file already exists: " + rel + "; edit its body with write_doc mode replace_section or append_section")
	if _, err := v.Stat(rel); err == nil {
		return exists
	}
	if errs := checkNew(fm, rel, p, l.Index(true)); len(errs) > 0 {
		return fail(strings.Join(errs, "; "))
	}
	if !hasH1(body) {
		body = "# " + c.Str("title") + "\n\n" + body
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	text := frontmatter.Serialize(fm) + body
	if err := v.MkdirAll(filepath.Dir(rel)); err != nil {
		return fail("write failed: " + err.Error())
	}
	if err := v.AtomicCreate(rel, text); err != nil {
		if errors.Is(err, fsx.ErrExists) {
			return exists
		}
		return fail("write failed: " + err.Error())
	}
	l.Index(true)
	return jsonx.New("ok", true, "doc_id", c.Str("doc_id"), "relative_path", rel, "wrote_bytes", len(text), "next_step", nextStep)
}

func hasH1(body string) bool {
	for _, h := range sections.Headings(body) {
		if h.Level == 1 {
			return true
		}
	}
	return false
}

// EditSection replaces or appends to the section under one heading of an
// existing doc. The section runs to the next heading of the same or higher
// level.
func EditSection(l *vault.Loader, relativePath, heading, body string, appendMode bool, p *schema.Profile) *jsonx.Obj {
	if appendMode && pystr.Strip(body) == "" {
		return fail("body is empty; nothing to append")
	}
	mode := "replace_section"
	if appendMode {
		mode = "append_section"
	}
	return EditBody(l, relativePath, p, func(old, rel string) (string, *jsonx.Obj) {
		return spliceSection(old, heading, body, appendMode, rel)
	}, "mode", mode, "heading", pystr.Strip(heading))
}

// EditBody rewrites an existing doc's body through edit, which gets the body
// and the vault-relative path and returns the new body or a failure. The
// frontmatter block is kept byte for byte except last_reviewed, which becomes
// today, and must still validate; the file is replaced atomically. extra are
// key/value pairs the result carries after relative_path.
func EditBody(l *vault.Loader, relativePath string, p *schema.Profile, edit func(body, rel string) (string, *jsonx.Obj), extra ...any) *jsonx.Obj {
	return editBody(l, relativePath, p, edit, true, extra...)
}

// EditBodyUnindexed is EditBody without rebuilding the index after the write,
// for a note read from disk on every use (a work queue), so a hook's one
// write does not pay for a scan of the whole vault.
func EditBodyUnindexed(l *vault.Loader, relativePath string, p *schema.Profile, edit func(body, rel string) (string, *jsonx.Obj), extra ...any) *jsonx.Obj {
	return editBody(l, relativePath, p, edit, false, extra...)
}

func editBody(l *vault.Loader, relativePath string, p *schema.Profile, edit func(body, rel string) (string, *jsonx.Obj), reindex bool, extra ...any) *jsonx.Obj {
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	rel, _ := filepath.Rel(fsx.Resolve(l.Config.VaultPath), full)
	var out *jsonx.Obj
	size := 0
	wrote, err := v.Update(full, func(text string) string {
		out, size = nil, 0
		cut := frontmatter.BodyOffset(text)
		if cut == 0 {
			out = fail("file has no frontmatter; register it with set_frontmatter first.")
			return text
		}
		head, old := text[:cut], text[cut:]
		newBody, ferr := edit(old, rel)
		if ferr != nil {
			out = ferr
			return text
		}
		newText := touchReviewed(head, Today()) + newBody
		fm, _, _ := frontmatter.Split(newText)
		if errs := p.Check(fm, rel); len(errs) > 0 {
			out = fail("frontmatter does not validate: " + strings.Join(errs, "; ") + "; fix it with set_frontmatter, then retry")
			return text
		}
		docID, _ := fm.Get("doc_id")
		out = jsonx.New("ok", true, "doc_id", docID, "relative_path", rel)
		for i := 0; i+1 < len(extra); i += 2 {
			out.Set(extra[i].(string), extra[i+1])
		}
		size = len(newText)
		return newText
	})
	if err != nil {
		return writeFailed(err)
	}
	if !wrote {
		if failed(out) {
			return out
		}
		return out.Set("no_op", true).Set("wrote_bytes", 0)
	}
	if reindex {
		l.Index(true)
	}
	return out.Set("wrote_bytes", size).Set("next_step", nextStep)
}

// spliceSection rewrites the content under heading in body; heading ""
// names the preamble, the text before the first heading.
func spliceSection(body, heading, content string, appendMode bool, rel string) (string, *jsonx.Obj) {
	needle := pystr.Strip(heading)
	all := sections.Headings(body)
	if needle == "" {
		return splicePreamble(body, all, content, appendMode), nil
	}
	texts := make([]string, len(all))
	var matches []sections.Heading
	for i, h := range all {
		texts[i] = h.Text
		if h.Text == needle {
			matches = append(matches, h)
		}
	}
	switch {
	case len(all) == 0:
		return "", fail(rel + " has no headings; create the doc's sections with write_doc mode create")
	case len(matches) == 0:
		return "", fail(fmt.Sprintf("no heading %s in %s; pass one of %s exactly", pystr.Repr(needle), rel, pystr.ReprList(texts)))
	case len(matches) > 1:
		return "", fail(fmt.Sprintf("heading %s appears %d times in %s; rename one so it is unique, then retry. Headings: %s",
			pystr.Repr(needle), len(matches), rel, pystr.ReprList(texts)))
	}
	target := matches[0]
	lines := strings.Split(body, "\n")
	end := len(lines)
	for _, h := range all {
		if h.Line > target.Line && h.Level <= target.Level {
			end = h.Line
			break
		}
	}
	section := strings.Trim(content, "\n")
	if existing := strings.Trim(strings.Join(lines[target.Line+1:end], "\n"), "\n"); appendMode && existing != "" {
		section = existing + "\n\n" + section
	}
	var sb strings.Builder
	for _, line := range lines[:target.Line] {
		sb.WriteString(line + "\n")
	}
	sb.WriteString(lines[target.Line] + "\n")
	if section != "" {
		sb.WriteString("\n" + section + "\n")
	}
	if end < len(lines) {
		sb.WriteString("\n" + strings.Join(lines[end:], "\n"))
	}
	return sb.String(), nil
}

// splicePreamble rewrites the text before the first heading of body (all of
// it when there is none), keeping the blank lines that open the body.
func splicePreamble(body string, all []sections.Heading, content string, appendMode bool) string {
	lines := strings.Split(body, "\n")
	end := len(lines)
	if len(all) > 0 {
		end = all[0].Line
	}
	section := strings.Trim(content, "\n")
	if existing := strings.Trim(strings.Join(lines[:end], "\n"), "\n"); appendMode && existing != "" {
		section = existing + "\n\n" + section
	}
	lead := body[:len(body)-len(strings.TrimLeft(body, "\n"))]
	rest := strings.Join(lines[end:], "\n")
	switch {
	case section == "" && end < len(lines):
		return lead + rest
	case section == "":
		return lead
	case end < len(lines):
		return lead + section + "\n\n" + rest
	}
	return lead + section + "\n"
}

// touchReviewed sets last_reviewed in a frontmatter block, leaving every
// other byte alone; a block without the field gets it before the closing fence.
func touchReviewed(head, today string) string {
	lines := strings.SplitAfter(head, "\n")
	line := "last_reviewed: " + today + "\n"
	fences := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "last_reviewed:") {
			lines[i] = line
			return strings.Join(lines, "")
		}
		if pystr.Strip(l) == "---" {
			fences++
			if fences == 2 {
				lines[i] = line + l
				return strings.Join(lines, "")
			}
		}
	}
	return head
}
