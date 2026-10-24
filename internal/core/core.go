// Package core is the shared vault tool surface, transport-free: find,
// read_doc, set_frontmatter, write_doc, move_doc, task_board, task_write,
// log_entries and queue, plus the two resource renderers. Each
// call runs against one Vault, so search, the sensitivity gate and schema
// validation never cross vaults. The MCP adapter and the tests both drive
// these functions.
package core

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/query"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/search"
	"github.com/joeseverino/severino-vault-mcp/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/internal/tabular"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

// QuickIndexDocID is the navigation hub doc.
const QuickIndexDocID = "report-playbook-mcp-index"

// Vault is one governed vault: its config, profile, and index.
type Vault struct {
	Name    string
	Config  config.Config
	Profile *schema.Profile
	Loader  *vault.Loader
}

// NewVault builds a governed vault.
func NewVault(name string, cfg config.Config, p *schema.Profile) *Vault {
	return &Vault{Name: name, Config: cfg, Profile: p, Loader: vault.NewLoader(cfg)}
}

var unlockMessages = map[string]string{
	"not_requested": "Body withheld. To request release, rerun with include_restricted=True; " +
		"the local MCP will still ask for Touch ID through 1Password.",
	"unconfigured": "Restricted unlock is not configured: set restricted.unlock_ref (or " +
		config.UnlockRefVar + ") to the op:// reference of a dedicated 1Password item.",
	"service-account-refused": gate.ServiceAccountToken + " is set in the MCP server's environment, " +
		"and a service account would approve its own unlock. Start the server without it to unlock with Touch ID.",
	"withheld": "1Password did not approve the unlock. Check that the 1Password app is unlocked " +
		"with CLI integration on, then retry and approve the Touch ID prompt.",
	"approved": "Unlocked through 1Password for this request only.",
}

func wikiTargets(text string) []string {
	var out []string
	parts := strings.Split(text, "[[")
	for _, part := range parts[1:] {
		target, _, _ := strings.Cut(part, "]]")
		target, _, _ = strings.Cut(target, "|")
		target, _, _ = strings.Cut(target, "#")
		target = pystr.Strip(target)
		if target != "" {
			out = append(out, target)
		}
	}
	return out
}

func docForReference(idx *vault.Index, reference string) *vault.Doc {
	clean := strings.Trim(pystr.Strip(reference), "`")
	if clean == "" {
		return nil
	}
	if d, ok := idx.ByDocID[clean]; ok {
		return d
	}
	targets := wikiTargets(reference)
	if len(targets) == 0 {
		targets = []string{clean}
	}
	byTitle, byStem := map[string]*vault.Doc{}, map[string]*vault.Doc{}
	for _, d := range idx.Docs {
		byTitle[strings.ToLower(d.Title)] = d
		byStem[strings.ToLower(d.Stem())] = d
	}
	for _, t := range targets {
		lowered := strings.ToLower(t)
		if d, ok := byTitle[lowered]; ok {
			return d
		}
		if d, ok := byStem[lowered]; ok {
			return d
		}
	}
	return nil
}

type qiMatch struct {
	o      *jsonx.Obj
	score  int
	intent string
}

func quickIndexMatches(idx *vault.Index, q string, limit int) []*jsonx.Obj {
	qi, ok := idx.ByDocID[QuickIndexDocID]
	if !ok {
		return nil
	}
	qt := search.Tokenize(q)
	if len(qt) == 0 {
		return nil
	}
	var rows []qiMatch
	var headers []string
	for _, raw := range pystr.SplitLines(qi.Body) {
		line := pystr.Strip(raw)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			headers = nil
			continue
		}
		cells := tabular.SplitRow(line)
		if tabular.IsSeparator(cells) {
			continue
		}
		if headers == nil {
			headers = cells
			continue
		}
		if len(cells) != len(headers) {
			continue
		}
		row := map[string]string{}
		for i, h := range headers {
			row[h] = cells[i]
		}
		pick := func(keys ...string) string {
			for _, k := range keys {
				if v := row[k]; v != "" {
					return v
				}
			}
			return ""
		}
		intent := pick("Intent", "Symptom", "Topic")
		command := pick("Command", "First step", "Start Here")
		docRef := pick("Doc", "Then Read")
		score := 5*search.Intersect(qt, search.Tokenize(intent)) +
			3*search.Intersect(qt, search.Tokenize(command)) +
			2*search.Intersect(qt, search.Tokenize(docRef))
		if score == 0 {
			continue
		}
		target := docForReference(idx, docRef)
		if target == nil {
			target = docForReference(idx, command)
		}
		m := jsonx.New("score", score, "intent", intent, "command", command, "doc", docRef, "quick_index_doc_id", qi.DocID)
		if target != nil {
			m.Set("target_doc_id", target.DocID)
			m.Set("target_title", target.Title)
		}
		rows = append(rows, qiMatch{m, score, intent})
	}
	slices.SortStableFunc(rows, func(a, b qiMatch) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(b.intent, a.intent)
	})
	var out []*jsonx.Obj
	for i, r := range rows {
		if i == limit {
			break
		}
		out = append(out, r.o)
	}
	return out
}

func findRelevance(v *Vault, q string, limit int, keep query.Keep) *jsonx.Obj {
	resp := query.FindSections(v.Loader, q, limit, keep)
	if keep != nil {
		// The quick index points across the whole vault, so it has nothing
		// to add to a filtered search.
		return resp
	}
	hitsV, _ := resp.Get("hits")
	hits := hitsV.([]*jsonx.Obj)
	topDocID := ""
	if len(hits) > 0 {
		topDocID = hits[0].Str("doc_id")
	}
	if matches := quickIndexMatches(v.Loader.Index(false), q, 3); len(matches) > 0 {
		resp.Set("quick_index_matches", matches)
		if topDocID == "" || matches[0].Str("target_doc_id") == topDocID {
			resp.Set("recommended", jsonx.New("source", "vault://"+v.Name+"/quick-index").Merge(matches[0]))
		}
	}
	return resp
}

// FindModes are find's modes; the first is the default.
var FindModes = []string{"relevance", "text"}

// FindArgs are find's options. System and Project, when set, narrow either
// mode to docs whose `system` field, or one of whose `related_projects`
// entries, contains them, case-insensitively.
type FindArgs struct {
	By            string
	Limit         int
	ContextLines  int
	CaseSensitive bool
	System        string
	Project       string
	View
}

// HitFields are the fields a find hit can carry.
var HitFields = append(append(typed("score", tInt), metaFields...), append(typed(
	"heading", tStr, "section", tStr, "heading_path", tStr, "section_summary", tStr, "section_score", tInt,
	"match_count", tInt, "snippet", tStr),
	list("snippets", typed("line_number", tInt, "kind", tStr, "text", tStr)), Field{Name: "advisory", Type: tStr})...)

// metaFields are a doc's frontmatter as hits and read_doc carry it.
var metaFields = typed("doc_id", tStr, "title", tStr, "doc_type", tStr, "system", tStr, "environment", tStr,
	"status", tStr, "sensitivity", tStr, "obsidian_path", tStr, "tags", tList, "last_reviewed", tOptStr)

// leanHit is a hit by default: enough to pick one and read its section.
// heading is the last part of heading_path; section stays because
// heading_path neither names the preamble nor tells repeated headings apart.
var leanHit = mustMask(HitFields, "doc_id", "title", "heading_path", "section", "section_summary", "score")

// findEnvelope is what a masked find keeps around its hits.
var findEnvelope = []string{"ok", "error", "mode", "filters", "fallback", "match_count", "words", "recommended", "hits", "next_steps"}

// Keep is the doc filter System and Project describe; nil when neither is set.
func (a FindArgs) Keep() query.Keep {
	system := strings.ToLower(pystr.Strip(a.System))
	project := strings.ToLower(pystr.Strip(a.Project))
	if system == "" && project == "" {
		return nil
	}
	return func(d *vault.Doc) bool {
		if system != "" && !strings.Contains(strings.ToLower(d.System), system) {
			return false
		}
		return project == "" || slices.ContainsFunc(d.RelatedProjects, func(p string) bool {
			return strings.Contains(strings.ToLower(p), project)
		})
	}
}

// filters echoes the filters a search applied.
func (a FindArgs) filters() *jsonx.Obj {
	o := jsonx.New()
	if s := pystr.Strip(a.System); s != "" {
		o.Set("system", s)
	}
	if p := pystr.Strip(a.Project); p != "" {
		o.Set("project", p)
	}
	return o
}

// Find searches one vault in the requested mode.
func Find(v *Vault, q string, a FindArgs) *jsonx.Obj {
	m, err := a.selection(HitFields, leanHit)
	if err != nil {
		return failNext(err.Error(), withoutFields("find", "query", q))
	}
	keep := a.Keep()
	var resp *jsonx.Obj
	switch a.By {
	case "relevance":
		resp = findRelevance(v, q, a.Limit, keep)
		hits, _ := resp.Get("hits")
		resp.Set("match_count", len(hits.([]*jsonx.Obj)))
	case "text":
		resp = query.SearchBody(v.Loader, q, a.Limit, a.ContextLines, a.CaseSensitive, keep)
		if resp.Has("error") {
			resp = steer(jsonx.New("ok", false).Merge(resp), Call("find", "query", q, "by", "relevance")+" to match titles and sections instead")
		} else {
			// Rename in place, keeping the Python key order.
			renamed := jsonx.New()
			for _, k := range resp.Keys() {
				val, _ := resp.Get(k)
				switch k {
				case "hits_by_doc":
					continue
				case "doc_count":
					continue
				}
				renamed.Set(k, val)
			}
			hits, _ := resp.Get("hits_by_doc")
			count, _ := resp.Get("doc_count")
			renamed.Set("hits", hits)
			renamed.Set("match_count", count)
			resp = renamed
			if count == 0 {
				excluded, _ := resp.Get("excluded")
				resp = jsonx.New("fallback", "relevance").Merge(findRelevance(v, q, a.Limit, keep))
				hits, _ := resp.Get("hits")
				resp.Set("match_count", len(hits.([]*jsonx.Obj)))
				resp.Set("excluded", excluded)
			}
		}
	default:
		return failNext(fmt.Sprintf("unknown mode %s; one of %s", pystr.Repr(a.By), strings.Join(FindModes, ", ")),
			Call("find", "query", q, "by", FindModes[0]))
	}
	out := jsonx.New("vault", v.Name, "mode", a.By)
	if f := a.filters(); len(f.Keys()) > 0 {
		out.Set("filters", f)
	}
	out.Merge(resp)
	if n, _ := out.Get("match_count"); n == 0 {
		steer(out, v.emptyFind(q, a)...)
	}
	if m == nil {
		return out
	}
	out = keepOnly(out, findEnvelope...)
	if rec, ok := out.Get("recommended"); ok {
		// The quick index's row: what to run, and the doc it points at.
		out.Set("recommended", keepOnly(rec.(*jsonx.Obj), "intent", "command", "target_doc_id"))
	}
	// A sensitive hit's advisory travels with it whatever the mask.
	cutEach(out, "hits", m.with("advisory"))
	return out
}

func lookupDoc(v *Vault, identifier string) (*vault.Doc, *jsonx.Obj) {
	idx := v.Loader.Index(false)
	if d, ok := idx.ByDocID[identifier]; ok {
		return d, nil
	}
	alias := vault.NormalizeAlias(identifier)
	if target, ok := idx.Aliases[alias]; ok {
		return idx.ByDocID[target], jsonx.New("input", identifier, "matched_alias", alias, "target_doc_id", target)
	}
	if alias == "" {
		return nil, nil
	}
	for _, c := range idx.Docs {
		names := []string{c.DocID, c.Title, c.RelativePath, c.Stem()}
		for _, n := range names {
			if vault.NormalizeAlias(n) == alias {
				return c, jsonx.New("input", identifier, "matched_alias", c.Title, "target_doc_id", c.DocID, "source", "title_or_path")
			}
		}
	}
	return nil, nil
}

func notFound(v *Vault, identifier string) *jsonx.Obj {
	if dups, ok := v.Loader.Index(false).DuplicateDocIDs[identifier]; ok {
		return steer(jsonx.New("doc_id", identifier, "found", false, "ambiguous", true,
			"error", "duplicate doc_id "+pystr.Repr(identifier), "paths", slices.Clone(dups)),
			Call("task_write", "action", "add", "title", "Resolve duplicate doc_id "+identifier)+
				" and tell the user; neither doc can be read until one file's doc_id changes")
	}
	return steer(jsonx.New("doc_id", identifier, "found", false,
		"alias_hint", "For recurring local phrases, add an entry to the vault aliases "+
			"file configured by `SVMC_ALIASES_PATH` or `[aliases].path`."),
		Call("find", "query", identifier)+" to get a doc_id, then retry read_doc with it")
}

func withheld(base *jsonx.Obj, result, message string) *jsonx.Obj {
	base.Set("body_released", false)
	base.Set("advisory", gate.Advisory(gate.Parse(base.Str("sensitivity")), false))
	if result != "" {
		base.Set("unlock", jsonx.New("allowed", false, "result", result, "message", message))
	}
	return steer(base, unlockSteps(base.Str("doc_id"), result)...)
}

// unlock asks 1Password, and so Joe's Touch ID, to release one restricted
// doc. The service-account check comes first: with a token in the
// environment op would answer without a human.
func unlock(v *Vault, _, _ string) (bool, string, string) {
	ref := v.Config.RestrictedUnlockRef
	if !strings.HasPrefix(ref, "op://") {
		return false, "unconfigured", unlockMessages["unconfigured"]
	}
	if gate.ServiceAccountPresent() {
		return false, "service-account-refused", unlockMessages["service-account-refused"]
	}
	if err := gate.Approve(ref); err != nil {
		return false, "withheld", unlockMessages["withheld"] + " (" + err.Error() + ")"
	}
	return true, "approved", unlockMessages["approved"]
}

// narrow replaces the body with the sections refs name, or, when one names
// no section, with the doc's section list and no text at all.
func narrow(base *jsonx.Obj, d *vault.Doc, refs []string) *jsonx.Obj {
	base.Delete("body")
	var found []*jsonx.Obj
	var missing []string
	for _, ref := range refs {
		sec := sections.Resolve(d.Sections, ref)
		if sec == nil {
			missing = append(missing, ref)
			continue
		}
		found = append(found, jsonx.New("section", sec.Slug, "heading_path", sec.HeadingPath, "body", sec.Body))
	}
	if len(missing) > 0 {
		base.Set("body_released", false)
		base.Set("section_error", fmt.Sprintf("no section %s in %s", strings.Join(reprs(missing), ", "), d.DocID))
		available := query.AvailableSections(d)
		base.Set("available_sections", available)
		if len(available) == 0 {
			return steer(base, Call("read_doc", "doc_id", d.DocID)+" without sections; the doc has none")
		}
		return steer(base, Call("read_doc", "doc_id", d.DocID, "sections", []string{available[0].Str("section")})+
			" or another section from available_sections")
	}
	base.Set("sections", found)
	return base
}

func reprs(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, pystr.Repr(s))
	}
	return out
}

// ReadArgs are read_doc's options. Sections, when set, returns those
// sections' text in place of the whole body.
type ReadArgs struct {
	Sections          []string
	IncludeRestricted bool
	View
}

// DocFields are the fields read_doc can return. outline is the doc's
// sections, slug and heading path, without their text.
var DocFields = append(append(typed("vault", tStr), metaFields...),
	Field{Name: "body", Type: tStr}, list("outline", typed("section", tStr, "heading_path", tStr)))

// leanDoc is read_doc by default: the body, and the frontmatter an agent acts
// on (whether the doc is current and how old its review is).
var leanDoc = mustMask(DocFields, "doc_id", "title", "status", "last_reviewed", "body")

// gateKeys always survive a mask: the sensitivity gate's answer, how the doc
// was resolved, and the requested sections.
var gateKeys = []string{"found", "resolved_from_alias", "body_released", "advisory", "unlock", "override_used",
	"section_error", "available_sections", "sections", "next_steps"}

// ReadDoc reads a doc's body behind the sensitivity gate.
func ReadDoc(v *Vault, docID string, a ReadArgs) *jsonx.Obj {
	m, err := a.selection(DocFields, leanDoc)
	if err != nil {
		return failNext(err.Error(), withoutFields("read_doc", "doc_id", docID))
	}
	if a.Sections != nil && len(a.Sections) == 0 {
		return failNext("sections: give at least one section slug or heading path, or omit sections for the whole body",
			Call("read_doc", "doc_id", docID, "fields", []string{"outline"})+" to list the sections")
	}
	o := jsonx.New("vault", v.Name).Merge(readDoc(v, docID, a))
	if m == nil || !o.Bool("found") {
		return o
	}
	if _, wantsOutline := m["outline"]; !wantsOutline {
		o.Delete("outline")
	}
	return cut(o, m.with(gateKeys...)).(*jsonx.Obj)
}

func readDoc(v *Vault, docID string, a ReadArgs) *jsonx.Obj {
	d, alias := lookupDoc(v, docID)
	if d == nil {
		return notFound(v, docID)
	}
	base := jsonx.New("doc_id", d.DocID, "found", true).Merge(query.Hit(d))
	if alias != nil {
		base.Set("resolved_from_alias", alias)
	}
	if d.Sensitivity == gate.Restricted {
		if !a.IncludeRestricted {
			return withheld(base, "not_requested", unlockMessages["not_requested"])
		}
		allowed, result, msg := unlock(v, d.DocID, d.Title)
		gate.AuditUnlock(v.Config.RestrictedAuditLog, d.DocID, result)
		base.Set("unlock", jsonx.New("allowed", allowed, "result", result, "message", msg))
		if !allowed {
			return withheld(base, result, msg)
		}
		base.Set("override_used", true)
		return release(base, d, a.Sections, gate.Advisory(d.Sensitivity, true))
	}
	advisory := ""
	if d.Sensitivity == gate.Sensitive {
		advisory = gate.Advisory(d.Sensitivity, false)
	}
	return release(base, d, a.Sections, advisory)
}

// release adds what the gate let through: the body (or the requested
// sections) and the outline. A withheld doc gets neither, since headings can
// say as much as the text under them.
func release(base *jsonx.Obj, d *vault.Doc, refs []string, advisory string) *jsonx.Obj {
	base.Set("body", d.Body)
	base.Set("outline", query.AvailableSections(d))
	base.Set("body_released", true)
	if advisory != "" {
		base.Set("advisory", advisory)
	}
	if refs != nil {
		return narrow(base, d, refs)
	}
	return base
}

// RenderDoc is a doc's markdown resource view, gated without unlock.
func RenderDoc(v *Vault, docID string) string {
	idx := v.Loader.Index(false)
	d, ok := idx.ByDocID[docID]
	if !ok {
		if dups, ok := idx.DuplicateDocIDs[docID]; ok {
			var lines []string
			for _, p := range dups {
				lines = append(lines, "- `"+p+"`")
			}
			return "# Duplicate Vault Doc ID\n\n`" + docID + "` is used by multiple indexed documents:\n\n" + strings.Join(lines, "\n") + "\n"
		}
		return "# Vault Doc Not Found\n\nNo indexed doc has doc_id `" + docID + "`. Use `find`."
	}
	if !gate.Releasable(d.Sensitivity) {
		return fmt.Sprintf("# %s\n\n%s\n\n- doc_id: `%s`\n- path: `%s`\n- system: `%s`\n- sensitivity: `%s`\n",
			d.Title, gate.Advisory(d.Sensitivity, false), d.DocID, d.RelativePath, d.System, d.Sensitivity)
	}
	return d.Body
}

// SetFrontmatter creates or updates a doc's frontmatter.
func SetFrontmatter(v *Vault, relativePath string, c write.Changes) *jsonx.Obj {
	if v.Profile == nil {
		return failNext("vault "+pystr.Repr(v.Name)+" has no schema profile", noProfile)
	}
	return guide(write.SetFrontmatter(v.Loader, relativePath, c, v.Profile), v.setSteps(relativePath, c.Str("doc_id")))
}

// WriteDocModes are write_doc's modes; the first is the default.
var WriteDocModes = []string{"create", "replace_section", "append_section"}

// WriteDocArgs are write_doc's arguments. DocID is the new doc's id on
// create and names the doc a section mode edits; New carries the rest of the
// frontmatter a create writes. Heading nil is not given; "" is the preamble,
// the text before the first heading.
type WriteDocArgs struct {
	Mode              string
	RelativePath      string
	DocID             string
	Heading           *string
	Body              string
	IncludeRestricted bool
	New               write.Changes
}

// WriteDoc creates a registered doc, or edits one section of its body, with
// the frontmatter validated against v's profile either way.
func WriteDoc(v *Vault, a WriteDocArgs) *jsonx.Obj {
	if v.Profile == nil {
		return failNext("vault "+pystr.Repr(v.Name)+" has no schema profile", noProfile)
	}
	heading := ""
	if a.Heading != nil {
		heading = *a.Heading
	}
	switch a.Mode {
	case "create":
		if a.RelativePath == "" {
			name := a.New.Str("title")
			if name == "" {
				name = "Doc Title"
			}
			return failNext("mode 'create' requires relative_path", v.writable(name+".md"))
		}
		if a.Heading != nil {
			return failNext("heading only applies to replace_section and append_section",
				Call("write_doc", "mode", "create", "relative_path", a.RelativePath)+" without heading",
				Call("write_doc", "mode", "replace_section", "relative_path", a.RelativePath, "heading", heading)+" to edit an existing doc")
		}
		return guide(write.CreateDoc(v.Loader, a.RelativePath, a.Body, a.New.Set("doc_id", a.DocID), v.Profile), v.createSteps(a.RelativePath, a.DocID))
	case "replace_section", "append_section":
	default:
		return failNext(fmt.Sprintf("unknown mode %s; one of %s", pystr.Repr(a.Mode), strings.Join(WriteDocModes, ", ")),
			Call("write_doc", "mode", WriteDocModes[0])+" or another listed mode")
	}
	target := a.DocID
	if target == "" {
		target = a.RelativePath
	}
	if !a.New.Empty() {
		return failNext("frontmatter fields only apply to mode 'create'; change them with set_frontmatter",
			Call("set_frontmatter", "relative_path", pathOf(v, a.DocID, a.RelativePath))+" with the frontmatter fields",
			Call("write_doc", "mode", a.Mode, "doc_id", target)+" with only heading and body")
	}
	if a.Heading == nil {
		return failNext(fmt.Sprintf("mode %s requires heading (\"\" for the text before the first heading)", pystr.Repr(a.Mode)),
			Call("read_doc", "doc_id", target, "fields", []string{"outline"})+" to list its headings")
	}
	d, err, steps := registeredDoc(v, a.DocID, a.RelativePath)
	if err != "" {
		return failNext(err, steps...)
	}
	if d.Sensitivity == gate.Restricted {
		retry := Call("write_doc", "mode", a.Mode, "doc_id", d.DocID, "heading", heading, "include_restricted", true)
		if !a.IncludeRestricted {
			return failNext(d.DocID+" is restricted; rerun with include_restricted=True, which also needs a Touch ID approval through 1Password",
				retry+" with the same body, from the main session, only if the user asked for this edit")
		}
		allowed, result, msg := unlock(v, d.DocID, d.Title)
		gate.AuditUnlock(v.Config.RestrictedAuditLog, d.DocID, result)
		if !allowed {
			return failNext(d.DocID+" is restricted and the unlock was refused: "+msg, retry+" with the same body, once the user has fixed the unlock")
		}
	}
	return guide(write.EditSection(v.Loader, d.RelativePath, heading, a.Body, a.Mode == "append_section", v.Profile), v.sectionSteps(d, a.Mode))
}

// noProfile is the step for a vault without a schema profile.
var noProfile = Call("find", "query", "...", "vault", "labs") + " and write in a vault with a schema profile"

// pathOf is the vault-relative path a doc_id or path names, for a step.
func pathOf(v *Vault, docID, relativePath string) string {
	if d, ok := v.Loader.Index(false).ByDocID[docID]; ok {
		return d.RelativePath
	}
	if relativePath != "" {
		return relativePath
	}
	return "..."
}

// registeredDoc finds the indexed doc an edit names by doc_id or path. A
// failure comes with its next steps.
func registeredDoc(v *Vault, docID, relativePath string) (*vault.Doc, string, []string) {
	idx := v.Loader.Index(true)
	switch {
	case docID != "" && relativePath != "":
		return nil, "pass doc_id or relative_path, not both", []string{Call("write_doc", "doc_id", docID) + " without relative_path"}
	case docID != "":
		if dups, ok := idx.DuplicateDocIDs[docID]; ok {
			return nil, fmt.Sprintf("duplicate doc_id %s at %s; resolve the duplicate first", pystr.Repr(docID), pystr.ReprList(dups)),
				[]string{Call("task_write", "action", "add", "title", "Resolve duplicate doc_id "+docID) + " and tell the user"}
		}
		if d, ok := idx.ByDocID[docID]; ok {
			return d, "", nil
		}
		return nil, "unknown doc_id " + pystr.Repr(docID) + "; call `find` to get one",
			[]string{Call("find", "query", docID) + " to get a doc_id, then retry with it"}
	case relativePath != "":
		clean := filepath.Clean(strings.TrimLeft(relativePath, "/"))
		for _, d := range idx.Docs {
			if d.RelativePath == clean {
				return d, "", nil
			}
		}
		msg := "no registered doc at " + relativePath + "; create it with mode 'create', or register it with set_frontmatter"
		if v.outsideWritable(relativePath) {
			return nil, msg, []string{v.writable(relativePath)}
		}
		return nil, msg, []string{Call("write_doc", "mode", "create", "relative_path", relativePath) + " to create it",
			Call("set_frontmatter", "relative_path", relativePath) + " with doc_id, title, doc_type and system to register an existing file"}
	}
	return nil, "pass doc_id or relative_path", []string{Call("find", "query", "...") + " to get the doc's doc_id"}
}

// CheckDoc validates one file's frontmatter against v's profile with the rules
// set_frontmatter writes by. path is absolute or vault-relative. ok is false
// when path is not one of v's docs (outside the vault or its indexed dirs, not
// markdown, or absent), so a caller can ignore it.
func CheckDoc(v *Vault, path string) (rel string, problems []string, ok bool) {
	if v.Profile == nil {
		return "", nil, false
	}
	root, err := fsx.OpenVault(v.Config.VaultPath)
	if err != nil {
		return "", nil, false
	}
	defer root.Close()
	rel, err = root.Rel(path)
	if err != nil || !vault.IsDocPath(rel, v.Config.IndexedDirs) {
		return "", nil, false
	}
	text, err := root.ReadText(rel)
	if err != nil {
		return "", nil, false
	}
	fm, _, _ := frontmatter.Split(text)
	if fm == nil {
		return rel, []string{"missing YAML frontmatter"}, true
	}
	return rel, append(v.Profile.Check(fm, rel), v.Profile.RefProblems(fm, v.Loader.Index(false).Has)...), true
}

// BoardArgs are task_board's options beyond the filter. Limit caps the tasks
// returned, after ranking; count still says how many matched.
type BoardArgs struct {
	Limit int
	View
}

// TaskFields are the fields a task on the board carries.
var TaskFields = typed("doc_id", tStr, "slug", tStr, "title", tStr, "status", tStr, "project", tStr,
	"related_projects", tList, "effort", tStr, "priority", tStr, "created", tStr, "closed", tStr, "tags", tList,
	"relative_path", tStr, "age_days", tInt, "stale", tBool)

var leanTask = mustMask(TaskFields, "doc_id", "title", "status", "project", "priority")

// TaskBoard is a vault's board plus the projects a task can go in. Masked,
// it keeps the matching count, the tasks and the recently shipped ones (both
// cut to the mask), and the projects as bare slugs.
func TaskBoard(v *Vault, f tasks.Filter, a BoardArgs) *jsonx.Obj {
	m, err := a.selection(TaskFields, leanTask)
	if err != nil {
		return failNext(err.Error(), withoutFields("task_board"))
	}
	if a.Limit < 0 {
		return failNext("limit: give a positive number, or 0 for no limit", Call("task_board", "limit", 25))
	}
	board := tasks.List(v.Loader, f)
	if all, _ := board.Get("tasks"); a.Limit > 0 && len(all.([]*jsonx.Obj)) > a.Limit {
		board.Set("tasks", all.([]*jsonx.Obj)[:a.Limit])
	}
	projects, _ := tasks.Projects(v.Loader).Get("projects")
	if m == nil {
		return board.Set("projects", projects)
	}
	slugs := []string{}
	for _, p := range projects.([]*jsonx.Obj) {
		slugs = append(slugs, p.Str("slug"))
	}
	board = keepOnly(board, "ok", "count", "total", "tasks", "shipped").Set("projects", slugs)
	cutEach(board, "tasks", m)
	cutEach(board, "shipped", m)
	return board
}

// TaskWriteArgs are task_write's arguments.
type TaskWriteArgs struct {
	Action          string
	Title           string
	Project         string
	RelatedProjects []string
	Effort          string
	Priority        string
	Tags            []string
	Problem         string
	Fix             string
	Principle       string
	Source          string
	DocID           string
	Status          string
	NotePath        string
}

// TaskWrite adds, moves, promotes or deletes a task.
func TaskWrite(v *Vault, a TaskWriteArgs) *jsonx.Obj {
	missing := func(names ...string) *jsonx.Obj {
		values := map[string]string{"title": a.Title, "doc_id": a.DocID, "status": a.Status, "note_path": a.NotePath}
		var absent []string
		for _, n := range names {
			if values[n] == "" {
				absent = append(absent, n)
			}
		}
		if len(absent) > 0 {
			args := []any{"action", a.Action}
			for _, n := range names {
				args = append(args, n, "...")
			}
			return failNext(fmt.Sprintf("action %s requires %s", pystr.Repr(a.Action), strings.Join(absent, ", ")), Call("task_write", args...))
		}
		return nil
	}
	guided := func(o *jsonx.Obj) *jsonx.Obj { return guide(o, taskSteps(a)) }
	switch a.Action {
	case "add":
		if err := missing("title"); err != nil {
			return err
		}
		return guided(tasks.Add(v.Loader, tasks.New{
			Title: a.Title, Project: a.Project, RelatedProjects: a.RelatedProjects,
			Effort: a.Effort, Priority: a.Priority, Tags: a.Tags,
		}, tasks.Sections{Problem: a.Problem, Fix: a.Fix, Principle: a.Principle, Source: a.Source}))
	case "status":
		if err := missing("doc_id", "status"); err != nil {
			return err
		}
		return guided(tasks.SetStatus(v.Loader, a.DocID, a.Status))
	case "promote":
		if err := missing("note_path", "title"); err != nil {
			return err
		}
		return guided(tasks.Promote(v.Loader, a.NotePath, tasks.New{Title: a.Title, Project: a.Project, Effort: a.Effort, Priority: a.Priority}))
	case "delete":
		if err := missing("doc_id"); err != nil {
			return err
		}
		return guided(tasks.Delete(v.Loader, a.DocID))
	}
	return failNext(fmt.Sprintf("unknown action %s; one of %s", pystr.Repr(a.Action), strings.Join(TaskActions, ", ")),
		Call("task_write", "action", TaskActions[0], "title", "...")+" or another listed action")
}

// TaskActions are task_write's actions.
var TaskActions = []string{"add", "status", "promote", "delete"}

// MaxTaskItems caps one batch, so a runaway caller can't rewrite the board in
// one call.
const MaxTaskItems = 50

// TaskWrites applies items in order, each through TaskWrite, and goes on past
// a failure: earlier items are not rolled back. ok is true only when every
// item applied; failed lists the indexes of the ones that didn't.
func TaskWrites(v *Vault, items []TaskWriteArgs) *jsonx.Obj {
	if len(items) == 0 {
		return failNext("items: give at least one item, or call with a single action instead", Call("task_write", "action", "...")+" for one change")
	}
	if len(items) > MaxTaskItems {
		return failNext(fmt.Sprintf("items: at most %d per call; split the batch", MaxTaskItems),
			Call("task_write", "items", []string{"..."})+fmt.Sprintf(" with at most %d items, once per batch", MaxTaskItems))
	}
	results := make([]*jsonx.Obj, 0, len(items))
	failed := []int{}
	for i, item := range items {
		r := TaskWrite(v, item)
		if !r.Bool("ok") {
			failed = append(failed, i)
		}
		// A receipt is for a projection replaying one write; a batch's
		// caller needs only what happened to each item.
		r.Delete("receipt")
		results = append(results, jsonx.New("index", i, "action", item.Action).Merge(r))
	}
	return jsonx.New("ok", len(failed) == 0, "applied", len(items)-len(failed), "failed", failed, "results", results)
}

// RelStem is used by tests to name a doc by its path stem.
func RelStem(rel string) string {
	base := filepath.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
