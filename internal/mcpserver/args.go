package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
	"github.com/joeseverino/severino-vault-mcp/internal/vaults"
)

// ----- schemas ------------------------------------------------------------------

func object(props *jsonx.Obj, required []string) json.RawMessage {
	o := jsonx.New("type", "object", "properties", props, "additionalProperties", false)
	if len(required) > 0 {
		o.Set("required", required)
	}
	b, _ := jsonx.Encode(o, jsonx.Options{})
	return b
}

func str(desc string) *jsonx.Obj { return jsonx.New("type", "string", "description", desc) }
func optStr(desc string) *jsonx.Obj {
	return jsonx.New("type", []string{"string", "null"}, "description", desc)
}
func integer(def int, desc string) *jsonx.Obj {
	return jsonx.New("type", "integer", "default", def, "description", desc)
}
func boolean(desc string) *jsonx.Obj {
	return jsonx.New("type", "boolean", "default", false, "description", desc)
}
func strList(desc string) *jsonx.Obj {
	return jsonx.New("type", []string{"array", "null"}, "items", jsonx.New("type", "string"), "description", desc)
}
func enum(values []string, def, desc string) *jsonx.Obj {
	return jsonx.New("type", "string", "enum", values, "default", def, "description", desc)
}

func (h *handlers) vaultProp(desc string) *jsonx.Obj {
	names := h.reg.Names()
	o := jsonx.New("type", "string")
	if len(names) == 1 {
		o.Set("const", names[0])
	} else {
		o.Set("enum", names)
	}
	return o.Set("default", vaults.DefaultVault).Set("description", desc)
}

// ----- arguments ------------------------------------------------------------------

// addTool registers every tool, core or provider, so a call naming an argument
// its schema doesn't declare fails before the handler runs: a mistyped key
// would otherwise be dropped and the call answer as if it had done the work.
func addTool(s *mcp.Server, tool *mcp.Tool, next mcp.ToolHandler) {
	valid, closed := declaredArgs(tool.InputSchema)
	if !closed {
		s.AddTool(tool, next)
		return
	}
	s.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := unknownArgs(req.Params.Arguments, valid); err != nil {
			return argFailure(tool, req.Params.Arguments, err), nil
		}
		return next(ctx, req)
	})
}

// declaredArgs reads a schema's property names. A schema that declares no
// properties, or allows additional ones, is open.
func declaredArgs(schema any) ([]string, bool) {
	raw, isRaw := schema.(json.RawMessage)
	if !isRaw {
		b, err := json.Marshal(schema)
		if err != nil {
			return nil, false
		}
		raw = b
	}
	var s struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties *json.RawMessage           `json:"additionalProperties"`
	}
	if json.Unmarshal(raw, &s) != nil || s.Properties == nil {
		return nil, false
	}
	if s.AdditionalProperties != nil && strings.TrimSpace(string(*s.AdditionalProperties)) != "false" {
		return nil, false
	}
	names := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		names = append(names, k)
	}
	slices.Sort(names)
	return names, true
}

func unknownArgs(raw json.RawMessage, valid []string) error {
	var given map[string]json.RawMessage
	if json.Unmarshal(raw, &given) != nil {
		return nil // not an object; the handler reports it
	}
	var unknown []string
	for k := range given {
		if !slices.Contains(valid, k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	if len(valid) == 0 {
		return argError{msg: fmt.Sprintf("unknown arguments %s; this tool takes none", pystr.ReprList(unknown)), unknown: unknown}
	}
	return argError{msg: fmt.Sprintf("unknown arguments %s; valid arguments are %s", pystr.ReprList(unknown), pystr.ReprList(valid)), unknown: unknown}
}

// argError is an argument the schema refuses. arg names the one at fault and
// unknown the undeclared ones, so the failure can name the call to make instead.
type argError struct {
	msg     string
	arg     string
	missing bool
	unknown []string
}

func (e argError) Error() string { return e.msg }

// params reads one call's arguments. The first refusal sticks in err: every
// read after it returns its zero value, so a handler reads all its arguments
// and checks err once, and the refusal it reports is the first in read order.
type params struct {
	o   *jsonx.Obj
	err error
}

func parseParams(raw json.RawMessage) (*params, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return &params{o: jsonx.New()}, nil
	}
	v, err := jsonx.Decode(raw)
	if err != nil {
		return nil, argError{msg: "arguments are not valid JSON"}
	}
	o, ok := v.(*jsonx.Obj)
	if !ok {
		return nil, argError{msg: "arguments must be an object"}
	}
	return &params{o: o}, nil
}

// fail records a refusal unless an earlier one stands.
func (p *params) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}

func (p *params) refuse(name, should string) {
	p.fail(argError{msg: name + ": input should be " + should, arg: name})
}

// value is the argument's value when the call gives it as non-null and
// nothing has been refused yet.
func (p *params) value(name string) (any, bool) {
	if p.err != nil {
		return nil, false
	}
	v, ok := p.o.Get(name)
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

func (p *params) given(name string) bool {
	_, ok := p.value(name)
	return ok
}

// required is a string argument the call must give.
func (p *params) required(name string) string {
	if p.err == nil && !p.given(name) {
		p.fail(argError{msg: name + ": field required", arg: name, missing: true})
		return ""
	}
	return p.str(name)
}

func (p *params) str(name string) string { return p.strOr(name, "") }

func (p *params) strOr(name, def string) string {
	if s := p.opt(name); s != nil {
		return *s
	}
	if p.err != nil {
		return ""
	}
	return def
}

// opt is a string argument, nil when the call leaves it out.
func (p *params) opt(name string) *string {
	v, ok := p.value(name)
	if !ok {
		return nil
	}
	s, isStr := v.(string)
	if !isStr {
		p.refuse(name, "a valid string")
		return nil
	}
	return &s
}

func (p *params) integer(name string, def int) int {
	v, ok := p.value(name)
	if !ok {
		if p.err != nil {
			return 0
		}
		return def
	}
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n)
		}
		if f, err := x.Float64(); err == nil && f == float64(int(f)) {
			return int(f)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%d", &n); err == nil {
			return n
		}
	}
	p.refuse(name, "a valid integer")
	return 0
}

func (p *params) boolean(name string) bool {
	v, ok := p.value(name)
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(x) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	case json.Number:
		switch x.String() {
		case "1":
			return true
		case "0":
			return false
		}
	}
	p.refuse(name, "a valid boolean")
	return false
}

func (p *params) list(name string) []string {
	l, _ := p.listGiven(name)
	return l
}

// listGiven is a list argument and whether the call gives it, so an empty
// list can mean "clear" where leaving it out means "keep".
func (p *params) listGiven(name string) ([]string, bool) {
	v, ok := p.value(name)
	if !ok {
		return nil, false
	}
	arr, isArr := v.([]any)
	if !isArr {
		p.refuse(name, "a valid list")
		return nil, false
	}
	out := []string{}
	for _, item := range arr {
		s, isStr := item.(string)
		if !isStr {
			p.fail(argError{msg: name + ": items should be strings", arg: name})
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func (p *params) choice(name string, choices []string, def string) string {
	s := p.strOr(name, def)
	if p.err != nil {
		return ""
	}
	if !slices.Contains(choices, s) {
		var quoted []string
		for _, c := range choices {
			quoted = append(quoted, pystr.Repr(c))
		}
		p.fail(argError{msg: fmt.Sprintf("%s: input should be %s", name, strings.Join(quoted, " or ")), arg: name})
		return ""
	}
	return s
}
