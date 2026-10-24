package mcpserver

import (
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

// logLimit is log_entries' default cap on a list.
const logLimit = 50

func (h *handlers) logEntries() coreTool {
	input := object(jsonx.New(
		"doc_id", str("The log note's doc_id (from `find`)."),
		"action", enum(core.LogActions, core.LogActions[0], "list, close or annotate; see the tool description."),
		"vault", h.vaultProp("Which vault."),
		"open_only", jsonx.New("type", "boolean", "default", true, "description", "list: only entries without a resolution."),
		"contains", optStr("list: only entries whose text contains this (case-insensitive)."),
		"limit", integer(logLimit, "list: most entries to return, in file order; 0 for all. `count` says how many matched."),
		"match", optStr("close: text (case-insensitive) that exactly one open entry contains; annotate: one closed entry."),
		"resolution", optStr("close: one line saying how the entry was resolved, e.g. a commit; annotate: the line that replaces its resolution."),
		"reopen", boolean("annotate: remove the entry's resolution, so it is open again."),
		"fields", strList("Entry fields to return. Default: line, text; also closed, resolution."),
	), []string{"doc_id"})
	return coreTool{"log_entries", descLogEntries, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		id := p.required("doc_id")
		l := core.LogArgs{Action: p.choice("action", core.LogActions, core.LogActions[0]), OpenOnly: true}
		if p.given("open_only") {
			l.OpenOnly = p.boolean("open_only")
		}
		l.Contains = p.str("contains")
		l.Match = p.str("match")
		l.Resolution = p.str("resolution")
		l.Reopen = p.boolean("reopen")
		l.Limit = p.integer("limit", logLimit)
		l.Fields = p.list("fields")
		if p.err != nil {
			return nil, p.err
		}
		return core.LogEntries(v, id, l), nil
	}}
}

const descLogEntries = `Work an append-only log note: one top-level bullet per entry, closed once
its line carries " → " and a resolution.

list (default): the entries as {line, text}, open ones only unless
    open_only=false, narrowed by contains, in file order.
close: append " → <resolution>" to the one open entry match names; zero or
    several matches is an error listing the candidates.
annotate: on the one closed entry match names, replace the text after
    " → " with resolution, or remove it with reopen=true; matched like close.
close and annotate keep the frontmatter as it is except last_reviewed,
    which becomes today.
Restricted notes are refused; notes in read-only folders can be listed, not closed.`
