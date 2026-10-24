package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
)

// boardLimit is task_board's default cap: the top of the ranked board.
const boardLimit = 25

func (h *handlers) taskBoard() coreTool {
	input := object(jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"status", optStr("Only this status."),
		"project", optStr("Only this project."),
		"stale_only", boolean("Only open or active tasks idle past stale_days."),
		"include_all", boolean("Include parked, done, wontfix."),
		"stale_days", integer(14, "Stale window in days."),
		"include_queue", boolean("Add open and returned queue items (of project, else every queue) under `queue`."),
		"limit", integer(boardLimit, "Most tasks, best first; 0 for all."),
		"fields", strList("Task fields to return (also slug, related_projects, effort, created, closed, tags, relative_path, age_days, stale)."),
	), nil)
	return coreTool{"task_board", descBoard, input, func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		f := tasks.Filter{
			Status:     p.str("status"),
			Project:    p.str("project"),
			StaleOnly:  p.boolean("stale_only"),
			IncludeAll: p.boolean("include_all"),
			StaleDays:  p.integer("stale_days", 14),
		}
		b := core.BoardArgs{
			Limit:        p.integer("limit", boardLimit),
			IncludeQueue: p.boolean("include_queue"),
			Fields:       p.list("fields"),
		}
		if p.err != nil {
			return nil, p.err
		}
		return core.TaskBoard(v, f, b), nil
	}}
}

func (h *handlers) taskWrite() coreTool {
	item := taskItemProps()
	props := jsonx.New(
		"vault", h.vaultProp("Which vault."),
		"items", jsonx.New("type", []string{"array", "null"}, "maxItems", core.MaxTaskItems,
			"items", jsonx.New("type", "object", "properties", item, "required", []string{"action"}, "additionalProperties", false),
			"description", "Several changes in one call, each with the fields below; leave the top-level ones unset."),
	).Merge(item)
	props.Set("action", jsonx.New("type", "string", "enum", core.TaskActions, "description", "add, status, promote or delete; omit with items."))
	return coreTool{"task_write", descTaskWrite, object(props, nil), func(p *params) (*jsonx.Obj, error) {
		v := h.vault(p)
		raw, batch := p.value("items")
		if !batch {
			t := taskItem(p)
			if p.err != nil {
				return nil, p.err
			}
			return core.TaskWrite(v, t), nil
		}
		changes := taskItems(p, raw, item.Keys())
		if p.err != nil {
			return nil, p.err
		}
		return core.TaskWrites(v, changes), nil
	}}
}

// taskItemProps are one task change's arguments, top-level or in items.
func taskItemProps() *jsonx.Obj {
	p := jsonx.New("action", jsonx.New("type", "string", "enum", core.TaskActions, "description", "add, status, promote or delete."))
	for _, f := range []string{"title", "project"} {
		p.Set(f, optStr(""))
	}
	p.Set("related_projects", strList(""))
	p.Set("effort", jsonx.New("type", "string", "default", "S", "description", "S, M or L."))
	p.Set("priority", jsonx.New("type", "string", "default", "med", "description", "high, med or low."))
	p.Set("tags", strList(""))
	for _, f := range []string{"problem", "fix", "principle", "source", "doc_id", "status", "note_path"} {
		p.Set(f, optStr(""))
	}
	return p
}

func taskItem(p *params) core.TaskWriteArgs {
	return core.TaskWriteArgs{
		Action:          p.required("action"),
		Title:           p.str("title"),
		Project:         p.str("project"),
		Effort:          p.strOr("effort", "S"),
		Priority:        p.strOr("priority", "med"),
		Problem:         p.str("problem"),
		Fix:             p.str("fix"),
		Principle:       p.str("principle"),
		Source:          p.str("source"),
		DocID:           p.str("doc_id"),
		Status:          p.str("status"),
		NotePath:        p.str("note_path"),
		RelatedProjects: p.list("related_projects"),
		Tags:            p.list("tags"),
	}
}

// taskItems reads items, a batch of task changes. Each item's refusal is
// reported under items[<index>], with the item's own fields checked as the
// top-level ones are.
func taskItems(p *params, raw any, valid []string) []core.TaskWriteArgs {
	refuse := func(msg string) { p.fail(argError{msg: msg, arg: "items"}) }
	for _, k := range p.o.Keys() {
		if k != "vault" && k != "items" && p.given(k) {
			refuse("items: give each change's fields inside its item, not beside items (got " + pystr.Repr(k) + ")")
			return nil
		}
	}
	list, isList := raw.([]any)
	if !isList {
		refuse("items: input should be a valid list")
		return nil
	}
	var batch []core.TaskWriteArgs
	for i, entry := range list {
		o, isObj := entry.(*jsonx.Obj)
		if !isObj {
			refuse(fmt.Sprintf("items[%d]: input should be an object", i))
			return nil
		}
		if err := unknownArgs(json.RawMessage(jsonx.Compact(o)), valid); err != nil {
			refuse(fmt.Sprintf("items[%d]: %s", i, err))
			return nil
		}
		ip := &params{o: o}
		t := taskItem(ip)
		if ip.err != nil {
			refuse(fmt.Sprintf("items[%d]: %s", i, ip.err))
			return nil
		}
		batch = append(batch, t)
	}
	return batch
}

const descBoard = `A vault's task board: open work, counts, recently shipped, and the projects tasks can go in.`

const descTaskWrite = `Change a vault's tasks.

add: file a task (title; optional project, related_projects, effort S|M|L,
    priority high|med|low, tags, and the body sections problem, fix,
    principle, source). Without project it goes to the cross-cutting backlog.
status: move doc_id to status (open, active, parked, done, wontfix);
    done stamps ` + "`closed:`" + `.
promote: turn the inbox note at note_path into a task titled title,
    keeping its body and removing the note.
delete: remove doc_id permanently (mistakes only; finished work is
    status done or wontfix).
items: a list of these changes, each with its own action, applied in
    order in one call; each reports its own result, and a failure doesn't
    stop the rest or undo the earlier ones.`
