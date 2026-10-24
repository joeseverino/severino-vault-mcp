// Package core is the shared vault tool surface, transport-free: find,
// read_doc, set_frontmatter, write_doc, move_doc, move_folder, log_entries and
// queue, plus the two resource renderers. Each
// call runs against one Vault, so search, the sensitivity gate and schema
// validation never cross vaults. The MCP adapter and the tests both drive
// these functions.
package core

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/gate"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/query"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/search"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/sections"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/tabular"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/vault"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
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

func findRelevance(v *Vault, q string, limit int, keep query.Keep, filtered bool) *jsonx.Obj {
	resp := query.FindSections(v.Loader, q, limit, keep)
	if filtered {
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
// entries, contains them, case-insensitively. DocType narrows to one
// doc_type. Work queue notes are left out unless DocType names their type:
// their items belong to the queue tool, and a search for prose should not
// land on them. Archived docs are left out too unless IncludeArchived: they
// are history, found when asked for.
type FindArgs struct {
	By              string
	Limit           int
	ContextLines    int
	CaseSensitive   bool
	System          string
	Project         string
	DocType         string
	IncludeArchived bool
	View
}

// HitFields are the fields a find hit can carry.
var HitFields = append(append(typed("score", tInt), metaFields...), append(typed(
	"heading", tStr, "section", tStr, "heading_path", tStr, "section_summary", tStr, "section_score", tInt,
	"match_count", tInt, "missing_words", tList, "snippet", tStr),
	list("snippets", typed("line_number", tInt, "kind", tStr, "text", tStr)), Field{Name: "advisory", Type: tStr})...)

// metaFields are a doc's frontmatter as hits and read_doc carry it.
var metaFields = typed("doc_id", tStr, "title", tStr, "doc_type", tStr, "system", tStr, "environment", tStr,
	"status", tStr, "sensitivity", tStr, "obsidian_path", tStr, "tags", tList, "last_reviewed", tOptStr)

// leanHit is a hit by default: enough to pick one and read its section.
// heading is the last part of heading_path; section stays because
// heading_path neither names the preamble nor tells repeated headings apart.
// missing_words, on a text hit holding only some of the query's words, says
// what the hit does not answer.
var leanHit = mustMask(HitFields, "doc_id", "title", "heading_path", "section", "section_summary", "score", "missing_words")

// findEnvelope is what a masked find keeps around its hits.
var findEnvelope = []string{"ok", "error", "mode", "filters", "fallback", "match_count", "words", "ranking", "exact_match", "recommended", "hits", "truncated", "next_steps"}

// Keep is the doc filter System and Project describe; nil when neither is set.
func (a FindArgs) Keep() query.Keep {
	system := strings.ToLower(pystr.Strip(a.System))
	project := strings.ToLower(pystr.Strip(a.Project))
	docType := pystr.Strip(a.DocType)
	return func(d *vault.Doc) bool {
		if d.Archived && !a.IncludeArchived {
			return false
		}
		if docType != "" && d.DocType != docType || docType == "" && d.DocType == schema.QueueDocType {
			return false
		}
		if system != "" && !containsFold(d.System, system) {
			return false
		}
		return project == "" || slices.ContainsFunc(d.RelatedProjects, func(p string) bool {
			return containsFold(p, project)
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
	if dt := pystr.Strip(a.DocType); dt != "" {
		o.Set("doc_type", dt)
	}
	return o
}

// Find searches one vault in the requested mode.
func Find(v *Vault, q string, a FindArgs) *jsonx.Obj {
	if f := v.Unreadable(); f != nil {
		return f
	}
	m, err := a.selection(HitFields, leanHit)
	if err != nil {
		return failNext(err.Error(), fieldSteps(q, a, err)...)
	}
	keep, filtered := a.Keep(), len(a.filters().Keys()) > 0
	var resp *jsonx.Obj
	switch a.By {
	case "relevance":
		resp = findRelevance(v, q, a.Limit, keep, filtered)
		hits, _ := resp.Get("hits")
		resp.Set("match_count", len(hits.([]*jsonx.Obj)))
	case "text":
		resp = query.SearchBody(v.Loader, q, a.Limit, a.ContextLines, a.CaseSensitive, keep)
		if resp.Failed() {
			resp = steer(resp, Call("find", "query", q, "by", "relevance")+" to match titles and sections instead")
		} else {
			// hits_by_doc and doc_count become hits and match_count, set after
			// the other keys, which keep their order.
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
				resp = jsonx.New("fallback", "relevance").Merge(findRelevance(v, q, a.Limit, keep, filtered))
				hits, _ := resp.Get("hits")
				resp.Set("match_count", len(hits.([]*jsonx.Obj)))
				resp.Set("excluded", excluded)
			}
		}
	default:
		return failNext(fmt.Sprintf("unknown mode %q; one of %s", a.By, strings.Join(FindModes, ", ")),
			Call("find", "query", q, "by", FindModes[0]))
	}
	out := jsonx.New("vault", v.Name, "mode", a.By)
	if f := a.filters(); len(f.Keys()) > 0 {
		out.Set("filters", f)
	}
	out.Merge(resp)
	if !out.Failed() {
		rankExactFirst(v, out, q, a, keep)
	}
	if n, _ := out.Get("match_count"); n == 0 {
		steer(out, v.emptyFind(q, a)...)
	}
	if m != nil {
		out = keepOnly(out, findEnvelope...)
		if rec, ok := out.Get("recommended"); ok {
			// The quick index's row: what to run, and the doc it points at.
			out.Set("recommended", keepOnly(rec.(*jsonx.Obj), "intent", "command", "target_doc_id"))
		}
		// A sensitive hit's advisory travels with it whatever the mask.
		cutEach(out, "hits", m.with("advisory"))
	}
	budget(out, q, a)
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
			"error", "duplicate doc_id "+strconv.Quote(identifier), "paths", slices.Clone(dups)),
			Call("queue", "action", "add", "project", vault.BacklogProject, "kind", "follow-up", "text", "Resolve duplicate doc_id "+identifier)+
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

// narrow replaces the body with the sections refs name, resolved by
// sections.Address, or, when one names no section or several, with the
// doc's outline and no text at all. A heading's section is its whole block,
// the span write_doc edits for the same ref (sections.Targets); a part's
// slug reads that part.
func narrow(base *jsonx.Obj, d *vault.Doc, refs []string, available []*jsonx.Obj) *jsonx.Obj {
	base.Delete("body")
	var found []*jsonx.Obj
	var missing, problems, candidates []string
	for _, ref := range refs {
		targets := sections.Address(d.Body, ref)
		switch len(targets) {
		case 0:
			missing = append(missing, ref)
		case 1:
			t := targets[0]
			found = append(found, jsonx.New("section", t.Name(), "heading_path", t.Path, "body", t.Span(d.Body)))
		default:
			names := make([]string, len(targets))
			for i, t := range targets {
				names[i] = t.Name()
			}
			problems = append(problems, fmt.Sprintf("%q names %d sections: %s", ref, len(targets), pystr.Quoted(names)))
			candidates = append(candidates, names...)
		}
	}
	if len(missing) == 0 && len(candidates) == 0 {
		base.Set("sections", found)
		return base
	}
	base.Set("body_released", false)
	if len(missing) > 0 {
		problems = append([]string{fmt.Sprintf("no section %s in %s", pystr.Quoted(missing), d.DocID)}, problems...)
	}
	base.Set("section_error", strings.Join(problems, "; "))
	if len(candidates) > 0 {
		base.Set("candidates", jsonx.Strings(candidates))
		return steer(base, Call("read_doc", "doc_id", d.DocID, "sections", []string{candidates[0]})+
			" or another name from candidates")
	}
	base.Set("available_sections", available)
	if len(available) == 0 {
		return steer(base, Call("read_doc", "doc_id", d.DocID)+" without sections; the doc has none")
	}
	return steer(base, Call("read_doc", "doc_id", d.DocID, "sections", []string{available[0].Str("section")})+
		" or another section from available_sections, by its slug, heading path or 1-based position")
}

// outline is the doc's sections in order, each with the size in characters
// of what reading it by its slug returns, so a caller can pick the part it
// needs before paying for it.
func outline(d *vault.Doc) []*jsonx.Obj {
	out := []*jsonx.Obj{}
	for _, e := range sections.Outline(d.Body) {
		out = append(out, jsonx.New("section", e.Section.Slug, "heading_path", e.Section.HeadingPath,
			"chars", utf8.RuneCountInString(e.Target.Span(d.Body))))
	}
	return out
}

// BoundedBodyChars is the largest body a read_doc call with no sections and
// no fields returns whole: about 5,000 tokens, a short runbook or note read
// in one call. Past it the answer is the outline and the lead, a tenth of
// that or less, and the caller picks sections or asks for fields=["body"].
// Below it the outline round trip would cost more than the body it saves.
const BoundedBodyChars = 20_000

// leadChars caps the lead a bounded read returns: the text before the first
// section heading, cut at a line, enough to say what the doc is for.
const leadChars = 2_000

// bound replaces a long body with its lead and outline, and steers the
// caller to a section or to the whole body, which fields=["body"] returns.
func bound(base *jsonx.Obj, d *vault.Doc) *jsonx.Obj {
	if utf8.RuneCountInString(d.Body) <= BoundedBodyChars {
		return base
	}
	base.Delete("body")
	base.Set("truncated", true)
	leadText := lead(d)
	base.Set("lead", leadText)
	condensed, omitted := boundedOutline(d, BoundedAnswerChars-utf8.RuneCountInString(leadText))
	base.Set("outline", condensed)
	pick := ""
	for _, e := range condensed {
		if e.Str("heading_path") != "" {
			pick = e.Str("section")
			break
		}
	}
	var steps []string
	if omitted > 0 {
		steps = append(steps, Call("read_doc", "doc_id", d.DocID, "fields", []string{"outline"})+
			fmt.Sprintf(" for the full outline: %d more sections are not listed here", omitted))
	}
	for _, e := range condensed {
		if e.Has("parts") {
			steps = append(steps, Call("read_doc", "doc_id", d.DocID, "sections", []string{e.Str("first_part")})+
				fmt.Sprintf(" for one part of a split heading: %s through %s, or a 1-based position from first_position to last_position", e.Str("first_part"), e.Str("last_part")))
			break
		}
	}
	if pick != "" {
		steps = append(steps, Call("read_doc", "doc_id", d.DocID, "sections", []string{pick})+
			" for one part of the outline, by slug, heading path or 1-based position")
	}
	steps = append(steps, Call("read_doc", "doc_id", d.DocID, "fields", []string{"body"})+
		fmt.Sprintf(" for the whole body, %d characters", utf8.RuneCountInString(d.Body)))
	return steer(base, steps...)
}

// BoundedAnswerChars is the most a bounded read's lead and outline take
// together, measured as compact JSON: a doc with hundreds of sections
// answers in about as much as a short runbook.
const BoundedAnswerChars = 8_000

// boundedOutline is the outline a bounded read returns, within budget
// characters. A run of consecutive parts of one heading is one entry: the
// section is the first part's slug, with parts, first_part, last_part,
// first_position, last_position (1-based, as read_doc's sections accepts)
// and the run's total chars. Entries past the budget are left out and
// counted.
func boundedOutline(d *vault.Doc, budget int) (entries []*jsonx.Obj, omitted int) {
	all := sections.Outline(d.Body)
	used := 0
	for i := 0; i < len(all); {
		e := all[i]
		var o *jsonx.Obj
		next := i + 1
		chars := utf8.RuneCountInString(e.Target.Span(d.Body))
		if e.Section.Part > 0 {
			base := strings.TrimSuffix(e.Section.HeadingPath, fmt.Sprintf(" (part %d)", e.Section.Part))
			for next < len(all) && all[next].Section.Part > 0 && strings.TrimSuffix(all[next].Section.HeadingPath, fmt.Sprintf(" (part %d)", all[next].Section.Part)) == base {
				chars += utf8.RuneCountInString(all[next].Target.Span(d.Body))
				next++
			}
			if next-i > 1 {
				o = jsonx.New("section", e.Section.Slug, "heading_path", base, "parts", next-i,
					"first_part", e.Section.Slug, "last_part", all[next-1].Section.Slug,
					"first_position", i+1, "last_position", next, "chars", chars)
			}
		}
		if o == nil {
			o = jsonx.New("section", e.Section.Slug, "heading_path", e.Section.HeadingPath, "chars", chars)
		}
		used += len(jsonx.Compact(o)) + 1
		if used > budget {
			omitted = len(all) - i
			break
		}
		entries = append(entries, o)
		i = next
	}
	return entries, omitted
}

// lead is the text before the doc's first section heading, up to leadChars
// and cut at a line.
func lead(d *vault.Doc) string {
	if len(d.Sections) == 0 || d.Sections[0].Level != 0 {
		return ""
	}
	text := d.Sections[0].Body
	if utf8.RuneCountInString(text) <= leadChars {
		return text
	}
	cut := pystr.Prefix(text, leadChars)
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		cut = cut[:i]
	}
	return cut
}

// ReadArgs are read_doc's options. Sections, when set, returns those
// sections' text in place of the whole body.
type ReadArgs struct {
	Sections          []string
	IncludeRestricted bool
	View
}

// DocFields are the fields read_doc can return. outline is the doc's
// sections, slug, heading path and size, without their text.
var DocFields = append(append(typed("vault", tStr), metaFields...),
	Field{Name: "body", Type: tStr}, list("outline", typed("section", tStr, "heading_path", tStr, "chars", tInt)))

// leanDoc is read_doc by default: the body, and the frontmatter an agent acts
// on (whether the doc is current and how old its review is).
var leanDoc = mustMask(DocFields, "doc_id", "title", "status", "last_reviewed", "body")

// gateKeys always survive a mask: the sensitivity gate's answer, how the doc
// was resolved, and the requested sections.
var gateKeys = []string{"found", "resolved_from_alias", "body_released", "advisory", "unlock", "override_used",
	"truncated", "lead", "section_error", "candidates", "available_sections", "sections", "next_steps"}

// ReadDoc reads a doc's body behind the sensitivity gate.
func ReadDoc(v *Vault, docID string, a ReadArgs) *jsonx.Obj {
	if f := v.Unreadable(); f != nil {
		return f
	}
	m, err := a.selection(DocFields, leanDoc)
	if err != nil {
		return failNext(err.Error(), withoutFields("read_doc", "doc_id", docID))
	}
	if a.Sections != nil && len(a.Sections) == 0 {
		return failNext("sections: give at least one section slug or heading path, or omit sections for the whole body",
			Call("read_doc", "doc_id", docID, "fields", []string{"outline"})+" to list the sections")
	}
	// Only the lean default is bounded: fields that name body, and the
	// CLI's full view, ask for the whole of it.
	bounded := a.Fields == nil && !a.Full
	o := jsonx.New("vault", v.Name).Merge(readDoc(v, docID, a, bounded))
	if m == nil || !o.Bool("found") {
		return o
	}
	keep := m.with(gateKeys...)
	if o.Bool("truncated") {
		keep = keep.with("outline")
	} else if _, wantsOutline := m["outline"]; !wantsOutline {
		o.Delete("outline")
	}
	return cut(o, keep).(*jsonx.Obj)
}

func readDoc(v *Vault, docID string, a ReadArgs, bounded bool) *jsonx.Obj {
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
		return release(base, d, a.Sections, gate.Advisory(d.Sensitivity, true), bounded)
	}
	advisory := ""
	if d.Sensitivity == gate.Sensitive {
		advisory = gate.Advisory(d.Sensitivity, false)
	}
	return release(base, d, a.Sections, advisory, bounded)
}

// release adds what the gate let through: the body (or the requested
// sections, or, bounded, a long body's lead) and the outline. A withheld doc
// gets neither, since headings can say as much as the text under them.
func release(base *jsonx.Obj, d *vault.Doc, refs []string, advisory string, bounded bool) *jsonx.Obj {
	entries := outline(d)
	base.Set("body", d.Body)
	base.Set("outline", entries)
	base.Set("body_released", true)
	if advisory != "" {
		base.Set("advisory", advisory)
	}
	if refs != nil {
		return narrow(base, d, refs, entries)
	}
	if bounded {
		return bound(base, d)
	}
	return base
}

// RenderDoc is a doc's markdown resource view, gated without unlock.
func RenderDoc(v *Vault, docID string) string {
	if f := v.Unreadable(); f != nil {
		return "# Vault Unreadable\n\n" + f.Str("error") + "\n"
	}
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
	if f := v.Unreadable(); f != nil {
		return f
	}
	if v.Profile == nil {
		return failNext("vault "+strconv.Quote(v.Name)+" has no schema profile", noProfile)
	}
	return guide(write.SetFrontmatter(v.Loader, relativePath, c, v.Profile), v.setSteps(relativePath, c.Str("doc_id")))
}

// WriteDocModes are write_doc's modes; the first is the default.
var WriteDocModes = []string{"create", "replace_section", "append_section", "delete_section", "replace_text"}

// WriteDocArgs are write_doc's arguments. DocID is the new doc's id on
// create and names the doc a section mode edits; New carries the rest of the
// frontmatter a create writes. Heading nil is not given; "" is the preamble,
// the text before the first heading. OldText and NewText are replace_text's;
// NewText nil is not given, "" removes OldText.
type WriteDocArgs struct {
	Mode              string
	RelativePath      string
	DocID             string
	Heading           *string
	Body              string
	OldText           string
	NewText           *string
	IncludeRestricted bool
	New               write.Changes
}

// WriteDoc creates a registered doc, edits one section of its body, or
// replaces one passage of text in it, with the frontmatter validated against
// v's profile either way.
func WriteDoc(v *Vault, a WriteDocArgs) *jsonx.Obj {
	if f := v.Unreadable(); f != nil {
		return f
	}
	if v.Profile == nil {
		return failNext("vault "+strconv.Quote(v.Name)+" has no schema profile", noProfile)
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
			return failNext(`mode "create" requires relative_path`, v.writable(name+".md"))
		}
		if a.OldText != "" || a.NewText != nil {
			return failNext(`old_text and new_text only apply to mode "replace_text"`,
				Call("write_doc", "mode", "create", "relative_path", a.RelativePath)+" without them")
		}
		if a.Heading != nil {
			return failNext("heading only applies to the section modes",
				Call("write_doc", "mode", "create", "relative_path", a.RelativePath)+" without heading",
				Call("write_doc", "mode", "replace_section", "relative_path", a.RelativePath, "heading", heading)+" to edit an existing doc")
		}
		return guide(write.CreateDoc(v.Loader, a.RelativePath, a.Body, a.New.Set("doc_id", a.DocID), v.Profile), v.createSteps(a.RelativePath, a.DocID))
	case "replace_section", "append_section", "delete_section", "replace_text":
	default:
		return failNext(fmt.Sprintf("unknown mode %q; one of %s", a.Mode, strings.Join(WriteDocModes, ", ")),
			Call("write_doc", "mode", WriteDocModes[0])+" or another listed mode")
	}
	target := a.DocID
	if target == "" {
		target = a.RelativePath
	}
	if !a.New.Empty() {
		return failNext(`frontmatter fields only apply to mode "create"; change them with set_frontmatter`,
			Call("set_frontmatter", "relative_path", pathOf(v, a.DocID, a.RelativePath))+" with the frontmatter fields",
			Call("write_doc", "mode", a.Mode, "doc_id", target)+" with only heading and body")
	}
	if a.Mode != "replace_text" && (a.OldText != "" || a.NewText != nil) {
		return failNext(`old_text and new_text only apply to mode "replace_text"`,
			Call("write_doc", "mode", "replace_text", "doc_id", target, "old_text", a.OldText, "new_text", "...")+" to replace one passage",
			Call("write_doc", "mode", a.Mode, "doc_id", target)+" with only heading and body")
	}
	if a.Mode == "replace_text" {
		if f := textArgs(a, target); f != nil {
			return f
		}
	} else if a.Heading == nil {
		return failNext(fmt.Sprintf("mode %q requires heading (\"\" for the text before the first heading)", a.Mode),
			Call("read_doc", "doc_id", target, "fields", []string{"outline"})+" to list its headings")
	}
	if a.Mode == "delete_section" && a.Body != "" {
		return failNext(`mode "delete_section" takes no body`,
			Call("write_doc", "mode", "delete_section", "doc_id", target, "heading", heading)+" without body")
	}
	d, err, steps := registeredDoc(v, a.DocID, a.RelativePath)
	if err != "" {
		return failNext(err, steps...)
	}
	if d.Sensitivity == gate.Restricted {
		retryArgs := []any{"mode", a.Mode, "doc_id", d.DocID}
		if a.Heading != nil {
			retryArgs = append(retryArgs, "heading", heading)
		}
		retry := Call("write_doc", append(retryArgs, "include_restricted", true)...)
		same := " with the same body"
		if a.Mode == "replace_text" {
			same = " with the same old_text and new_text"
		}
		if !a.IncludeRestricted {
			return failNext(d.DocID+" is restricted; rerun with include_restricted=True, which also needs a Touch ID approval through 1Password",
				retry+same+", from the main session, only if the user asked for this edit")
		}
		allowed, result, msg := unlock(v, d.DocID, d.Title)
		gate.AuditUnlock(v.Config.RestrictedAuditLog, d.DocID, result)
		if !allowed {
			return failNext(d.DocID+" is restricted and the unlock was refused: "+msg, retry+same+", once the user has fixed the unlock")
		}
	}
	var out *jsonx.Obj
	if a.Mode == "replace_text" {
		return replaceTextSteps(v, d, a, write.ReplaceText(v.Loader, d.RelativePath, a.Heading, a.OldText, *a.NewText, v.Profile))
	}
	if a.Mode == "delete_section" {
		out = write.DeleteSection(v.Loader, d.RelativePath, heading, v.Profile)
	} else {
		out = write.EditSection(v.Loader, d.RelativePath, heading, a.Body, a.Mode == "append_section", v.Profile)
	}
	if c, ok := out.Get("candidates"); ok {
		names := vault.CoerceList(c)
		steps := make([]string, len(names))
		for i, n := range names {
			steps[i] = Call("write_doc", "mode", a.Mode, "doc_id", d.DocID, "heading", n) + " with the same body"
		}
		return steer(out, steps...)
	}
	if s, ok := out.Get("subsections"); ok {
		steps := []string{Call("read_doc", "doc_id", d.DocID, "sections", []string{heading}) +
			" for the whole block, then " + Call("write_doc", "mode", a.Mode, "doc_id", d.DocID, "heading", heading) + " with a body that keeps each subsection heading"}
		for _, n := range vault.CoerceList(s) {
			steps = append(steps, Call("write_doc", "mode", a.Mode, "doc_id", d.DocID, "heading", n)+" to replace only that subsection")
		}
		return steer(out, steps...)
	}
	return guide(out, v.sectionSteps(d, a.Mode))
}

// textArgs checks replace_text's arguments before the doc is looked up.
func textArgs(a WriteDocArgs, target string) *jsonx.Obj {
	retry := func(args ...any) string {
		return Call("write_doc", append([]any{"mode", "replace_text", "doc_id", target}, args...)...)
	}
	switch {
	case a.Body != "":
		return failNext(`mode "replace_text" takes old_text and new_text, not body`,
			retry("old_text", "...", "new_text", "...")+" without body",
			Call("write_doc", "mode", "replace_section", "doc_id", target, "heading", "...")+" to replace a whole section with body")
	case a.OldText == "":
		return failNext(`mode "replace_text" requires old_text: the exact text to replace, copied from read_doc`,
			Call("read_doc", "doc_id", target)+" to copy the passage")
	case a.NewText == nil:
		return failNext(`mode "replace_text" requires new_text ("" removes old_text)`,
			retry("old_text", a.OldText, "new_text", "..."))
	case *a.NewText == a.OldText:
		return failNext("new_text is the same as old_text; nothing to change",
			retry("old_text", a.OldText, "new_text", "...")+" with the text it should become")
	}
	return nil
}

// replaceTextSteps gives a replace_text result the calls that recover from
// its failures: a narrower heading for a passage that occurs more than once,
// the doc's text for one that does not occur.
func replaceTextSteps(v *Vault, d *vault.Doc, a WriteDocArgs, out *jsonx.Obj) *jsonx.Obj {
	retry := func(args ...any) string {
		return Call("write_doc", append([]any{"mode", "replace_text", "doc_id", d.DocID}, args...)...)
	}
	if c, ok := out.Get("candidates"); ok {
		var steps []string
		for _, n := range vault.CoerceList(c) {
			steps = append(steps, retry("heading", n)+" with the same old_text and new_text")
		}
		return steer(out, steps...)
	}
	if !out.Failed() || !out.Has("count") {
		return guide(out, v.sectionSteps(d, a.Mode))
	}
	read := Call("read_doc", "doc_id", d.DocID)
	if a.Heading != nil {
		read = Call("read_doc", "doc_id", d.DocID, "sections", []string{*a.Heading})
	}
	if out.Int("count") == 0 {
		if strings.Contains(out.Str("error"), "set_frontmatter") {
			return steer(out, Call("set_frontmatter", "relative_path", d.RelativePath)+" with the field to change")
		}
		steps := []string{read + " and copy the passage exactly"}
		if a.Heading != nil {
			steps = append(steps, retry("old_text", a.OldText, "new_text", *a.NewText)+" without heading to search the whole body")
		}
		return steer(out, steps...)
	}
	steps := []string{read + fmt.Sprintf(", then the same call with an old_text long enough that only one of its %d matches contains it", out.Int("count"))}
	if s, ok := out.Get("sections"); ok {
		for _, n := range vault.CoerceList(s) {
			steps = append(steps, retry("heading", n)+" with the same old_text and new_text, if the one you mean is there")
		}
	}
	return steer(out, steps...)
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
			return nil, fmt.Sprintf("duplicate doc_id %q at %s; resolve the duplicate first", docID, pystr.Quoted(dups)),
				[]string{Call("queue", "action", "add", "project", vault.BacklogProject, "kind", "follow-up", "text", "Resolve duplicate doc_id "+docID) + " and tell the user"}
		}
		if d, ok := idx.ByDocID[docID]; ok {
			return d, "", nil
		}
		return nil, "unknown doc_id " + strconv.Quote(docID) + "; call `find` to get one",
			[]string{Call("find", "query", docID) + " to get a doc_id, then retry with it"}
	case relativePath != "":
		clean := filepath.Clean(strings.TrimLeft(relativePath, "/"))
		for _, d := range idx.WithArchived() {
			if d.RelativePath == clean {
				return d, "", nil
			}
		}
		msg := "no registered doc at " + relativePath + `; create it with mode "create", or register it with set_frontmatter`
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
	note, err := root.ReadNote(rel)
	if err != nil {
		return "", nil, false
	}
	fm := note.FM
	if fm == nil {
		return rel, []string{"missing YAML frontmatter"}, true
	}
	idx := v.Loader.Index(false)
	return rel, slices.Concat(v.Profile.Check(fm, rel), v.Profile.RefProblems(fm, idx.Has), v.Profile.ProjectProblems(fm, idx.Projects)), true
}

// RelStem is used by tests to name a doc by its path stem.
func RelStem(rel string) string {
	base := filepath.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
