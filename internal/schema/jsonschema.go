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
// check a block the way Check does. In a profile that declares tasks, a task
// (doc_type: task) is held to the task's required fields and lifecycle
// through if/then/else, and a field
// only some doc_types carry is held absent on the others. What a schema
// can't see is left to Check and RefProblems and said in the description: a
// field's path scope, uniqueness across docs, immutability, whether a doc
// reference names a doc that exists, and the folder a doc_type lives in.
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
	isTask := ofType(taskDocType)
	// A doc_type with its own required set is held to it in its allOf entry
	// below, and not to the profile's.
	notTask := branch(doc, p.RequiredFields)
	if own := p.ownRequired(); len(own) > 0 {
		notTask = jsonx.New("properties", doc, "if", ofType(own...), "else", requires(p.RequiredFields))
	}
	o := jsonx.New(
		"$schema", JSONSchemaDialect,
		"title", p.Name+" frontmatter",
		"description", "The frontmatter block of a doc in a "+p.Name+" vault. Fields it does not declare are allowed."+p.placementText(),
		"type", "object",
		"$defs", jsonx.New("absent", jsonx.New("description", "A value Check treats as not given.", "enum", []any{nil, "", []any{}})),
	)
	if p.HasDocType(taskDocType) {
		o.Set("properties", shared).Set("if", isTask).Set("then", branch(task, p.TaskRequiredFields)).Set("else", notTask)
	} else {
		// No doc is a task, so every block is held to the doc branch.
		props, _ := notTask.Get("properties")
		o.Set("properties", shared.Merge(props.(*jsonx.Obj)))
		for _, k := range notTask.Keys() {
			if k != "properties" {
				v, _ := notTask.Get(k)
				o.Set(k, v)
			}
		}
	}
	var perType []any
	for _, name := range slices.Sorted(mapKeys(p.DocumentSchemas)) {
		ds := p.DocumentSchemas[name]
		props := jsonx.New()
		required := slices.Clone(ds.RequiredFields)
		for _, f := range slices.Sorted(mapKeys(ds.Fields)) {
			props.Set(f, property(ds.Fields[f], false))
			if ds.Fields[f].Required && !slices.Contains(required, f) {
				required = append(required, f)
			}
		}
		perType = append(perType, jsonx.New("if", ofType(name), "then", branch(props, required)))
	}
	for _, f := range p.Settable() {
		if len(f.DocTypes) > 0 {
			perType = append(perType, jsonx.New("if", jsonx.New("not", ofType(Sorted(f.DocTypes)...)),
				"then", jsonx.New("properties", jsonx.New(f.Name, absent()))))
		}
	}
	if len(perType) > 0 {
		o.Set("allOf", perType)
	}
	return o
}

// placementText says where each doc_type with a folder lives, "" when none
// has one.
func (p *Profile) placementText() string {
	var out strings.Builder
	for _, t := range slices.Sorted(mapKeys(p.DocTypeFolders)) {
		out.WriteString(" A doc_type " + t + " doc lives in a " + p.DocTypeFolders[t] + " folder, and only those docs do.")
	}
	return out.String()
}

// absent is a value Check treats as not given: null, "", or an empty list.
func absent() *jsonx.Obj { return jsonx.New("$ref", "#/$defs/absent") }

// branch holds props and requires each of required with a value that is not
// absent.
func branch(props *jsonx.Obj, required []string) *jsonx.Obj {
	return jsonx.New("properties", props).Merge(requires(required))
}

// requires is the schema that holds each of required with a value that is
// not absent; empty when required is.
func requires(required []string) *jsonx.Obj {
	if len(required) == 0 {
		return jsonx.New()
	}
	present := jsonx.New()
	for _, name := range required {
		present.Set(name, jsonx.New("not", absent()))
	}
	return jsonx.New("required", slices.Clone(required), "allOf", []any{jsonx.New("properties", present)})
}

// ofType matches a block whose doc_type is one of names.
func ofType(names ...string) *jsonx.Obj {
	match := jsonx.New("const", names[0])
	if len(names) > 1 {
		match = jsonx.New("enum", slices.Clone(names))
	}
	return jsonx.New("properties", jsonx.New("doc_type", match), "required", []string{"doc_type"})
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
	if len(f.DocTypes) > 0 {
		parts = append(parts, "Only on doc_type "+strings.Join(Sorted(f.DocTypes), ", ")+".")
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
