package write

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
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
	return createDoc(l, relativePath, body, c, p, true)
}

// CreateNote is CreateDoc writing body as given, for a note whose file name
// and breadcrumb name it without an H1 (a work queue).
func CreateNote(l *vault.Loader, relativePath, body string, c Changes, p *schema.Profile) *jsonx.Obj {
	return createDoc(l, relativePath, body, c, p, false)
}

func createDoc(l *vault.Loader, relativePath, body string, c Changes, p *schema.Profile, titled bool) *jsonx.Obj {
	if msg := c.undeclared(p); msg != "" {
		return jsonx.Fail(msg)
	}
	archive := ""
	if l.Config.Archived(strings.TrimLeft(relativePath, "/")) {
		archive = l.Config.ArchiveDir
	}
	if absent := absentForCreate(c, archive); len(absent) > 0 {
		return jsonx.Fail("creating a doc needs " + strings.Join(absent, ", "))
	}
	if strings.HasPrefix(pystr.LStrip(body), "---") {
		return jsonx.Fail("body starts with a `---` block; pass the body without frontmatter and give its fields as arguments")
	}
	fm := newFrontmatter(c, p)
	v, err := fsx.OpenVault(l.Config.VaultPath)
	if err != nil {
		return jsonx.Fail("write failed: " + err.Error())
	}
	defer v.Close()
	rel, err := v.Rel(strings.TrimLeft(relativePath, "/"))
	if err != nil {
		return jsonx.Fail("path escapes vault root: " + relativePath)
	}
	if refused := readOnly(l, rel); refused != nil {
		return refused
	}
	if archive != "" && !l.Config.Archived(rel) {
		archive = ""
	}
	writable := l.Config.IndexedDirs
	if archive != "" {
		writable = []string{archive}
	}
	if !vault.IsDocPath(rel, writable) {
		return jsonx.Fail("relative_path must be a .md file under the indexed dirs " + pystr.ReprList(l.Config.IndexedDirs) + ": " + relativePath)
	}
	exists := jsonx.Fail("file already exists: " + rel + "; edit its body with write_doc mode replace_section or append_section")
	if _, err := v.Stat(rel); err == nil {
		return exists
	}
	if errs := checkNew(fm, rel, archive, p, l.Index(true)); len(errs) > 0 {
		return jsonx.Fail(strings.Join(errs, "; "))
	}
	if titled && !hasH1(body) {
		body = "# " + c.Str("title") + "\n\n" + body
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	text := frontmatter.Serialize(fm) + body
	if err := v.MkdirAll(filepath.Dir(rel)); err != nil {
		return jsonx.Fail("write failed: " + err.Error())
	}
	if err := v.AtomicCreate(rel, text); err != nil {
		if errors.Is(err, fsx.ErrExists) {
			return exists
		}
		return jsonx.Fail("write failed: " + err.Error())
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

// EditSection replaces or appends to one section of an existing doc. The
// section is what read_doc returns for heading (sections.Targets): replace
// covers all of it, and append adds at the end of the text under its heading,
// before its first subheading.
func EditSection(l *vault.Loader, relativePath, heading, body string, appendMode bool, p *schema.Profile) *jsonx.Obj {
	if appendMode && pystr.Strip(body) == "" {
		return jsonx.Fail("body is empty; nothing to append")
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
	return editBody(l, relativePath, p, func(_ *jsonx.Obj, body, rel string) (string, []HeadField, *jsonx.Obj) {
		out, f := edit(body, rel)
		return out, nil, f
	}, true, extra...)
}

// HeadField is one scalar frontmatter field a NoteEdit sets, written as
// frontmatter.Escape writes the value, or, with Drop, a field it removes
// with any list items under it.
type HeadField struct {
	Key, Value string
	Drop       bool
}

// NoteEdit is a body edit that also reads the frontmatter block as it is on
// disk under the lock, and may set scalar fields in it.
type NoteEdit func(fm *jsonx.Obj, body, rel string) (string, []HeadField, *jsonx.Obj)

// EditNoteUnindexed is EditBody with a NoteEdit and without rebuilding the
// index after the write, for a note read from disk on every use (a work
// queue), so a hook's one write does not pay for a scan of the whole vault.
func EditNoteUnindexed(l *vault.Loader, relativePath string, p *schema.Profile, edit NoteEdit, extra ...any) *jsonx.Obj {
	return editBody(l, relativePath, p, edit, false, extra...)
}

func editBody(l *vault.Loader, relativePath string, p *schema.Profile, edit NoteEdit, reindex bool, extra ...any) *jsonx.Obj {
	v, full, rel, archive, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	var out *jsonx.Obj
	size := 0
	wrote, err := v.Update(full, func(text string) string {
		out, size = nil, 0
		cut := frontmatter.BodyOffset(text)
		if cut == 0 {
			out = jsonx.Fail("file has no frontmatter; register it with set_frontmatter first.")
			return text
		}
		head, old := text[:cut], text[cut:]
		before, _, _ := frontmatter.Split(text)
		newBody, fields, ferr := edit(before, old, rel)
		if ferr != nil {
			out = ferr
			return text
		}
		head = setHeadField(head, "last_reviewed", clock.TodayISO())
		for _, f := range fields {
			if f.Drop {
				head = dropHeadField(head, f.Key)
				continue
			}
			head = setHeadField(head, f.Key, frontmatter.Escape(f.Value))
		}
		newText := head + newBody
		fm, _, _ := frontmatter.Split(newText)
		errs := p.Check(fm, rel)
		if archive != "" {
			errs = p.CheckArchived(fm, archive)
		}
		if len(errs) > 0 {
			out = jsonx.Fail("frontmatter does not validate: " + strings.Join(errs, "; ") + "; fix it with set_frontmatter, then retry")
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
		if out.Failed() {
			return out
		}
		return out.Set("no_op", true).Set("wrote_bytes", 0)
	}
	if reindex {
		l.Index(true)
	}
	return out.Set("wrote_bytes", size).Set("next_step", nextStep)
}

// spliceSection rewrites the section heading names in body; heading ""
// names the preamble, the text before the first heading. A failure naming
// several sections carries their heading paths as candidates.
func spliceSection(body, heading, content string, appendMode bool, rel string) (string, *jsonx.Obj) {
	needle := pystr.Strip(heading)
	b := sections.NewBody(body)
	all := b.Headings()
	if needle == "" {
		return splicePreamble(body, b, all, content, appendMode), nil
	}
	if len(all) == 0 {
		return "", jsonx.Fail(rel + " has no headings; create the doc's sections with write_doc mode create")
	}
	// The index parses the body without its final newline; so does this, so
	// a section's span here is the one read_doc returned.
	parsed := strings.TrimSuffix(body, "\n")
	targets := sections.Targets(parsed, needle)
	switch len(targets) {
	case 0:
		texts := make([]string, len(all))
		for i, h := range all {
			texts[i] = h.Text
		}
		return "", jsonx.Fail(fmt.Sprintf("no heading %s in %s; pass a heading path or section slug from read_doc's outline, or one of %s exactly",
			pystr.Repr(needle), rel, pystr.ReprList(texts)))
	case 1:
	default:
		names := candidateNames(targets)
		return "", jsonx.Fail(fmt.Sprintf("heading %s appears %d times in %s; pass one of its heading paths instead: %s",
			pystr.Repr(needle), len(targets), rel, pystr.ReprList(names))).Set("candidates", jsonx.Strings(names))
	}
	t := targets[0]
	if t.Line >= 0 {
		if echo, ok := firstHeading(content); ok && echo.Level == t.Level && echo.Text == t.Text {
			return "", jsonx.Fail(fmt.Sprintf("body starts with the section's own heading %s; the heading line stays, so send the body without it",
				pystr.Repr(strings.Repeat("#", t.Level)+" "+t.Text)))
		}
	}
	lines := b.Lines
	end := t.End
	if end >= len(sections.Lines(parsed)) {
		end = len(lines)
	}
	start := t.Start
	if t.Line >= 0 {
		start = t.Line + 1
		if appendMode {
			end = min(end, b.NextHeading(t.Line))
		}
	}
	section := strings.Trim(content, "\n")
	if existing := strings.Trim(strings.Join(lines[start:end], "\n"), "\n"); appendMode && existing != "" {
		section = existing + "\n\n" + section
	}
	var sb strings.Builder
	for _, line := range lines[:start] {
		sb.WriteString(line + "\n")
	}
	if section != "" {
		if t.Line >= 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(section + "\n")
	}
	if end < len(lines) {
		sb.WriteString("\n" + strings.Join(lines[end:], "\n"))
	}
	return sb.String(), nil
}

// candidateNames names each target the way write_doc takes it back: its
// heading path, or its section slug where two share a path.
func candidateNames(targets []sections.Target) []string {
	count := map[string]int{}
	for _, t := range targets {
		count[t.Path]++
	}
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Path
		if count[t.Path] > 1 && t.Slug != "" {
			names[i] = t.Slug
		}
	}
	return names
}

// firstHeading is the heading on content's first non-blank line, if it is one.
func firstHeading(content string) (sections.Heading, bool) {
	lines := sections.Lines(content)
	for i, l := range lines {
		if pystr.Strip(l) == "" {
			continue
		}
		for _, h := range sections.Headings(content) {
			if h.Line == i {
				return h, true
			}
		}
		break
	}
	return sections.Heading{}, false
}

// splicePreamble rewrites the text before the first heading of body (all of
// it when there is none), keeping the blank lines that open the body.
func splicePreamble(body string, b *sections.Body, all []sections.Heading, content string, appendMode bool) string {
	lines := b.Lines
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

// setHeadField sets one scalar field to value, as written, in a frontmatter
// block, leaving every other byte alone; a block without the field gets it
// before the closing fence.
func setHeadField(head, key, value string) string {
	lines := strings.SplitAfter(head, "\n")
	line := key + ": " + value + "\n"
	fences := 0
	for i, l := range lines {
		if strings.HasPrefix(l, key+":") {
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

// dropHeadField removes one field from a frontmatter block, with the
// indented list items that follow it, leaving every other byte alone.
func dropHeadField(head, key string) string {
	lines := strings.SplitAfter(head, "\n")
	for i, l := range lines {
		if i == 0 || !strings.HasPrefix(l, key+":") {
			continue
		}
		end := i + 1
		for end < len(lines) && (strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t")) {
			end++
		}
		return strings.Join(append(lines[:i:i], lines[end:]...), "")
	}
	return head
}
