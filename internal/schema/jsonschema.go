package schema

import (
	"regexp"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

// JSONSchemaDialect is the JSON Schema draft JSONSchema emits.
const JSONSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

// JSONSchema is the profile's frontmatter rules as a JSON Schema (draft
// 2020-12), derived from Settable, so off-the-shelf validators and editors
// check a block the way Check does. A task (doc_type: task) is held to the
// task's required fields and lifecycle through if/then/else. What a schema
// can't see is left to Check and RefProblems and said in the field's
// description: a field's path scope, uniqueness across docs, immutability,
// and whether a doc reference names a doc that exists.
func (p *Profile) JSONSchema() *jsonx.Obj {
	shared, task, doc := jsonx.New(), jsonx.New(), jsonx.New()
	for _, f := range p.Settable() {
		switch {
		case p.docOnly(f.Name):
			doc.Set(f.Name, property(f, false))
		case len(f.TaskChoices) > 0:
			task.Set(f.Name, property(f, true))
			doc.Set(f.Name, property(f, false))
		default:
			shared.Set(f.Name, property(f, false))
		}
	}
	isTask := jsonx.New("properties", jsonx.New("doc_type", jsonx.New("const", "task")), "required", []string{"doc_type"})
	o := jsonx.New(
		"$schema", JSONSchemaDialect,
		"title", p.Name+" frontmatter",
		"description", "The frontmatter block of a doc in a "+p.Name+" vault. Fields it does not declare are allowed.",
		"type", "object",
		"$defs", jsonx.New("absent", jsonx.New("description", "A value Check treats as not given.", "enum", []any{nil, "", []any{}})),
		"properties", shared,
		"if", isTask,
		"then", branch(task, p.TaskRequiredFields),
		"else", branch(doc, p.RequiredFields),
	)
	var perType []any
	for _, name := range slices.Sorted(mapKeys(p.DocumentSchemas)) {
		ds := p.DocumentSchemas[name]
		props := jsonx.New()
		var required []string
		for _, f := range slices.Sorted(mapKeys(ds.Fields)) {
			props.Set(f, property(ds.Fields[f], false))
			if ds.Fields[f].Required {
				required = append(required, f)
			}
		}
		perType = append(perType, jsonx.New(
			"if", jsonx.New("properties", jsonx.New("doc_type", jsonx.New("const", name)), "required", []string{"doc_type"}),
			"then", branch(props, required)))
	}
	if len(perType) > 0 {
		o.Set("allOf", perType)
	}
	return o
}

// absent is a value Check treats as not given: null, "", or an empty list.
func absent() *jsonx.Obj { return jsonx.New("$ref", "#/$defs/absent") }

// branch holds props and requires each of required with a value that is not
// absent.
func branch(props *jsonx.Obj, required []string) *jsonx.Obj {
	o := jsonx.New("properties", props)
	if len(required) == 0 {
		return o
	}
	present := jsonx.New()
	for _, name := range required {
		present.Set(name, jsonx.New("not", absent()))
	}
	return o.Set("required", slices.Clone(required)).Set("allOf", []any{jsonx.New("properties", present)})
}

// property is one field's schema: absent, or a value its rule accepts.
func property(f Field, task bool) *jsonx.Obj {
	o := jsonx.New()
	if d := describe(f); d != "" {
		o.Set("description", d)
	}
	if f.Example != "" {
		o.Set("examples", []any{example(f)})
	}
	return o.Set("anyOf", []any{absent(), present(f, task)})
}

func example(f Field) any {
	if f.IsList() {
		return []any{f.Example}
	}
	return f.Example
}

// describe is the field's help and the rules a schema can't hold (an
// immutable field's help says so).
func describe(f Field) string {
	parts := []string{}
	if f.Help != "" {
		parts = append(parts, f.Help)
	}
	if f.Scope != "" {
		parts = append(parts, "Only on docs at "+f.Scope+".")
	}
	if f.Unique {
		parts = append(parts, "Unique across docs.")
	}
	if f.DocRef {
		parts = append(parts, "Names the doc_id of another doc in the vault.")
	}
	return strings.Join(parts, " ")
}

// present is the schema of a value f's rule accepts, the task lifecycle's
// choices when task is set.
func present(f Field, task bool) *jsonx.Obj {
	if f.IsList() {
		return jsonx.New("type", "array", "items", valueSchema(f, task))
	}
	var kind *jsonx.Obj
	switch f.Kind {
	case KindInteger:
		number := jsonx.New("type", "integer")
		if f.Minimum != nil {
			number.Set("minimum", *f.Minimum)
		}
		if f.Maximum != nil {
			number.Set("maximum", *f.Maximum)
		}
		// The frontmatter parser keeps numbers as text.
		kind = jsonx.New("anyOf", []any{number, jsonx.New("type", "string", "pattern", `^\s*[-+]?[0-9]+\s*$`)})
	case KindBoolean:
		kind = jsonx.New("type", "boolean")
	case KindDate:
		kind = jsonx.New("type", "string", "format", "date", "pattern", `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	case KindPath:
		kind = jsonx.New("type", "string", "pattern", `^(?:/|~/)`)
	default:
		kind = jsonx.New("type", "string")
	}
	return merge(kind, valueSchema(f, task))
}

// merge is a schema holding both a's and b's keywords, which apply together;
// a keyword both use keeps them apart under allOf.
func merge(a, b *jsonx.Obj) *jsonx.Obj {
	if slices.ContainsFunc(b.Keys(), a.Has) {
		return jsonx.New("allOf", []any{a, b})
	}
	return a.Merge(b)
}

// valueSchema is the rules on a value's text: choices, prefixes, pattern and
// reserved values.
func valueSchema(f Field, task bool) *jsonx.Obj {
	choices := f.Choices
	if task && len(f.TaskChoices) > 0 {
		choices = f.TaskChoices
	}
	var rules []*jsonx.Obj
	if len(choices) > 0 {
		rules = append(rules, jsonx.New("enum", Sorted(choices)))
	}
	if len(f.Prefixes) > 0 {
		quoted := make([]string, len(f.Prefixes))
		for i, pre := range f.Prefixes {
			quoted[i] = regexp.QuoteMeta(pre)
		}
		rules = append(rules, jsonx.New("pattern", "^(?:"+strings.Join(quoted, "|")+")"))
	}
	if f.Pattern != "" {
		rules = append(rules, jsonx.New("pattern", "^(?:"+f.Pattern+")$"))
	}
	if len(f.Reserved) > 0 {
		rules = append(rules, jsonx.New("not", jsonx.New("enum", Sorted(f.Reserved))))
	}
	out := jsonx.New()
	for _, r := range rules {
		out = merge(out, r)
	}
	return out
}
