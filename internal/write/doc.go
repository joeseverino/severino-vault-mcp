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

// NewDoc is a created doc's frontmatter. Empty Environment, Status and
// Sensitivity take set_frontmatter's defaults; empty LastReviewed is today.
type NewDoc struct {
	DocID, Title, DocType, System, Environment, Status, Sensitivity, LastReviewed string
	Tags, RelatedProjects, RelatedAssets                                          []string
}

// CreateDoc writes a new doc, frontmatter and body, in one atomic create.
// The block is checked with the rules validate applies, so a created doc
// always passes validate.
func CreateDoc(l *vault.Loader, relativePath, body string, n NewDoc, p *schema.Profile) *jsonx.Obj {
	var absent []string
	for _, f := range [][2]string{{"doc_id", n.DocID}, {"title", n.Title}, {"doc_type", n.DocType}, {"system", n.System}} {
		if pystr.Strip(f[1]) == "" {
			absent = append(absent, f[0])
		}
	}
	if len(absent) > 0 {
		return fail("creating a doc needs " + strings.Join(absent, ", "))
	}
	if strings.HasPrefix(pystr.LStrip(body), "---") {
		return fail("body starts with a `---` block; pass the body without frontmatter and give its fields as arguments")
	}
	fm := newFrontmatter(n.DocID, n.Title, n.DocType, n.System, def(n.Environment, "other"), def(n.Status, "active"),
		def(n.Sensitivity, "internal"), n.LastReviewed, n.Tags, n.RelatedProjects, n.RelatedAssets, n.Tags != nil)
	if errs := p.Check(fm); len(errs) > 0 {
		return fail(strings.Join(errs, "; "))
	}
	v, err := fsx.OpenVault(l.Config.VaultPath)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	defer v.Close()
	rel, err := v.Rel(strings.TrimLeft(relativePath, "/"))
	if err != nil {
		return fail("path escapes vault root: " + relativePath)
	}
	if !vault.IsDocPath(rel, l.Config.IndexedDirs) {
		return fail("relative_path must be a .md file under the indexed dirs " + pystr.ReprList(l.Config.IndexedDirs) + ": " + relativePath)
	}
	exists := fail("file already exists: " + rel + "; edit its body with write_doc mode replace_section or append_section")
	if _, err := v.Stat(rel); err == nil {
		return exists
	}
	if taken := docIDTaken(l.Index(true), n.DocID); taken != nil {
		return taken
	}
	if !hasH1(body) {
		body = "# " + n.Title + "\n\n" + body
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
	return jsonx.New("ok", true, "doc_id", n.DocID, "relative_path", rel, "wrote_bytes", len(text), "next_step", nextStep)
}

func def(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
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
// level. The frontmatter block is kept byte for byte except last_reviewed,
// which becomes today, and must still validate.
func EditSection(l *vault.Loader, relativePath, heading, body string, appendMode bool, p *schema.Profile) *jsonx.Obj {
	if appendMode && pystr.Strip(body) == "" {
		return fail("body is empty; nothing to append")
	}
	v, full, perr := fsx.IndexedPath(l.Config, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	rel, _ := filepath.Rel(fsx.Resolve(l.Config.VaultPath), full)
	text, err := v.ReadText(full)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	cut := frontmatter.BodyOffset(text)
	if cut == 0 {
		return fail("file has no frontmatter; register it with set_frontmatter first.")
	}
	head, old := text[:cut], text[cut:]
	newBody, ferr := spliceSection(old, heading, body, appendMode, rel)
	if ferr != nil {
		return ferr
	}
	newText := touchReviewed(head, Today()) + newBody
	fm, _, _ := frontmatter.Split(newText)
	if errs := p.Check(fm); len(errs) > 0 {
		return fail("frontmatter does not validate: " + strings.Join(errs, "; ") + "; fix it with set_frontmatter, then retry")
	}
	docID, _ := fm.Get("doc_id")
	mode := "replace_section"
	if appendMode {
		mode = "append_section"
	}
	out := jsonx.New("ok", true, "doc_id", docID, "relative_path", rel, "mode", mode, "heading", pystr.Strip(heading))
	if newText == text {
		return out.Set("no_op", true).Set("wrote_bytes", 0)
	}
	if err := v.AtomicWrite(full, newText); err != nil {
		return fail("write failed: " + err.Error())
	}
	l.Index(true)
	return out.Set("wrote_bytes", len(newText)).Set("next_step", nextStep)
}

// spliceSection rewrites the content under heading in body.
func spliceSection(body, heading, content string, appendMode bool, rel string) (string, *jsonx.Obj) {
	needle := pystr.Strip(heading)
	all := sections.Headings(body)
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
