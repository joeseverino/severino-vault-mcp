// Package write holds the schema-aware frontmatter mutations. Every writer
// resolves through fsx.IndexedPath, renders through frontmatter.Serialize,
// writes atomically, and reports failures as {"ok": false, "error": ...}.
package write

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// Today is the local date as YYYY-MM-DD.
func Today() string { return clock.TodayISO() }

const nextStep = "run any downstream vault metadata sync if your workflow uses one"

func fail(msg string) *jsonx.Obj { return fsx.Fail(msg) }

func writeFailed(err error) *jsonx.Obj { return fail("write failed: " + err.Error()) }

func failed(o *jsonx.Obj) bool {
	ok, has := o.Get("ok")
	return has && ok == false
}

// readOnly refuses a write into a read-only folder, or returns nil.
func readOnly(l *vault.Loader, relativePath string) *jsonx.Obj {
	dir := l.Config.ReadOnlyDir(strings.TrimLeft(relativePath, "/"))
	if dir == "" {
		return nil
	}
	return fail(fmt.Sprintf("%s is in %s, and %s are read-only: find and read_doc serve them, "+
		"but their notes keep their own frontmatter (inbox captures, site content), so set_frontmatter and write_doc leave them alone",
		relativePath, pystr.Repr(dir), pystr.ReprList(l.Config.ReadOnlyDirs)))
}

// indexedPath is fsx.IndexedPath behind the read-only refusal.
func indexedPath(l *vault.Loader, relativePath string) (*fsx.Vault, string, *jsonx.Obj) {
	if refused := readOnly(l, relativePath); refused != nil {
		return nil, "", refused
	}
	return fsx.IndexedPath(l.Config, relativePath)
}

func strList(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

// createNeeds are the fields a new block has no default for.
var createNeeds = []string{"doc_id", "title", "doc_type", "system"}

// absentForCreate lists the createNeeds c leaves empty.
func absentForCreate(c Changes) []string {
	var absent []string
	for _, name := range createNeeds {
		if pystr.Strip(c.Str(name)) == "" {
			absent = append(absent, name)
		}
	}
	return absent
}

// newFrontmatter is the block a create writes from c: a task gets the task
// fields, anything else the doc fields, each defaulted when c leaves it out;
// every other field c gives follows.
func newFrontmatter(c Changes) *jsonx.Obj {
	def := func(name, fallback string) string {
		if s := c.Str(name); s != "" {
			return s
		}
		return fallback
	}
	list := func(name string) []any { return strList(c.Lists[name].created()) }
	reviewed := def("last_reviewed", Today())
	if slices.Contains(c.Touch, "last_reviewed") {
		reviewed = Today()
	}
	var fm *jsonx.Obj
	task := c.Str("doc_type") == "task"
	if task {
		tags := c.Lists["tags"].created()
		if len(tags) == 0 {
			tags = []string{"backlog"}
		}
		fm = jsonx.New(
			"doc_id", c.Str("doc_id"), "title", c.Str("title"), "doc_type", "task", "status", def("status", "active"),
			"related_projects", list("related_projects"),
			"effort", def("effort", "S"), "priority", def("priority", "med"), "created", def("created", reviewed),
			"tags", strList(tags),
		)
	} else {
		fm = jsonx.New(
			"doc_id", c.Str("doc_id"), "title", c.Str("title"), "doc_type", c.Str("doc_type"), "system", c.Str("system"),
			"environment", def("environment", "other"), "status", def("status", "active"), "sensitivity", def("sensitivity", "internal"),
			"last_reviewed", reviewed,
			"related_projects", list("related_projects"),
			"related_assets", list("related_assets"),
			"tags", list("tags"),
		)
	}
	for _, name := range c.names() {
		if fm.Has(name) || (task && name == "last_reviewed") {
			continue
		}
		switch {
		case slices.Contains(c.Touch, name):
			fm.Set(name, Today())
		case c.Lists[name].given():
			fm.Set(name, list(name))
		default:
			fm.Set(name, c.Values[name])
		}
	}
	return fm
}

// held is the problem with a unique field's values: another doc that may
// carry the field already holds one, compared case-insensitively. "" when
// every value is free. self is the doc being written.
func held(idx *vault.Index, f schema.Field, values []string, self string) string {
	for _, d := range idx.Docs {
		if d.RelativePath == self || d.Frontmatter == nil || !f.InScope(d.RelativePath) {
			continue
		}
		cur, _ := d.Frontmatter.Get(f.Name)
		if cur == nil {
			continue
		}
		theirs := vault.CoerceList(cur)
		for _, v := range values {
			if !slices.ContainsFunc(theirs, func(t string) bool { return strings.EqualFold(t, v) }) {
				continue
			}
			name := f.Name
			if f.IsList() {
				name += " entry"
			}
			return fmt.Sprintf("%s %s already exists at %s; a value belongs to one doc, so change it there first",
				name, pystr.Repr(v), d.RelativePath)
		}
	}
	return ""
}

// valueList reads a frontmatter value as a list of strings.
func valueList(v any) []string {
	switch x := v.(type) {
	case nil:
		return []string{}
	case []any:
		out := []string{}
		for _, item := range x {
			out = append(out, pystr.Str(item))
		}
		return out
	case string:
		if x == "" {
			return []string{}
		}
	}
	if !pystr.Truthy(v) {
		return []string{}
	}
	return []string{pystr.Str(v)}
}

// SetFrontmatter applies c to the doc at relativePath, validated against p:
// a file without frontmatter gets a new block, an existing block is changed
// in place. Every field p declares can be set; an immutable one only while
// unset.
func SetFrontmatter(l *vault.Loader, relativePath string, c Changes, p *schema.Profile) *jsonx.Obj {
	if msg := c.undeclared(p); msg != "" {
		return fail(msg)
	}
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	rel, _ := filepath.Rel(fsx.Resolve(l.Config.VaultPath), full)
	var idx *vault.Index
	if slices.ContainsFunc(c.names(), func(name string) bool { f, _ := p.Field(name); return f.Unique || f.DocRef }) {
		idx = l.Index(true)
	}
	var out *jsonx.Obj
	wrote, err := v.Update(full, func(text string) string {
		next, o := applyChanges(text, rel, c, p, idx)
		out = o
		return next
	})
	if err != nil {
		return writeFailed(err)
	}
	if wrote {
		l.Index(true)
	}
	return out
}

// applyChanges is c applied to a note's text: the new text and the result to
// report, or the text unchanged and a failure or no-op result.
func applyChanges(text, rel string, c Changes, p *schema.Profile, idx *vault.Index) (string, *jsonx.Obj) {
	before, body, _ := frontmatter.Split(text)
	if before == nil {
		return createBlock(text, rel, c, p, idx)
	}
	fm := before.Clone()
	var changed []string
	set := func(name string, value any) {
		if cur, has := fm.Get(name); has && cur != nil && pystr.Str(cur) == pystr.Str(value) {
			if _, isList := cur.([]any); !isList {
				return
			}
		}
		fm.Set(name, value)
		changed = append(changed, name)
	}
	for _, name := range c.names() {
		f, _ := p.Field(name)
		cur, _ := fm.Get(name)
		switch {
		case slices.Contains(c.Touch, name):
			set(name, Today())
		case c.Lists[name].given():
			current := valueList(cur)
			if next := applyListOp(current, c.Lists[name]); !slices.Equal(next, current) {
				fm.Set(name, strList(next))
				changed = append(changed, name)
			}
		default:
			value := c.Values[name]
			if f.Immutable && pystr.Truthy(cur) && pystr.Str(cur) != pystr.Str(value) {
				return text, fail(fmt.Sprintf("%s is immutable (file has %s, got %s)", name, pystr.Repr(pystr.Str(cur)), pystr.Repr(pystr.Str(value))))
			}
			set(name, value)
		}
	}
	docID, _ := fm.Get("doc_id")
	if len(changed) == 0 {
		return text, jsonx.New("ok", true, "no_op", true, "doc_id", docID, "relative_path", rel,
			"message", "No fields differ — nothing written.")
	}
	task := schema.IsTask(fm)
	var errs []string
	refs := jsonx.New("doc_id", docID)
	for _, name := range changed {
		f, _ := p.Field(name)
		if !f.InScope(rel) {
			errs = append(errs, f.ScopeError(rel))
			continue
		}
		v, _ := fm.Get(name)
		errs = append(errs, p.Problems(name, v, task)...)
		if f.DocRef {
			refs.Set(name, v)
		}
		if f.Unique && idx != nil {
			old, _ := before.Get(name)
			fresh := slices.DeleteFunc(valueList(v), func(s string) bool { return slices.Contains(valueList(old), s) })
			if e := held(idx, f, fresh, rel); e != "" {
				errs = append(errs, e)
			}
		}
	}
	if idx != nil {
		errs = append(errs, p.RefProblems(refs, idx.Has)...)
	}
	if len(errs) > 0 {
		return text, fail(strings.Join(errs, "; "))
	}
	slices.Sort(changed)
	return frontmatter.Serialize(fm) + body, jsonx.New("ok", true, "doc_id", docID, "relative_path", rel,
		"changed_fields", changed, "next_step", nextStep)
}

// createBlock prepends the block c builds to a file that has none.
func createBlock(text, rel string, c Changes, p *schema.Profile, idx *vault.Index) (string, *jsonx.Obj) {
	if strings.HasPrefix(pystr.LStrip(text), "---") {
		return text, fail("file already starts with `---` (existing frontmatter); update it instead of adding a block.")
	}
	if absent := absentForCreate(c); len(absent) > 0 {
		return text, fail("no frontmatter yet; creating one needs " + strings.Join(absent, ", "))
	}
	for _, op := range c.Lists {
		if len(op.Remove) > 0 {
			return text, fail("no frontmatter yet; remove_* lists only apply to an update")
		}
	}
	fm := newFrontmatter(c)
	if errs := checkNew(fm, rel, p, idx); len(errs) > 0 {
		return text, fail(strings.Join(errs, "; "))
	}
	out := frontmatter.Serialize(fm) + text
	return out, jsonx.New("ok", true, "doc_id", c.Str("doc_id"), "relative_path", rel, "wrote_bytes", len(out), "next_step", nextStep)
}

// checkNew is every problem with a new block at rel: the profile's rules,
// each unique field's value held by another doc, and each doc reference that
// names no doc.
func checkNew(fm *jsonx.Obj, rel string, p *schema.Profile, idx *vault.Index) []string {
	errs := p.Check(fm, rel)
	if idx != nil {
		errs = append(errs, p.RefProblems(fm, idx.Has)...)
	}
	for _, f := range p.Settable() {
		v, _ := fm.Get(f.Name)
		if !f.Unique || v == nil || idx == nil {
			continue
		}
		if e := held(idx, f, valueList(v), rel); e != "" {
			errs = append(errs, e)
		}
	}
	return errs
}

// TouchReviewed sets last_reviewed to today without rebuilding the index.
func TouchReviewed(l *vault.Loader, relativePath string) *jsonx.Obj {
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	rel, _ := filepath.Rel(fsx.Resolve(l.Config.VaultPath), full)
	var out *jsonx.Obj
	_, err := v.Update(full, func(text string) string {
		fm, body, _ := frontmatter.Split(text)
		if fm == nil {
			out = fail("file has no frontmatter; add a block first.")
			return text
		}
		reviewed := Today()
		docID, _ := fm.Get("doc_id")
		if cur, _ := fm.Get("last_reviewed"); cur == reviewed {
			out = jsonx.New("ok", true, "no_op", true, "doc_id", docID, "relative_path", rel,
				"message", "No fields differ — nothing written.")
			return text
		}
		fm.Set("last_reviewed", reviewed)
		out = jsonx.New("ok", true, "doc_id", docID, "relative_path", rel, "changed_fields", []string{"last_reviewed"}, "next_step", nextStep)
		return frontmatter.Serialize(fm) + body
	})
	if err != nil {
		return writeFailed(err)
	}
	return out
}

// BackfillAliases gives every folder note (<folder>/index.md) an Obsidian
// alias equal to its title. Idempotent.
func BackfillAliases(l *vault.Loader) *jsonx.Obj {
	idx := l.Index(true)
	root := fsx.Resolve(l.Config.VaultPath)
	v, err := fsx.OpenVault(l.Config.VaultPath)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	defer v.Close()
	updated := []string{}
	skipped := 0
	for _, d := range idx.Docs {
		if !strings.HasSuffix(d.RelativePath, "/index.md") || d.ReadOnlyDir != "" {
			continue
		}
		wrote, err := v.Update(filepath.Join(root, d.RelativePath), withTitleAlias)
		if err != nil {
			return fail(fmt.Sprintf("write failed for %s: %s", d.RelativePath, err))
		}
		if !wrote {
			skipped++
			continue
		}
		updated = append(updated, d.RelativePath)
	}
	return jsonx.New("ok", true, "updated", updated, "updated_count", len(updated), "skipped", skipped)
}

// withTitleAlias puts a folder note's title first in its aliases, or returns
// the text unchanged when it has no title or already carries the alias.
func withTitleAlias(text string) string {
	fm, body, _ := frontmatter.Split(text)
	titleV, _ := fm.Get("title")
	if fm == nil || fm.Len() == 0 || !pystr.Truthy(titleV) {
		return text
	}
	title := pystr.Str(titleV)
	existingV, _ := fm.Get("aliases")
	var existing []any
	switch x := existingV.(type) {
	case []any:
		existing = x
	case nil:
	default:
		if pystr.Truthy(x) {
			existing = []any{x}
		}
	}
	if slices.ContainsFunc(existing, func(a any) bool { return a == title }) {
		return text
	}
	aliases := []any{title}
	for _, a := range existing {
		if a != title {
			aliases = append(aliases, a)
		}
	}
	fm.Set("aliases", aliases)
	return frontmatter.Serialize(fm) + body
}
