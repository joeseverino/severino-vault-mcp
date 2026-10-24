package write

import (
	"fmt"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
)

// ListOp is a list field change: Set replaces; Add appends new values;
// Remove drops values. Nil means "not given".
type ListOp struct {
	Set, Add, Remove []string
	HasSet           bool
}

func (op ListOp) given() bool { return op.HasSet || op.Add != nil || op.Remove != nil }

// created is the list a new block starts with: Set, then Add.
func (op ListOp) created() []string { return applyListOp(op.Set, ListOp{Add: op.Add}) }

func applyListOp(current []string, op ListOp) []string {
	if op.HasSet {
		current = op.Set
	}
	out := slices.Clone(current)
	if out == nil {
		out = []string{}
	}
	if len(op.Remove) > 0 {
		out = slices.DeleteFunc(out, func(v string) bool { return slices.Contains(op.Remove, v) })
	}
	for _, v := range op.Add {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// Changes are field changes by field name: Values sets scalar fields (a
// string, or an int or bool for integer and boolean fields), Lists edits list
// fields, and Touch sets date fields to today.
type Changes struct {
	Values map[string]any
	Lists  map[string]ListOp
	Touch  []string
}

// With is Changes from name/value pairs: a []string replaces a list field,
// anything else sets a scalar one.
func With(pairs ...any) Changes {
	var c Changes
	for i := 0; i+1 < len(pairs); i += 2 {
		name := pairs[i].(string)
		if l, isList := pairs[i+1].([]string); isList {
			c = c.List(name, ListOp{Set: l, HasSet: true})
		} else {
			c = c.Set(name, pairs[i+1])
		}
	}
	return c
}

// Set is c with a scalar field's new value.
func (c Changes) Set(name string, value any) Changes {
	if c.Values == nil {
		c.Values = map[string]any{}
	}
	c.Values[name] = value
	return c
}

// List is c with a list field's change, merged with any earlier one.
func (c Changes) List(name string, op ListOp) Changes {
	if c.Lists == nil {
		c.Lists = map[string]ListOp{}
	}
	cur := c.Lists[name]
	if op.HasSet {
		cur.Set, cur.HasSet = op.Set, true
	}
	if op.Add != nil {
		cur.Add = append(cur.Add, op.Add...)
	}
	if op.Remove != nil {
		cur.Remove = append(cur.Remove, op.Remove...)
	}
	c.Lists[name] = cur
	return c
}

// Touched is c with a date field set to today.
func (c Changes) Touched(name string) Changes {
	c.Touch = append(slices.Clone(c.Touch), name)
	return c
}

// Empty reports no change at all.
func (c Changes) Empty() bool { return len(c.Values) == 0 && len(c.Lists) == 0 && len(c.Touch) == 0 }

// Str is a scalar field's new value as text, "" when not given.
func (c Changes) Str(name string) string {
	v, ok := c.Values[name]
	if !ok {
		return ""
	}
	return pystr.Str(v)
}

// names lists every field c touches, sorted.
func (c Changes) names() []string {
	var out []string
	for k := range c.Values {
		out = append(out, k)
	}
	for k := range c.Lists {
		out = append(out, k)
	}
	out = append(out, c.Touch...)
	slices.Sort(out)
	return slices.Compact(out)
}

// undeclared refuses a change the profile cannot take: a field it does not
// declare, or an operation the field's kind does not have.
func (c Changes) undeclared(p *schema.Profile) string {
	var settable []string
	for _, f := range p.Settable() {
		settable = append(settable, f.Name)
	}
	for _, name := range c.names() {
		f, ok := p.Field(name)
		if !ok {
			return fmt.Sprintf("%s is not a field of the %s profile; it declares %s", pystr.Repr(name), p.Name, pystr.ReprList(settable))
		}
		_, isValue := c.Values[name]
		_, isList := c.Lists[name]
		switch {
		case isValue && f.IsList():
			return fmt.Sprintf("%s is a list; replace, add to or remove from it", name)
		case isList && !f.IsList():
			return fmt.Sprintf("%s is not a list; give it one value", name)
		case slices.Contains(c.Touch, name) && !f.Touch:
			return fmt.Sprintf("%s cannot be touched; give it a value", name)
		}
	}
	return ""
}

// Arg operations: what one argument does to its field.
const (
	OpSet     = "set"
	OpReplace = "replace"
	OpAdd     = "add"
	OpRemove  = "remove"
	OpTouch   = "touch"
)

// Arg is one argument a declared field gives a frontmatter writer. Key is
// its name, in snake case; each surface spells it its own way.
type Arg struct {
	Key   string
	Op    string
	Field schema.Field
}

// Args are the arguments fields give a writer, in field order. A list's
// whole-list key is replace plus its name ("" on MCP, "set_" on the CLI);
// create leaves out what only an update does (add, remove, touch).
func Args(fields []schema.Field, replace string, create bool) []Arg {
	var out []Arg
	for _, f := range fields {
		if !f.IsList() {
			out = append(out, Arg{f.Name, OpSet, f})
			if f.Touch && !create {
				out = append(out, Arg{"touch_" + f.Name, OpTouch, f})
			}
			continue
		}
		out = append(out, Arg{replace + f.Name, OpReplace, f})
		if !create {
			out = append(out, Arg{"add_" + f.Name, OpAdd, f}, Arg{"remove_" + f.Name, OpRemove, f})
		}
	}
	return out
}

// Help describes the argument.
func (a Arg) Help() string {
	f := a.Field
	switch a.Op {
	case OpTouch:
		return "Set " + f.Name + " to today."
	case OpAdd:
		return "Append to " + f.Name + "."
	case OpRemove:
		return "Remove from " + f.Name + "."
	}
	var parts []string
	if f.Help != "" {
		parts = append(parts, f.Help)
	}
	switch f.Kind {
	case schema.KindDate:
		parts = append(parts, "YYYY-MM-DD.")
	case schema.KindPath:
		parts = append(parts, "Absolute or ~/ path.")
	}
	if f.Shape != "" {
		parts = append(parts, strings.ToUpper(f.Shape[:1])+f.Shape[1:]+".")
	}
	if f.Unique {
		parts = append(parts, "Unique.")
	}
	if f.Scope != "" {
		parts = append(parts, "Only on "+f.Scope+".")
	}
	if a.Op == OpReplace {
		parts = append(parts, "Replaces the list.")
	}
	return strings.Join(parts, " ")
}

// Apply records one given argument: a string, int or bool for a set, a list
// for a list operation, true for a touch.
func (c *Changes) Apply(a Arg, value any) {
	name := a.Field.Name
	switch a.Op {
	case OpSet:
		*c = c.Set(name, value)
	case OpTouch:
		if on, _ := value.(bool); on {
			c.Touch = append(c.Touch, name)
		}
	case OpReplace:
		*c = c.List(name, ListOp{Set: value.([]string), HasSet: true})
	case OpAdd:
		*c = c.List(name, ListOp{Add: value.([]string)})
	case OpRemove:
		*c = c.List(name, ListOp{Remove: value.([]string)})
	}
}
