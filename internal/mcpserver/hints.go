package mcpserver

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
)

// Every core tool works inside the vault on disk, never the open world.
var closedWorld = new(false)

// annotations are the core tools' behavior hints. set_frontmatter and
// write_doc are destructive: they overwrite values and sections. A tool is
// idempotent when repeating the same call leaves the vault as the first call
// did.
var annotations = map[string]*mcp.ToolAnnotations{
	"find":            {ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: closedWorld},
	"read_doc":        {ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: closedWorld},
	"set_frontmatter": {DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: closedWorld},
	"write_doc":       {DestructiveHint: new(true), OpenWorldHint: closedWorld},
	// A move keeps every byte and never replaces a file; repeating it finds
	// the doc already there.
	"move_doc": {DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: closedWorld},
	// A folder move rewrites links and related_projects in other notes;
	// repeating it finds the folder already gone.
	"move_folder": {DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: closedWorld},
	// Closing an entry appends to its line and never removes text.
	"log_entries": {DestructiveHint: new(false), OpenWorldHint: closedWorld},
	// set replaces an item's text, owner and links.
	"queue": {DestructiveHint: new(true), OpenWorldHint: closedWorld},
}

// toolMeta are the _meta keys Claude Code reads on a tools/list entry
// (code.claude.com/docs/en/mcp). anthropic/alwaysLoad keeps a tool out of
// tool search, so the two every vault question starts with need no search
// step. No tool raises anthropic/maxResultSizeChars past its 50,000-character
// default: read_doc bounds its own default answer (core.BoundedBodyChars), so
// only a whole body asked for by fields=["body"] can pass it, and that one
// is better saved to a file than read into context.
var toolMeta = map[string]mcp.Meta{
	"find":     {"anthropic/alwaysLoad": true},
	"read_doc": {"anthropic/alwaysLoad": true},
}

// outputSchemas describe each core tool's structuredContent: the envelope
// every result shares, then the tool's own keys. Every key is optional, since
// a failure is {"ok": false, "error": ...} in the same object; the record
// fields come from the same declarations the field masks check against.
var outputSchemas = map[string]json.RawMessage{
	"find": outSchema(jsonx.New(
		"mode", ty("string"), "filters", ty("object"),
		"fallback", ty("string"), "match_count", ty("integer"), "words", textList(), "ranking", ty("string"),
		"exact_match", jsonx.New("type", "object", "properties", jsonx.New("doc_id", ty("string"), "matched", ty("string"))),
		"recommended", ty("object"), "hits", records(core.HitFields), "warning", ty("string"),
		"truncated", jsonx.New("type", "object", "properties", jsonx.New(
			"budget_chars", ty("integer"), "snippets_trimmed", ty("boolean"), "hits_dropped", ty("integer"))),
	)),
	"read_doc": outSchema(fieldProps(core.DocFields).Merge(jsonx.New(
		"found", ty("boolean"), "resolved_from_alias", ty("object"), "body_released", ty("boolean"),
		"advisory", ty("string"), "unlock", ty("object"), "override_used", ty("boolean"),
		"sections", jsonx.New("type", "array", "items", jsonx.New("type", "object", "properties", jsonx.New(
			"section", ty("string"), "heading_path", ty("string"), "body", ty("string")))),
		"truncated", ty("boolean"), "lead", ty("string"),
		"section_error", ty("string"), "candidates", textList(), "available_sections", jsonx.New("type", "array", "items", ty("object")),
		"ambiguous", ty("boolean"), "paths", textList(),
		"alias_hint", ty("string"), "warning", ty("string"),
	))),
	"log_entries": outSchema(jsonx.New(
		"vault", ty("string"), "doc_id", ty("string"),
		"count", ty("integer"), "entries", records(core.EntryFields),
		"candidates", jsonx.New("type", "array", "items", ty("object")),
		"relative_path", ty("string"), "action", ty("string"), "line", ty("integer"), "text", ty("string"),
		"no_op", ty("boolean"), "wrote_bytes", ty("integer"), "next_step", ty("string"),
	)),
	"queue": outSchema(jsonx.New(
		"id", ty("string"), "project", ty("string"), "doc_id", ty("string"),
		"relative_path", ty("string"), "exists", ty("boolean"), "created_queue", ty("boolean"), "fallback", ty("string"), "warning", ty("string"),
		// item is one of items; the reference keeps the item shape out of tools/list twice.
		"item", jsonx.New("$ref", "#/properties/items/items"), "no_op", ty("boolean"),
		"owner", ty("string"), "count", ty("integer"), "items", records(core.QueueItemFields),
		"skipped_restricted", textList(), "unparsed_lines", ty("integer"),
		"results", jsonx.New("type", "array", "items", jsonx.New("type", "object", "properties", jsonx.New(
			"ok", ty("boolean"), "id", ty("string"), "project", ty("string"), "status", ty("string"), "resolution", ty("string"),
			"no_op", ty("boolean"), "warning", ty("string"), "error", ty("string"), "next_steps", textList()))),
	)),
	"set_frontmatter": outSchema(nil),
	"write_doc":       outSchema(nil),
	"move_doc": outSchema(jsonx.New(
		"doc_id", ty("string"), "from", ty("string"), "to", ty("string"),
		"folder", jsonx.New("type", "object", "properties", jsonx.New("from", ty("string"), "to", ty("string"))),
		"links", jsonx.New("type", "array", "items", jsonx.New("type", "object", "properties",
			jsonx.New("relative_path", ty("string"), "count", ty("integer")))),
		"skipped_restricted", textList(), "failed", textList(),
	)),
	"move_folder": outSchema(jsonx.New(
		"from", ty("string"), "to", ty("string"),
		"files", ty("integer"), "docs", ty("integer"),
		"links", jsonx.New("type", "array", "items", jsonx.New("type", "object", "properties",
			jsonx.New("relative_path", ty("string"), "count", ty("integer")))),
		"skipped_restricted", textList(),
		"project", jsonx.New("type", "object", "properties", jsonx.New(
			"from", ty("string"), "to", ty("string"), "related_projects", textList(), "related_projects_skipped", textList(),
			"queue", jsonx.New("type", "object", "properties", jsonx.New("doc_id", ty("string"), "relative_path", ty("string"), "title", ty("string"))),
			"queue_prefix", ty("string"))),
		"failed", textList(),
	)),
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
		case f.Type == "object":
			props.Set(f.Name, jsonx.New("type", "object", "properties", fieldProps(f.Inner)))
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
	envelope := jsonx.New("ok", ty("boolean"), "error", ty("string"), "next_steps", textList()).Merge(props)
	return json.RawMessage(jsonx.Compact(jsonx.New("type", "object", "properties", envelope)))
}
