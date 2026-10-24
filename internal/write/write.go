// Package write holds the schema-aware frontmatter mutations. Every writer
// resolves through fsx.IndexedPath, renders through frontmatter.Serialize,
// writes atomically, and reports failures as {"ok": false, "error": ...}.
package write

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/internal/contracts"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// Today is the local date as YYYY-MM-DD.
func Today() string { return clock.TodayISO() }

const nextStep = "run any downstream vault metadata sync if your workflow uses one"

func fail(msg string) *jsonx.Obj { return fsx.Fail(msg) }

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

// UpdateLink replaces one exact Markdown link in an indexed, non-restricted doc.
func UpdateLink(l *vault.Loader, docID, label, expectedHref, replacementHref string) *jsonx.Obj {
	idx := l.Index(true)
	d, ok := idx.ByDocID[pystr.Strip(docID)]
	if !ok {
		return fail("unknown doc_id: " + pystr.Repr(docID))
	}
	if d.Sensitivity == gate.Restricted {
		return fail("restricted document bodies cannot be mutated")
	}
	if refused := readOnly(l, d.RelativePath); refused != nil {
		return refused
	}
	if pystr.Strip(label) == "" {
		return fail("link label required")
	}
	for _, pair := range [][2]string{{"expected_href", expectedHref}, {"replacement_href", replacementHref}} {
		u, err := url.Parse(pair[1])
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fail(pair[0] + " must be an absolute HTTP(S) URL")
		}
	}
	v, err := fsx.OpenVault(l.Config.VaultPath)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	defer v.Close()
	text, err := v.ReadText(d.Path)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	fm, _, _ := frontmatter.Split(text)
	if fm == nil {
		return fail("document has no frontmatter")
	}
	cut := frontmatter.BodyOffset(text)
	head, body := text[:cut], text[cut:]
	pattern := regexp.MustCompile(`\[` + regexp.QuoteMeta(label) + `\]\(` + regexp.QuoteMeta(expectedHref) + `(?:\s+"[^"]*")?\)`)
	found := pattern.FindAllStringIndex(body, -1)
	if len(found) != 1 {
		return fail(fmt.Sprintf("expected exactly one matching link; found %d", len(found)))
	}
	newBody := body[:found[0][0]] + "[" + label + "](" + replacementHref + ")" + body[found[0][1]:]
	if err := v.AtomicWrite(d.Path, head+newBody); err != nil {
		return fail("write failed: " + err.Error())
	}
	l.Index(true)
	receipt := contracts.Receipt{
		Operation:           "document.link.update",
		EntityType:          "document",
		EntityID:            d.DocID,
		ChangedFields:       []string{"body.link"},
		AfterFingerprint:    contracts.Fingerprint(jsonx.New("doc_id", d.DocID, "href", replacementHref)),
		AffectedProjections: []string{"vault_index"},
		Metadata:            jsonx.New("label", label),
	}.AsDict()
	return jsonx.New("ok", true, "doc_id", d.DocID, "relative_path", d.RelativePath, "label", label,
		"old_href", expectedHref, "new_href", replacementHref, "receipt", receipt)
}

func fieldErrors(p *schema.Profile, task bool, checks ...[2]string) []string {
	var errs []string
	for _, c := range checks {
		if e := p.FieldError(c[0], c[1], task); e != "" {
			errs = append(errs, e)
		}
	}
	return errs
}

// docIDTaken is the failure for a doc_id another doc already holds, nil when free.
func docIDTaken(idx *vault.Index, docID string) *jsonx.Obj {
	if dups, ok := idx.DuplicateDocIDs[docID]; ok {
		return fail(fmt.Sprintf("doc_id %s is already duplicated at %s", pystr.Repr(docID), pystr.ReprList(dups)))
	}
	if existing, ok := idx.ByDocID[docID]; ok {
		return fail(fmt.Sprintf("doc_id %s already exists at %s", pystr.Repr(docID), existing.RelativePath))
	}
	return nil
}

// newFrontmatter is the block a create writes: a task gets the task fields,
// anything else the doc fields. lastReviewed "" is today.
func newFrontmatter(docID, title, docType, system, environment, status, sensitivity, lastReviewed string,
	tags, relatedProjects, relatedAssets []string, hasTags bool) *jsonx.Obj {
	reviewed := lastReviewed
	if reviewed == "" {
		reviewed = Today()
	}
	if docType == "task" {
		taskTags := tags
		if !hasTags || len(tags) == 0 {
			taskTags = []string{"backlog"}
		}
		return jsonx.New(
			"doc_id", docID, "title", title, "doc_type", "task", "status", status,
			"related_projects", strList(relatedProjects),
			"effort", "S", "priority", "med", "created", reviewed,
			"tags", strList(taskTags),
		)
	}
	return jsonx.New(
		"doc_id", docID, "title", title, "doc_type", docType, "system", system,
		"environment", environment, "status", status, "sensitivity", sensitivity,
		"last_reviewed", reviewed,
		"related_projects", strList(relatedProjects),
		"related_assets", strList(relatedAssets),
		"tags", strList(tags),
	)
}

// AddFrontmatter prepends a new block to a file that has none.
func AddFrontmatter(l *vault.Loader, relativePath, docID, title, docType, system, environment, status, sensitivity string,
	tags, relatedProjects, relatedAssets []string, hasTags, hasProjects, hasAssets bool, lastReviewed string, p *schema.Profile) *jsonx.Obj {
	isTask := docType == "task"
	checks := [][2]string{{"doc_type", docType}, {"status", status}}
	if !isTask {
		checks = [][2]string{{"doc_type", docType}, {"environment", environment}, {"status", status}, {"sensitivity", sensitivity}}
	}
	errs := fieldErrors(p, isTask, append(checks, [2]string{"doc_id", docID})...)
	if len(errs) > 0 {
		return fail(strings.Join(errs, "; "))
	}
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	body, err := v.ReadText(full)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	if strings.HasPrefix(pystr.LStrip(body), "---") {
		return fail("file already starts with `---` (existing frontmatter); update it instead of adding a block.")
	}
	if taken := docIDTaken(l.Index(true), docID); taken != nil {
		return taken
	}
	payload := newFrontmatter(docID, title, docType, system, environment, status, sensitivity, lastReviewed,
		tags, relatedProjects, relatedAssets, hasTags)
	if isTask {
		if err := v.AtomicWrite(full, frontmatter.Serialize(payload)+body); err != nil {
			return fail("write failed: " + err.Error())
		}
		l.Index(true)
		return jsonx.New("ok", true, "relative_path", relativePath, "doc_id", docID)
	}
	text := frontmatter.Serialize(payload) + body
	if err := v.AtomicWrite(full, text); err != nil {
		return fail("write failed: " + err.Error())
	}
	l.Index(true)
	rel, _ := filepath.Rel(l.Config.VaultPath, full)
	if r, err := filepath.Rel(fsx.Resolve(l.Config.VaultPath), full); err == nil {
		rel = r
	}
	return jsonx.New("ok", true, "doc_id", docID, "relative_path", rel, "wrote_bytes", len(text), "next_step", nextStep)
}

// ListOp is a list field change: Set replaces; Add appends new values;
// Remove drops values. Nil means "not given".
type ListOp struct {
	Set, Add, Remove []string
	HasSet           bool
}

func (op ListOp) given() bool { return op.HasSet || op.Add != nil || op.Remove != nil }

func applyListOp(current []string, op ListOp) []string {
	if op.HasSet {
		return slices.Clone(op.Set)
	}
	out := slices.Clone(current)
	if len(op.Remove) > 0 {
		out = slices.DeleteFunc(out, func(v string) bool { return slices.Contains(op.Remove, v) })
	}
	for _, v := range op.Add {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// Update holds the optional fields of an update; nil pointers are unchanged.
type Update struct {
	// Register gives a block that has no doc_id its first one.
	Register                                                 *string
	TouchLastReviewed                                        bool
	LastReviewed                                             *string
	Title, DocType, System, Environment, Status, Sensitivity *string
	Tags, RelatedProjects, RelatedAssets                     ListOp
}

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

// UpdateFrontmatter changes fields in an existing block; doc_id is immutable.
func UpdateFrontmatter(l *vault.Loader, relativePath string, u Update, p *schema.Profile) *jsonx.Obj {
	var checks [][2]string
	for _, f := range []struct {
		key string
		val *string
	}{{"doc_type", u.DocType}, {"environment", u.Environment}, {"status", u.Status}, {"sensitivity", u.Sensitivity}} {
		if f.val != nil {
			checks = append(checks, [2]string{f.key, *f.val})
		}
	}
	if errs := fieldErrors(p, false, checks...); len(errs) > 0 {
		return fail(strings.Join(errs, "; "))
	}
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	root := fsx.Resolve(l.Config.VaultPath)
	rel, _ := filepath.Rel(root, full)
	text, err := v.ReadText(full)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	fm, body, _ := frontmatter.Split(text)
	if fm == nil {
		return fail("file has no frontmatter; add a block first.")
	}
	changed := map[string]bool{}
	if u.Register != nil {
		fm.Delete("doc_id")
		fm = jsonx.New("doc_id", *u.Register).Merge(fm)
		changed["doc_id"] = true
	}
	for _, f := range []struct {
		key string
		val *string
	}{{"title", u.Title}, {"doc_type", u.DocType}, {"system", u.System}, {"environment", u.Environment}, {"status", u.Status}, {"sensitivity", u.Sensitivity}} {
		if f.val == nil {
			continue
		}
		cur, ok := fm.Get(f.key)
		if s, isStr := cur.(string); !ok || !isStr || s != *f.val {
			fm.Set(f.key, *f.val)
			changed[f.key] = true
		}
	}
	var reviewed *string
	if u.TouchLastReviewed {
		t := Today()
		reviewed = &t
	} else if u.LastReviewed != nil {
		reviewed = u.LastReviewed
	}
	if reviewed != nil {
		cur, ok := fm.Get("last_reviewed")
		if s, isStr := cur.(string); !ok || !isStr || s != *reviewed {
			fm.Set("last_reviewed", *reviewed)
			changed["last_reviewed"] = true
		}
	}
	for _, f := range []struct {
		key string
		op  ListOp
	}{{"tags", u.Tags}, {"related_projects", u.RelatedProjects}, {"related_assets", u.RelatedAssets}} {
		if !f.op.given() {
			continue
		}
		cur, _ := fm.Get(f.key)
		current := valueList(cur)
		next := applyListOp(current, f.op)
		if !slices.Equal(next, current) {
			fm.Set(f.key, strList(next))
			changed[f.key] = true
		}
	}
	docID, _ := fm.Get("doc_id")
	if len(changed) == 0 {
		return jsonx.New("ok", true, "no_op", true, "doc_id", docID, "relative_path", rel,
			"message", "No fields differ — nothing written.")
	}
	if err := v.AtomicWrite(full, frontmatter.Serialize(fm)+body); err != nil {
		return fail("write failed: " + err.Error())
	}
	l.Index(true)
	keys := make([]string, 0, len(changed))
	for k := range changed {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return jsonx.New("ok", true, "doc_id", docID, "relative_path", rel, "changed_fields", keys, "next_step", nextStep)
}

// Set holds set_frontmatter's arguments. Nil pointers and nil lists are "not given".
type Set struct {
	DocID, Title, DocType, System, Environment, Status, Sensitivity *string
	Tags, RelatedProjects, RelatedAssets                            ListOp
	LastReviewed                                                    *string
	TouchLastReviewed                                               bool
}

func strOr(p *string, def string) string {
	if p == nil || *p == "" {
		return def
	}
	return *p
}

// SetFrontmatter creates the block when absent, else updates it.
func SetFrontmatter(l *vault.Loader, relativePath string, s Set, p *schema.Profile) *jsonx.Obj {
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	text, err := v.ReadText(full)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	existing, _, _ := frontmatter.Split(text)
	if existing == nil {
		var absent []string
		for _, f := range []struct {
			name string
			val  *string
		}{{"doc_id", s.DocID}, {"title", s.Title}, {"doc_type", s.DocType}, {"system", s.System}} {
			if f.val == nil || *f.val == "" {
				absent = append(absent, f.name)
			}
		}
		if len(absent) > 0 {
			return fail("no frontmatter yet; creating one needs " + strings.Join(absent, ", "))
		}
		if len(s.Tags.Remove) > 0 || len(s.RelatedProjects.Remove) > 0 || len(s.RelatedAssets.Remove) > 0 {
			return fail("no frontmatter yet; remove_* lists only apply to an update")
		}
		merged := func(op ListOp) ([]string, bool) {
			if !op.HasSet && op.Add == nil {
				return nil, false
			}
			return applyListOp(op.Set, ListOp{Add: op.Add}), true
		}
		tags, hasTags := merged(s.Tags)
		projects, hasProjects := merged(s.RelatedProjects)
		assets, hasAssets := merged(s.RelatedAssets)
		reviewed := ""
		if s.LastReviewed != nil {
			reviewed = *s.LastReviewed
		}
		return AddFrontmatter(l, relativePath, *s.DocID, *s.Title, *s.DocType, *s.System,
			strOr(s.Environment, "other"), strOr(s.Status, "active"), strOr(s.Sensitivity, "internal"),
			tags, projects, assets, hasTags, hasProjects, hasAssets, reviewed, p)
	}
	var register *string
	if s.DocID != nil {
		cur, _ := existing.Get("doc_id")
		if c, _ := cur.(string); c != "" {
			if c != *s.DocID {
				return fail(fmt.Sprintf("doc_id is immutable (file has %s, got %s)", pystr.Repr(c), pystr.Repr(*s.DocID)))
			}
		} else {
			if errs := fieldErrors(p, false, [2]string{"doc_id", *s.DocID}); len(errs) > 0 {
				return fail(strings.Join(errs, "; "))
			}
			if taken := docIDTaken(l.Index(false), *s.DocID); taken != nil {
				return taken
			}
			register = s.DocID
		}
	}
	return UpdateFrontmatter(l, relativePath, Update{
		Register:          register,
		TouchLastReviewed: s.TouchLastReviewed,
		LastReviewed:      s.LastReviewed,
		Title:             s.Title, DocType: s.DocType, System: s.System,
		Environment: s.Environment, Status: s.Status, Sensitivity: s.Sensitivity,
		Tags: s.Tags, RelatedProjects: s.RelatedProjects, RelatedAssets: s.RelatedAssets,
	}, p)
}

// TouchReviewed sets last_reviewed to today without rebuilding the index.
func TouchReviewed(l *vault.Loader, relativePath string) *jsonx.Obj {
	v, full, perr := indexedPath(l, relativePath)
	if perr != nil {
		return perr
	}
	defer v.Close()
	rel, _ := filepath.Rel(fsx.Resolve(l.Config.VaultPath), full)
	text, err := v.ReadText(full)
	if err != nil {
		return fail("write failed: " + err.Error())
	}
	fm, body, _ := frontmatter.Split(text)
	if fm == nil {
		return fail("file has no frontmatter; add a block first.")
	}
	reviewed := Today()
	docID, _ := fm.Get("doc_id")
	if cur, _ := fm.Get("last_reviewed"); cur == reviewed {
		return jsonx.New("ok", true, "no_op", true, "doc_id", docID, "relative_path", rel,
			"message", "No fields differ — nothing written.")
	}
	fm.Set("last_reviewed", reviewed)
	if err := v.AtomicWrite(full, frontmatter.Serialize(fm)+body); err != nil {
		return fail("write failed: " + err.Error())
	}
	return jsonx.New("ok", true, "doc_id", docID, "relative_path", rel, "changed_fields", []string{"last_reviewed"}, "next_step", nextStep)
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
		full := filepath.Join(root, d.RelativePath)
		text, err := v.ReadText(full)
		if err != nil {
			return fail(fmt.Sprintf("write failed for %s: %s", d.RelativePath, err))
		}
		fm, body, _ := frontmatter.Split(text)
		titleV, _ := fm.Get("title")
		if fm == nil || fm.Len() == 0 || !pystr.Truthy(titleV) {
			skipped++
			continue
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
			skipped++
			continue
		}
		aliases := []any{title}
		for _, a := range existing {
			if a != title {
				aliases = append(aliases, a)
			}
		}
		fm.Set("aliases", aliases)
		if err := v.AtomicWrite(full, frontmatter.Serialize(fm)+body); err != nil {
			return fail(fmt.Sprintf("write failed for %s: %s", d.RelativePath, err))
		}
		updated = append(updated, d.RelativePath)
	}
	return jsonx.New("ok", true, "updated", updated, "updated_count", len(updated), "skipped", skipped)
}
