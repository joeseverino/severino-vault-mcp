package mcpserver

import (
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/queue"
)

// queueLimit is the queue list's default cap.
const queueLimit = 50

func (h *handlers) queue() coreTool {
	linkProps := jsonx.New()
	for _, k := range queue.LinkKeys {
		linkProps.Set(k, jsonx.New("type", []string{"string", "null"}))
	}
	input := object(jsonx.New(
		"action", enum(core.QueueActions, core.QueueActions[0], "list, add, set or close."),
		"vault", h.vaultProp("Which vault."),
		"project", optStr("The queue's project folder under 01 Projects, or backlog."),
		"repo", optStr("A repository directory name, resolved through index.md repos."),
		"cwd", optStr("A working directory, resolved to its repository's project."),
		"all", boolean("list: every queue."),
		"id", optStr("set, close: the item's id, e.g. example-3."),
		"body", optStr("add, set: the doc_id of the doc holding the item's detail; it must exist (\"\" removes it on set)."),
		"agent", optStr("add: the worker's agent id. list: only that agent's items. set, close: the item by agent instead of id; set with id writes the agent onto that item."),
		"origin", optStr("add: the sending session's repo or working directory. list: only items from it."),
		"include_origin", boolean("list: also delegated items anywhere whose origin is this repo."),
		"kind", optStr("add: "+strings.Join(queue.Kinds, ", ")+". list: only this kind. set: delegated, for a next or follow-up item (the only change)."),
		"text", optStr("add: one line saying what the work is. set: replace it. Escaped in the note, never by you."),
		"owner", optStr("add, set: who holds the item (one word; \"\" or \"-\" for nobody). list: only that owner's items."),
		"links", jsonx.New("type", []string{"object", "null"}, "properties", linkProps, "additionalProperties", false,
			"description", "add, set, close: typed links, each one word; on set, \"\" removes one. list: only items whose links equal these, e.g. {\"branch\": \"pass/x\"}."),
		"blocked_by", strList("add, set: item ids that must close before this one is ready."),
		"repos", strList("add, set: the repos one worker's item spans."),
		"base", optStr("add, set: the branch the item's work lands on (\"\" removes it on set)."),
		"transcript", optStr("add, set: the absolute path of the worker transcript holding the item's brief and report (\"\" removes it on set)."),
		"owns", strList("add, set: the paths the item's worker owns ([] clears them on set)."),
		"status", optStr("set: open or returned. close: closed (default) or dropped. list: comma-separated statuses or all; default open,returned."),
		"ready", boolean("list: only open next and follow-up items whose blocked_by are all closed or dropped."),
		"contains", optStr("list: only items whose text contains this (case-insensitive)."),
		"limit", integer(queueLimit, "list: most items to return; 0 for all; `count` is all that matched."),
		"claim", boolean("set: take the item for owner (and agent) only if no other owner or agent holds it and it is not done; it becomes open."),
		"release", boolean("set: hand the item back to the queue: no agent or owner, kind next (from delegated), status open. With id, agent names the holder it must have."),
		"resolution", optStr("close: one line: repo@sha (comma-separated for repos), a PR, a doc id, or why it was dropped."),
		"partial", boolean("close: record these repo@sha landings, closing once every repo has one."),
		"fields", strList("list: item fields to return. Default: id, kind, status, owner, text (and project, origin "+
			"on include_origin); also links (or links.pr), blocked_by, body, agent, repos, base, owns, transcript, created, resolution."),
	), nil)
	return coreTool{"queue", descQueue, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		q := queueArgs(p)
		if p.err != nil {
			return nil, p.err
		}
		return core.Queue(v, q), nil
	}}
}

func queueArgs(p *params) core.QueueArgs {
	q := core.QueueArgs{
		Action:        p.choice("action", core.QueueActions, core.QueueActions[0]),
		Project:       p.str("project"),
		Repo:          p.str("repo"),
		Cwd:           p.str("cwd"),
		ID:            p.str("id"),
		Agent:         p.str("agent"),
		Origin:        p.str("origin"),
		Kind:          p.str("kind"),
		Status:        p.str("status"),
		Contains:      p.str("contains"),
		Resolution:    p.str("resolution"),
		Text:          p.opt("text"),
		Owner:         p.opt("owner"),
		Body:          p.opt("body"),
		Base:          p.opt("base"),
		All:           p.boolean("all"),
		Ready:         p.boolean("ready"),
		Claim:         p.boolean("claim"),
		Release:       p.boolean("release"),
		IncludeOrigin: p.boolean("include_origin"),
		Partial:       p.boolean("partial"),
		Limit:         p.integer("limit", queueLimit),
	}
	q.BlockedBy, q.HasBlockedBy = p.listGiven("blocked_by")
	q.Repos, q.HasRepos = p.listGiven("repos")
	q.Transcript = p.opt("transcript")
	q.Owns, q.HasOwns = p.listGiven("owns")
	q.Fields = p.list("fields")
	q.Links = queueLinks(p)
	return q
}

// queueLinks reads links, one string (or null, for "") per link key.
func queueLinks(p *params) map[string]string {
	raw, ok := p.value("links")
	if !ok {
		return nil
	}
	o, isObj := raw.(*jsonx.Obj)
	if !isObj {
		p.fail(argError{msg: "links: input should be an object", arg: "links"})
		return nil
	}
	links := map[string]string{}
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		switch x := v.(type) {
		case nil:
			links[k] = ""
		case string:
			links[k] = x
		default:
			p.fail(argError{msg: "links." + k + ": input should be a valid string", arg: "links"})
			return nil
		}
	}
	return links
}

const descQueue = `Track work in a project's queue: one Obsidian task per item in
01 Projects/<project>/Queue.md (cross-cutting work in 07 Backlog/Queue.md).
Kinds: next, delegated (a worker holds it), decision (the user must
answer), follow-up. Status: open, returned (delegated only), closed,
dropped; a box ticked in Obsidian reads as closed. Each item carries the
date it was filed (created); a worker's item may carry base (the branch
it lands on), owns (the paths its worker owns) and transcript (the
absolute path of the worker transcript holding its brief and report).

Name the queue by project, repo or cwd (through the projects' index.md
repos). A write from a repo no project lists is refused with the call that
maps it (the backlog takes writes only as project=backlog); a list there
reads the backlog under a warning.
list (default): open and returned items; all=true every queue;
    include_origin adds the workers this repo sent to other queues.
add: kind and text; returns the new id (an agent's open item with the
    same text answers instead, as no_op); body names its detail doc.
set: id (or agent, when it holds one item) and what changes; claim=true
    with owner takes an item no other owner or agent holds, atomically.
    With an id, agent, kind=delegated (from next or follow-up) and
    links.branch ride in the same locked edit. release=true hands an item
    back as unowned next work. A change to an item another agent holds
    goes through with a warning naming it.
close: id (or agent) and resolution; status=dropped for work not done. An
    item with repos closes once each has a repo@sha; partial=true adds some.`

// linkArgStep moves a link key given at the top level of a queue call into
// links, the form the tool takes it in. It reports whether name was one.
func linkArgStep(given *jsonx.Obj, name string, v any) bool {
	if !slices.Contains(queue.LinkKeys, name) {
		return false
	}
	links, _ := given.Get("links")
	o, ok := links.(*jsonx.Obj)
	if !ok {
		o = jsonx.New()
		given.Set("links", o)
	}
	o.Set(name, v)
	return true
}
