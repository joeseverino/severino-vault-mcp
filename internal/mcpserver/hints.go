package mcpserver

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

// Every core tool works inside the vault on disk, never the open world.
var closedWorld = new(false)

// annotations are the core tools' behaviour hints. Destructive is honest
// rather than hopeful: set_frontmatter and write_doc overwrite values and
// sections, and task_write can delete a task. Idempotent means repeating the
// same call leaves the vault as the first call did.
var annotations = map[string]*mcp.ToolAnnotations{
	"find":            {ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: closedWorld},
	"read_doc":        {ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: closedWorld},
	"task_board":      {ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: closedWorld},
	"set_frontmatter": {DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: closedWorld},
	"write_doc":       {DestructiveHint: new(true), OpenWorldHint: closedWorld},
	"task_write":      {DestructiveHint: new(true), OpenWorldHint: closedWorld},
}

// outputSchemas describe each core tool's structuredContent. They name the
// keys a result can carry and leave every key optional, since a failure is
// {"ok": false, "error": ...} in the same object; the record fields come from
// the same declarations the field masks check against.
var outputSchemas = map[string]json.RawMessage{
	"find": outSchema(jsonx.New(
		"ok", ty("boolean"), "error", ty("string"), "mode", ty("string"), "filters", ty("object"),
		"fallback", ty("string"), "match_count", ty("integer"), "words", textList(),
		"recommended", ty("object"), "hits", records(core.HitFields),
	)),
	"read_doc": outSchema(fieldProps(core.DocFields).Merge(jsonx.New(
		"found", ty("boolean"), "resolved_from_alias", ty("object"), "body_released", ty("boolean"),
		"advisory", ty("string"), "unlock", ty("object"), "override_used", ty("boolean"),
		"sections", jsonx.New("type", "array", "items", jsonx.New("type", "object", "properties", jsonx.New(
			"section", ty("string"), "heading_path", ty("string"), "body", ty("string")))),
		"section_error", ty("string"), "available_sections", jsonx.New("type", "array", "items", ty("object")),
		"ok", ty("boolean"), "error", ty("string"), "ambiguous", ty("boolean"), "paths", textList(),
		"guidance", ty("string"), "alias_hint", ty("string"),
	))),
	"task_board": outSchema(jsonx.New(
		"ok", ty("boolean"), "error", ty("string"), "count", ty("integer"), "total", ty("integer"),
		"tasks", records(core.TaskFields), "shipped", records(core.TaskFields), "projects", textList(),
	)),
	"task_write": outSchema(jsonx.New(
		"ok", ty("boolean"), "error", ty("string"), "doc_id", ty("string"), "relative_path", ty("string"),
		"applied", ty("integer"), "failed", jsonx.New("type", "array", "items", ty("integer")),
		"results", jsonx.New("type", "array", "items", ty("object")),
	)),
	"set_frontmatter": outSchema(jsonx.New("ok", ty("boolean"), "error", ty("string"))),
	"write_doc":       outSchema(jsonx.New("ok", ty("boolean"), "error", ty("string"))),
}

func ty(t any) *jsonx.Obj { return jsonx.New("type", t) }

func textList() *jsonx.Obj { return jsonx.New("type", "array", "items", ty("string")) }

func records(fs []core.Field) *jsonx.Obj {
	return jsonx.New("type", "array", "items", jsonx.New("type", "object", "properties", fieldProps(fs)))
}

func fieldProps(fs []core.Field) *jsonx.Obj {
	props := jsonx.New()
	for _, f := range fs {
		switch {
		case f.Inner != nil:
			props.Set(f.Name, records(f.Inner))
		case f.Type == "array":
			props.Set(f.Name, textList())
		default:
			props.Set(f.Name, ty(f.Type))
		}
	}
	return props
}

func outSchema(props *jsonx.Obj) json.RawMessage {
	b, _ := jsonx.Encode(jsonx.New("type", "object", "properties", props), jsonx.Options{})
	return b
}
