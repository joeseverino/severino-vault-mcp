package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
)

// The tools register through the SDK's raw Server.AddTool, which hands the
// arguments over unparsed and leaves checking them against the schema to the
// handler. So an argument the schema refuses fails here, in the house shape
// every other failure takes: {ok: false, error, next_steps}. The published
// schema stays as strict as the check.

// argFailure is a refused call's result. It stays a tool error, so a client
// doesn't hold it to the tool's output schema, and carries the failure as
// structured content too.
func argFailure(tool *mcp.Tool, raw json.RawMessage, err error) *mcp.CallToolResult {
	o := jsonx.Fail(err.Error())
	if ae, ok := errors.AsType[argError](err); ok {
		if steps := argSteps(tool, raw, ae); len(steps) > 0 {
			o.Set("next_steps", steps)
		}
	}
	return result(o)
}

// prop is the part of an argument's schema a step needs.
type prop struct {
	Type        any    `json:"type"`
	Enum        []any  `json:"enum"`
	Const       any    `json:"const"`
	Description string `json:"description"`
}

func (p prop) takes(t string) bool {
	switch x := p.Type.(type) {
	case string:
		return x == t
	case []any:
		return slices.Contains(x, any(t))
	}
	return false
}

// inputSchema is the part of a tool's input schema the argument checks read.
type inputSchema struct {
	Properties           map[string]prop  `json:"properties"`
	Required             []string         `json:"required"`
	AdditionalProperties *json.RawMessage `json:"additionalProperties"`
}

// readSchema is the one reader of a tool's input schema, which the SDK holds
// as raw JSON or as a value; an unreadable schema reads as empty.
func readSchema(schema any) inputSchema {
	raw, isRaw := schema.(json.RawMessage)
	if !isRaw {
		raw, _ = json.Marshal(schema)
	}
	var s inputSchema
	_ = json.Unmarshal(raw, &s)
	return s
}

// argSteps is the call to make instead: the same call with the argument at
// fault renamed, replaced or left out.
func argSteps(tool *mcp.Tool, raw json.RawMessage, e argError) []string {
	in := readSchema(tool.InputSchema)
	props, required := in.Properties, in.Required
	given := jsonx.New()
	if v, err := jsonx.Decode(raw); err == nil {
		if o, ok := v.(*jsonx.Obj); ok {
			given = o.Clone()
		}
	}
	render := func(o *jsonx.Obj) string {
		kv := make([]any, 0, 2*o.Len())
		for _, k := range o.Keys() {
			v, _ := o.Get(k)
			kv = append(kv, k, v)
		}
		return core.Call(tool.Name, kv...)
	}
	switch {
	case len(e.unknown) > 0:
		valid := make([]string, 0, len(props))
		for k := range props {
			valid = append(valid, k)
		}
		slices.Sort(valid)
		var renamed, dropped []string
		for _, u := range e.unknown {
			v := given.Pop(u)
			if tool.Name == "queue" && linkArgStep(given, u, v) {
				renamed = append(renamed, strconv.Quote(u)+" goes inside links")
				continue
			}
			if to := closestArg(u, valid); to != "" && !given.Has(to) {
				if s, isStr := v.(string); isStr && props[to].takes("array") && !props[to].takes("string") {
					v = []any{s}
				}
				given.Set(to, v)
				renamed = append(renamed, strconv.Quote(u)+" is "+strconv.Quote(to))
				continue
			}
			dropped = append(dropped, u)
		}
		var notes []string
		if len(renamed) > 0 {
			notes = append(notes, strings.Join(renamed, ", "))
		}
		if len(dropped) > 0 {
			notes = append(notes, fmt.Sprintf("without %s", pystr.Quoted(dropped)))
		}
		return []string{render(given) + ": " + strings.Join(notes, "; ")}
	case e.arg == "":
		return []string{tool.Name + "(...) with the arguments as one JSON object"}
	case e.arg == "items":
		return []string{tool.Name + "(items=[...]) with each change's fields inside its own item, fixed as the error says"}
	case e.missing:
		given.Set(e.arg, "...")
		step := render(given) + " with " + e.arg + " set"
		if d := props[e.arg].Description; d != "" {
			step += ": " + strings.ToLower(d[:1]) + d[1:]
		}
		return []string{step}
	}
	bad, _ := given.Get(e.arg)
	given.Delete(e.arg)
	// find's filters are arguments of their own, not ways to match.
	if tool.Name == "find" && e.arg == "by" {
		s, _ := bad.(string)
		if slices.Contains(findFilters, s) {
			q := given.Str("query")
			if q == "" {
				q = "..."
			}
			given.Set(s, q)
			return []string{render(given) + " to keep docs whose " + s + " matches; by is how query matches, " +
				strconv.Quote(core.FindModes[0]) + " or " + strconv.Quote(core.FindModes[1])}
		}
		return []string{render(given) + " with by one of " + jsonx.Compact(props[e.arg].Enum) + " (how query matches), or without it for the default",
			render(given.Set("system", "...")) + " to keep docs by a field instead: system=, project= or doc_type=, each its own argument"}
	}
	step := render(given) + " with " + e.arg + " " + want(props[e.arg], e.msg)
	if !slices.Contains(required, e.arg) {
		step += ", or without it for the default"
	}
	return []string{step}
}

// findFilters are find's arguments that narrow by a doc field, which agents
// pass as a by value.
var findFilters = []string{"system", "project"}

// want says what an argument takes.
func want(p prop, msg string) string {
	switch {
	case len(p.Enum) > 0:
		return "one of " + jsonx.Compact(p.Enum)
	case p.Const != nil:
		return "set to " + jsonx.Compact(p.Const)
	}
	if _, should, ok := strings.Cut(msg, "input should be "); ok {
		return "as " + should
	}
	return "fixed as the error says"
}

// closestArg is the declared name an unknown one most plausibly meant: its
// singular or plural, or the one name within two edits of it.
func closestArg(u string, valid []string) string {
	for _, v := range []string{u + "s", strings.TrimSuffix(u, "s")} {
		if v != u && slices.Contains(valid, v) {
			return v
		}
	}
	if len(u) < 4 {
		return ""
	}
	best, bestDist, ties := "", 3, 0
	for _, v := range valid {
		switch d := pystr.EditDistance(u, v); {
		case d < bestDist:
			best, bestDist, ties = v, d, 1
		case d == bestDist:
			ties++
		}
	}
	if ties != 1 {
		return ""
	}
	return best
}
