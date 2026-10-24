package mcpserver

import (
	"fmt"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

func (h *handlers) find() coreTool {
	input := object(jsonx.New(
		"query", str("What to look for."),
		"vault", h.vaultProp("Which vault to search."),
		"by", enum(core.FindModes, core.FindModes[0], "relevance or text; see the tool description."),
		"system", optStr("Only docs whose `system` contains this."),
		"project", optStr("Only docs with a `related_projects` entry containing this."),
		"doc_type", optStr("Only docs of this doc_type. Queue notes (doc_type queue) are left out unless asked for."),
		"limit", integer(10, "Maximum hits (documents for `text`)."),
		"context_lines", integer(1, "Lines of context per match, `text` only."),
		"case_sensitive", boolean("Case-sensitive match, `text` only."),
		"include_archived", boolean("Also search the archive folder's docs (status archived), left out by default."),
		"fields", strList("Hit fields as dotted paths (`tags`, `snippets.text`)."),
	), []string{"query"})
	return coreTool{"find", descFind, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		q := p.required("query")
		f := core.FindArgs{
			By:              p.choice("by", core.FindModes, core.FindModes[0]),
			System:          p.str("system"),
			Project:         p.str("project"),
			Limit:           p.integer("limit", 10),
			ContextLines:    p.integer("context_lines", 1),
			CaseSensitive:   p.boolean("case_sensitive"),
			DocType:         p.str("doc_type"),
			IncludeArchived: p.boolean("include_archived"),
			Fields:          p.list("fields"),
		}
		if p.err != nil {
			return nil, p.err
		}
		return core.Find(v, q, f), nil
	}}
}

func (h *handlers) readDoc() coreTool {
	input := object(jsonx.New(
		"doc_id", str("A `find` hit's doc_id (a title or vault path also resolves)."),
		"vault", h.vaultProp("Which vault the doc lives in."),
		"sections", strList("Sections to read, each a slug, heading path, heading text, unique slug prefix or 1-based outline position."),
		"fields", strList("Doc fields to return; `outline` lists sections with sizes, `body` is the whole body, frontmatter fields (`tags`, `system`) add more."),
		"include_restricted", boolean("Request a restricted body."),
	), []string{"doc_id"})
	return coreTool{"read_doc", descReadDoc, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		id := p.required("doc_id")
		r := core.ReadArgs{
			Sections:          p.list("sections"),
			Fields:            p.list("fields"),
			IncludeRestricted: p.boolean("include_restricted"),
		}
		if p.err != nil {
			return nil, p.err
		}
		return core.ReadDoc(v, id, r), nil
	}}
}

const descFind = `Find docs in a vault. Use this first for any question the vault may answer.

Returns ` + "`match_count`" + ` and ` + "`hits`" + ` (each with a doc_id); then ` + "`read_doc`" + ` the best hit.

by relevance (default) ranks title, system, tag and section matches. by text
searches bodies: several words match docs holding any of them, ranked by
relevance, a hit lacking some listing them in missing_words; a single token
or a regex is a ripgrep pattern; zero hits fall back to relevance
("fallback"); restricted bodies are never searched. A query that is a doc's
doc_id, title or alias puts that doc first (exact_match).`

var descReadDoc = fmt.Sprintf(`Read a vault doc's markdown body, gated by its sensitivity.

A body over %d characters comes back as its lead and outline (each
section's size) with truncated true; read the part you need with sections
(the hit's `+"`section`"+`), or the whole with fields=["body"]. Restricted bodies are
withheld unless include_restricted is set and a Touch ID approval through
1Password succeeds.`, core.BoundedBodyChars)
