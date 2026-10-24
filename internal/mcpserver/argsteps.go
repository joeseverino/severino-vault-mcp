package mcpserver

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
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
	o := jsonx.New("ok", false, "error", err.Error())
	if ae, ok := errors.AsType[argError](err); ok {
		if steps := argSteps(tool, raw, ae); len(steps) > 0 {
			o.Set("next_steps", steps)
		}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: core.Text(o)}}, StructuredContent: o, IsError: true}
}

// prop is the part of an argument's schema a step needs.
type prop struct {
	Type  any   `json:"type"`
	Enum  []any `json:"enum"`
	Const any   `json:"const"`
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

func schemaProps(schema any) (map[string]prop, []string) {
	raw, isRaw := schema.(json.RawMessage)
	if !isRaw {
		raw, _ = json.Marshal(schema)
	}
	var s struct {
		Properties map[string]prop `json:"properties"`
		Required   []string        `json:"required"`
	}
	_ = json.Unmarshal(raw, &s)
	return s.Properties, s.Required
}

// argSteps is the call to make instead: the same call with the argument at
// fault renamed, replaced or left out.
func argSteps(tool *mcp.Tool, raw json.RawMessage, e argError) []string {
	props, required := schemaProps(tool.InputSchema)
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
			if to := closestArg(u, valid); to != "" && !given.Has(to) {
				if s, isStr := v.(string); isStr && props[to].takes("array") && !props[to].takes("string") {
					v = []any{s}
				}
				given.Set(to, v)
				renamed = append(renamed, pystr.Repr(u)+" is "+pystr.Repr(to))
				continue
			}
			dropped = append(dropped, u)
		}
		var notes []string
		if len(renamed) > 0 {
			notes = append(notes, strings.Join(renamed, ", "))
		}
		if len(dropped) > 0 {
			notes = append(notes, "without "+pystr.ReprList(dropped))
		}
		return []string{render(given) + ": " + strings.Join(notes, "; ")}
	case e.arg == "":
		return []string{tool.Name + "(...) with the arguments as one JSON object"}
	case e.arg == "items":
		return []string{tool.Name + "(items=[...]) with each change's fields inside its own item, fixed as the error says"}
	case e.missing:
		return []string{render(given) + " with " + e.arg + " set"}
	}
	bad, _ := given.Get(e.arg)
	given.Delete(e.arg)
	// find's filters are arguments of their own, not ways to match.
	if s, _ := bad.(string); tool.Name == "find" && e.arg == "by" && (s == "system" || s == "project") {
		q := given.Str("query")
		if q == "" {
			q = "..."
		}
		given.Set(s, q)
		return []string{render(given) + " to keep docs whose " + s + " matches; by is how query matches, " +
			pystr.Repr(core.FindModes[0]) + " or " + pystr.Repr(core.FindModes[1])}
	}
	step := render(given) + " with " + e.arg + " " + want(props[e.arg], e.msg)
	if !slices.Contains(required, e.arg) {
		step += ", or without it for the default"
	}
	return []string{step}
}

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
		switch d := editDistance(u, v); {
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

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
