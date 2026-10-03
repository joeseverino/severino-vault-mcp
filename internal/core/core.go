// Package core is the shared vault tool surface, transport-free: find,
// read_doc, set_frontmatter, update_link, task_board, task_write,
// recent_changes and daily_progress, plus the two resource renderers. Each
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
	"github.com/joeseverino/severino-vault-mcp/internal/daily"
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
		"the local MCP will still require an interactive unlock on the Mac.",
	"disabled": "Interactive unlock is disabled. Set SVMC_ALLOW_RESTRICTED_UNLOCK=1 " +
		"in the local MCP environment to allow local unlock prompts.",
	"no_unlock_hash": "No local unlock hash is configured. Store the output of " +
		"`severino-vault-mcp unlock-hash` in Keychain, SVMC_RESTRICTED_UNLOCK_HASH_FILE, or SVMC_RESTRICTED_UNLOCK_HASH.",
	"prompt_unavailable": "Local hidden-input prompt was unavailable or cancelled.",
	"failed":             "Local unlock phrase verification failed.",
}

func wikiTargets(text string) []string {
	var out []string
	parts := strings.Split(text, "[[")
	for _, part := range parts[1:] {
		target := strings.SplitN(part, "]]", 2)[0]
		target = strings.SplitN(target, "|", 2)[0]
		target = pystr.Strip(strings.SplitN(target, "#", 2)[0])
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

func findRelevance(v *Vault, q string, limit int) *jsonx.Obj {
	resp := query.FindSections(v.Loader, q, limit)
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

func hitList(docs []*vault.Doc) []*jsonx.Obj {
	out := []*jsonx.Obj{}
	for _, d := range docs {
		out = append(out, query.Hit(d))
	}
	return out
}

func findSystem(v *Vault, q string) *jsonx.Obj {
	needle := strings.ToLower(pystr.Strip(q))
	if needle == "" {
		return jsonx.New("query", q, "match_count", 0, "hits", []*jsonx.Obj{})
	}
	var matches []*vault.Doc
	for _, d := range v.Loader.Index(false).Docs {
		if strings.Contains(strings.ToLower(d.System), needle) || strings.Contains(strings.ToLower(d.Title), needle) ||
			strings.Contains(strings.ToLower(d.DocID), needle) {
			matches = append(matches, d)
		}
	}
	slices.SortStableFunc(matches, func(a, b *vault.Doc) int {
		ai, bi := boolInt(a.Status != "active"), boolInt(b.Status != "active")
		if ai != bi {
			return ai - bi
		}
		if c := strings.Compare(a.LastReviewed, b.LastReviewed); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	return jsonx.New("query", q, "match_count", len(matches), "hits", hitList(matches))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func findProject(v *Vault, q string) *jsonx.Obj {
	slug := pystr.Strip(q)
	if slug == "" {
		return jsonx.New("query", q, "match_count", 0, "hits", []*jsonx.Obj{})
	}
	var matches []*vault.Doc
	for _, d := range v.Loader.Index(false).Docs {
		if slices.Contains(d.RelatedProjects, slug) {
			matches = append(matches, d)
		}
	}
	slices.SortStableFunc(matches, func(a, b *vault.Doc) int {
		if c := strings.Compare(a.DocType, b.DocType); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	return jsonx.New("query", q, "match_count", len(matches), "hits", hitList(matches))
}

// FindArgs are find's options.
type FindArgs struct {
	By            string
	Limit         int
	ContextLines  int
	CaseSensitive bool
}

// Find searches one vault in the requested mode.
func Find(v *Vault, q string, a FindArgs) *jsonx.Obj {
	var resp *jsonx.Obj
	switch a.By {
	case "relevance":
		resp = findRelevance(v, q, a.Limit)
		hits, _ := resp.Get("hits")
		resp.Set("match_count", len(hits.([]*jsonx.Obj)))
	case "system":
		resp = findSystem(v, q)
	case "project":
		resp = findProject(v, q)
	case "text":
		resp = query.SearchBody(v.Loader, q, a.Limit, a.ContextLines, a.CaseSensitive)
		if !resp.Has("error") {
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
		}
	default:
		return jsonx.New("ok", false, "error", fmt.Sprintf("unknown mode %s; one of relevance, system, project, text", pystr.Repr(a.By)))
	}
	return jsonx.New("vault", v.Name, "mode", a.By).Merge(resp)
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
		return jsonx.New("doc_id", identifier, "found", false, "ambiguous", true,
			"error", "duplicate doc_id "+pystr.Repr(identifier), "paths", slices.Clone(dups),
			"guidance", "Resolve the duplicate frontmatter IDs before reading this document.")
	}
	return jsonx.New("doc_id", identifier, "found", false,
		"guidance", "Call `find` to get a doc_id, then retry `read_doc` with it.",
		"alias_hint", "For recurring local phrases, add an entry to the vault aliases "+
			"file configured by `SVMC_ALIASES_PATH` or `[aliases].path`.")
}

func withheld(base *jsonx.Obj, result string) *jsonx.Obj {
	base.Set("body_released", false)
	base.Set("advisory", gate.Advisory(gate.Parse(base.Str("sensitivity")), false))
	if result != "" {
		base.Set("unlock", jsonx.New("allowed", false, "result", result, "message", unlockMessages[result]))
	}
	return base
}

func unlock(v *Vault, docID, title string) (bool, string, string) {
	c := v.Config
	if !c.AllowRestrictedUnlock {
		return false, "disabled", unlockMessages["disabled"]
	}
	hash := gate.LoadHash(c.RestrictedUnlockHash, c.RestrictedUnlockHashFile, c.RestrictedKeychainService, c.RestrictedKeychainAccount)
	if hash == "" {
		return false, "no_unlock_hash", unlockMessages["no_unlock_hash"]
	}
	phrase, ok := gate.PromptPhrase(docID, title)
	if !ok {
		return false, "prompt_unavailable", unlockMessages["prompt_unavailable"]
	}
	if !gate.VerifyPhrase(phrase, hash) {
		return false, "failed", unlockMessages["failed"]
	}
	return true, "released", "Local unlock succeeded for this request only."
}

func narrow(base *jsonx.Obj, d *vault.Doc, section string) *jsonx.Obj {
	sec := sections.Resolve(d.Sections, section)
	if sec == nil {
		base.Delete("body")
		base.Set("body_released", false)
		base.Set("section_error", fmt.Sprintf("no section %s in %s", pystr.Repr(section), d.DocID))
		base.Set("available_sections", query.AvailableSections(d))
		return base
	}
	heading := sec.Heading
	if heading == "" {
		heading = d.Title
	}
	base.Set("body", sec.Body)
	base.Set("heading", heading)
	base.Set("section", sec.Slug)
	base.Set("heading_path", sec.HeadingPath)
	base.Set("body_scope", "section")
	return base
}

// ReadDoc reads a doc's body behind the sensitivity gate.
func ReadDoc(v *Vault, docID, section string, includeRestricted bool) *jsonx.Obj {
	return jsonx.New("vault", v.Name).Merge(readDoc(v, docID, section, includeRestricted))
}

func readDoc(v *Vault, docID, section string, includeRestricted bool) *jsonx.Obj {
	d, alias := lookupDoc(v, docID)
	if d == nil {
		return notFound(v, docID)
	}
	base := jsonx.New("doc_id", d.DocID, "found", true).Merge(query.Hit(d))
	if alias != nil {
		base.Set("resolved_from_alias", alias)
	}
	if d.Sensitivity == gate.Restricted {
		if !includeRestricted {
			return withheld(base, "not_requested")
		}
		allowed, result, msg := unlock(v, d.DocID, d.Title)
		gate.AuditUnlock(v.Config.RestrictedAuditLog, d.DocID, result)
		base.Set("unlock", jsonx.New("allowed", allowed, "result", result, "message", msg))
		if !allowed {
			return withheld(base, result)
		}
		base.Set("body", d.Body)
		base.Set("body_released", true)
		base.Set("override_used", true)
		base.Set("advisory", gate.Advisory(d.Sensitivity, true))
		if section != "" {
			return narrow(base, d, section)
		}
		return base
	}
	base.Set("body", d.Body)
	base.Set("body_released", true)
	if d.Sensitivity == gate.Sensitive {
		base.Set("advisory", gate.Advisory(d.Sensitivity, false))
	}
	if section != "" {
		return narrow(base, d, section)
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

// RecentChanges lists a vault's recent commits.
func RecentChanges(v *Vault, days, limit int) *jsonx.Obj {
	return query.RecentChanges(v.Loader, days, limit)
}

// DailyProgress reads the daily note a progress question refers to.
func DailyProgress(v *Vault, q, today string) *jsonx.Obj {
	return daily.Progress(v.Config, q, today)
}

// SetFrontmatter creates or updates a doc's frontmatter.
func SetFrontmatter(v *Vault, relativePath string, s write.Set) *jsonx.Obj {
	if v.Profile == nil {
		return jsonx.New("ok", false, "error", "vault "+pystr.Repr(v.Name)+" has no schema profile")
	}
	return write.SetFrontmatter(v.Loader, relativePath, s, v.Profile)
}

// UpdateLink replaces one exact Markdown link in a doc.
func UpdateLink(v *Vault, docID, label, expected, replacement string) *jsonx.Obj {
	return write.UpdateLink(v.Loader, docID, label, expected, replacement)
}

// TaskBoard is a vault's board plus its projects.
func TaskBoard(v *Vault, f tasks.Filter) *jsonx.Obj {
	board := tasks.List(v.Loader, f)
	projects, _ := tasks.Projects(v.Loader).Get("projects")
	board.Set("projects", projects)
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
			return jsonx.New("ok", false, "error", fmt.Sprintf("action %s requires %s", pystr.Repr(a.Action), strings.Join(absent, ", ")))
		}
		return nil
	}
	switch a.Action {
	case "add":
		if err := missing("title"); err != nil {
			return err
		}
		return tasks.Add(v.Loader, tasks.New{
			Title: a.Title, Project: a.Project, RelatedProjects: a.RelatedProjects,
			Effort: a.Effort, Priority: a.Priority, Tags: a.Tags,
		}, tasks.Sections{Problem: a.Problem, Fix: a.Fix, Principle: a.Principle, Source: a.Source})
	case "status":
		if err := missing("doc_id", "status"); err != nil {
			return err
		}
		return tasks.SetStatus(v.Loader, a.DocID, a.Status)
	case "promote":
		if err := missing("note_path", "title"); err != nil {
			return err
		}
		return tasks.Promote(v.Loader, a.NotePath, tasks.New{Title: a.Title, Project: a.Project, Effort: a.Effort, Priority: a.Priority})
	case "delete":
		if err := missing("doc_id"); err != nil {
			return err
		}
		return tasks.Delete(v.Loader, a.DocID)
	}
	return jsonx.New("ok", false, "error", fmt.Sprintf("unknown action %s; one of add, status, promote, delete", pystr.Repr(a.Action)))
}

// RelStem is used by tests to name a doc by its path stem.
func RelStem(rel string) string {
	base := filepath.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
