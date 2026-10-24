package core

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// View is how much of a result the caller asks for. Fields is an AIP-161
// field mask over each record (a find hit, a task, the doc read_doc
// returns); nil gives the tool's lean default. Full, with no Fields, returns
// the whole result unmasked, which the CLI prints for the scripts that read it.
type View struct {
	Fields []string
	Full   bool
}

// Field is one name a record can carry. Inner names the fields of an object
// value, or of each object in a list value; a field without Inner is only
// ever kept whole. Type is the value's JSON Schema type, which the MCP
// server's output schemas are built from.
type Field struct {
	Name  string
	Type  any
	Inner []Field
}

// JSON Schema types of record values. A plain array is a list of strings.
const (
	tStr  = "string"
	tInt  = "integer"
	tBool = "boolean"
	tList = "array"
)

var tOptStr = []string{"string", "null"}

// typed reads name/type pairs.
func typed(pairs ...any) []Field {
	out := make([]Field, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Field{Name: pairs[i].(string), Type: pairs[i+1]})
	}
	return out
}

// list is a field holding a list of objects shaped by inner.
func list(name string, inner []Field) Field { return Field{Name: name, Type: "array", Inner: inner} }

// mask is a parsed field mask: each name keeps its whole value (nil), or
// only the names under it.
type mask map[string]mask

// FieldsError is a refused field mask. Fixed is the mask with every refused
// path replaced by the field it most plausibly meant, nil when one has no
// such field, so a refusal can name the call that works.
type FieldsError struct {
	msg   string
	Fixed []string
}

func (e *FieldsError) Error() string { return e.msg }

// parseFields reads dotted paths into one mask checked against shape. Shared
// prefixes merge and a whole value wins over a path into it. An error names
// the path by its index and the valid names at the failing depth, never the
// caller's text, and the field a refused name most plausibly meant.
func parseFields(fields []string, shape []Field) (mask, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("fields: give at least one field, or omit fields for the default; valid fields are %s", names(shape))
	}
	m := mask{}
	var first string
	fixed := slices.Clone(fields)
	fixable := true
	for i, path := range fields {
		msg, meant := checkPath(i, path, shape)
		if msg == "" {
			m.merge(strings.Split(path, "."))
			continue
		}
		if first == "" {
			first = msg
		}
		if meant == "" {
			fixable = false
		}
		fixed[i] = meant
	}
	if first == "" {
		return m, nil
	}
	e := &FieldsError{msg: first}
	if fixable {
		e.Fixed = fixed
	}
	return nil, e
}

// checkPath is why path is refused ("" when it is valid), and the path it
// most plausibly meant ("" when none).
func checkPath(i int, path string, shape []Field) (msg, meant string) {
	segments := strings.Split(path, ".")
	if slices.Contains(segments, "") {
		return fmt.Sprintf("fields[%d]: a path is field names joined by single dots", i), ""
	}
	level, at := shape, []string{}
	for _, seg := range segments {
		if level == nil {
			return fmt.Sprintf("fields[%d]: %s is kept whole; it has no fields inside it", i, strings.Join(at, ".")), strings.Join(at, ".")
		}
		j := slices.IndexFunc(level, func(f Field) bool { return f.Name == seg })
		if j < 0 {
			where := "the top level"
			if len(at) > 0 {
				where = strings.Join(at, ".")
			}
			msg = fmt.Sprintf("fields[%d]: unknown field under %s; valid fields there are %s", i, where, names(level))
			var paths []string
			for _, g := range meantFields(seg, level, len(at) == 0) {
				paths = append(paths, strings.Join(append(slices.Clone(at), g), "."))
			}
			switch len(paths) {
			case 0:
			case 1:
				meant = paths[0]
				msg += fmt.Sprintf("; fields[%d] is likely %q", i, meant)
			default:
				msg += fmt.Sprintf("; fields[%d] is likely one of %s", i, pystr.Quoted(paths))
			}
			return msg, meant
		}
		at = append(at, seg)
		level = level[j].Inner
	}
	return "", ""
}

// meantFields are the fields of level an unknown name most plausibly
// meant: those within two edits of it, or whose name holds it as a word
// (path in obsidian_path); at the top level, failing those, a field inside a
// list named exactly so (kind in snippets.kind).
func meantFields(name string, level []Field, top bool) []string {
	var fits []string
	for _, f := range level {
		if pystr.EditDistance(name, f.Name) <= 2 && len(name) >= 4 ||
			slices.Contains(strings.Split(f.Name, "_"), name) || slices.Contains(strings.Split(name, "_"), f.Name) && len(f.Name) >= 4 {
			fits = append(fits, f.Name)
		}
	}
	if top && len(fits) == 0 {
		for _, f := range level {
			if slices.ContainsFunc(f.Inner, func(in Field) bool { return in.Name == name }) {
				fits = append(fits, f.Name+"."+name)
			}
		}
	}
	return fits
}

func names(shape []Field) string {
	names := make([]string, 0, len(shape))
	for _, f := range shape {
		names = append(names, f.Name)
	}
	return pystr.Quoted(names)
}

func (m mask) merge(segments []string) {
	node := m
	for _, seg := range segments[:len(segments)-1] {
		child, seen := node[seg]
		if seen && child == nil {
			return // the whole value is already kept
		}
		if child == nil {
			child = mask{}
			node[seg] = child
		}
		node = child
	}
	node[segments[len(segments)-1]] = nil
}

// mustMask parses a default mask; a default that doesn't fit its shape is a
// programming error.
func mustMask(shape []Field, fields ...string) mask {
	m, err := parseFields(fields, shape)
	if err != nil {
		panic(err)
	}
	return m
}

// with is m plus names kept whole whatever the caller asked for.
func (m mask) with(always ...string) mask {
	out := make(mask, len(m)+len(always))
	maps.Copy(out, m)
	for _, k := range always {
		out[k] = nil
	}
	return out
}

// cut is v reduced to m, keeping v's own key order. A path through a list
// applies to every element and keeps the list's length.
func cut(v any, m mask) any {
	switch x := v.(type) {
	case *jsonx.Obj:
		out := jsonx.New()
		for _, k := range x.Keys() {
			inner, ok := m[k]
			if !ok {
				continue
			}
			val, _ := x.Get(k)
			if inner != nil {
				val = cut(val, inner)
			}
			out.Set(k, val)
		}
		return out
	case []*jsonx.Obj:
		out := make([]*jsonx.Obj, 0, len(x))
		for _, o := range x {
			out = append(out, cut(o, m).(*jsonx.Obj))
		}
		return out
	}
	return v
}

// selection resolves a View against a record shape and its lean default.
// nil means the caller asked for the whole result.
func (view View) selection(shape []Field, lean mask) (mask, error) {
	if view.Fields != nil {
		return parseFields(view.Fields, shape)
	}
	if view.Full {
		return nil, nil
	}
	return lean, nil
}

// cutEach replaces o[key], a list of records, with each record cut to m.
func cutEach(o *jsonx.Obj, key string, m mask) {
	if v, ok := o.Get(key); ok {
		o.Set(key, cut(v, m))
	}
}

// keepOnly drops every key of o not in keep, keeping o's order.
func keepOnly(o *jsonx.Obj, keep ...string) *jsonx.Obj {
	out := jsonx.New()
	for _, k := range o.Keys() {
		if slices.Contains(keep, k) {
			v, _ := o.Get(k)
			out.Set(k, v)
		}
	}
	return out
}

// Text is a result as an MCP client reads it: compact JSON, since indentation
// is whitespace the model pays for and never reads.
func Text(o *jsonx.Obj) string {
	return jsonx.Compact(o)
}
