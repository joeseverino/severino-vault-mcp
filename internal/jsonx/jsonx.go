// Package jsonx is the module's one JSON home: an insertion-ordered object for
// dynamic payloads, encoded and decoded through encoding/json.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
)

// Obj is a JSON object that keeps insertion order.
type Obj struct {
	keys []string
	vals map[string]any
}

// New builds an Obj from alternating key, value pairs.
func New(kv ...any) *Obj {
	o := &Obj{vals: map[string]any{}}
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// Failure is the failure envelope, {"ok": false} followed by kv. Every ok:false
// object in the server is built here (archtest.TestOneFailureEnvelope).
func Failure(kv ...any) *Obj { return New(append([]any{"ok", false}, kv...)...) }

// Fail is a failure with one error message.
func Fail(msg string) *Obj { return Failure("error", msg) }

// Failed reports whether o is a failure envelope: ok present and false.
func (o *Obj) Failed() bool {
	ok, has := o.Get("ok")
	return has && ok == false
}

// Set assigns k, keeping its position when it already exists.
func (o *Obj) Set(k string, v any) *Obj {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

// Get returns the value at k.
func (o *Obj) Get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[k]
	return v, ok
}

// Has reports whether k is present.
func (o *Obj) Has(k string) bool {
	_, ok := o.Get(k)
	return ok
}

// Str returns the string at k, or "".
func (o *Obj) Str(k string) string {
	v, _ := o.Get(k)
	s, _ := v.(string)
	return s
}

// Int returns the int at k, or 0.
func (o *Obj) Int(k string) int {
	v, _ := o.Get(k)
	n, _ := v.(int)
	return n
}

// Text is the value at k as pystr.Str writes it when the value is
// truthy, else "". A nil o has no fields.
func (o *Obj) Text(k string) string {
	if o == nil {
		return ""
	}
	v, _ := o.Get(k)
	if !pystr.Truthy(v) {
		return ""
	}
	return pystr.Str(v)
}

// Strings is a string list as JSON array values.
func Strings(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

// Bool returns the bool at k, or false.
func (o *Obj) Bool(k string) bool {
	v, _ := o.Get(k)
	b, _ := v.(bool)
	return b
}

// Delete removes k.
func (o *Obj) Delete(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	o.keys = slices.DeleteFunc(o.keys, func(s string) bool { return s == k })
}

// Pop removes k and returns its value.
func (o *Obj) Pop(k string) any {
	v, _ := o.Get(k)
	o.Delete(k)
	return v
}

// Keys returns the keys in order.
func (o *Obj) Keys() []string {
	if o == nil {
		return nil
	}
	return slices.Clone(o.keys)
}

// Len is the number of keys.
func (o *Obj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Merge sets every key of other in other's order; a key o already has keeps its place.
func (o *Obj) Merge(other *Obj) *Obj {
	if other == nil {
		return o
	}
	for _, k := range other.keys {
		o.Set(k, other.vals[k])
	}
	return o
}

// Clone is a shallow copy.
func (o *Obj) Clone() *Obj {
	c := &Obj{vals: map[string]any{}}
	if o == nil {
		return c
	}
	c.keys = slices.Clone(o.keys)
	maps.Copy(c.vals, o.vals)
	return c
}

// MarshalJSON emits the object in insertion order.
func (o *Obj) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := put(&buf, k); err != nil {
			return nil, err
		}
		buf.WriteByte(':')
		if err := put(&buf, o.vals[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes a JSON object, keeping key order.
func (o *Obj) UnmarshalJSON(data []byte) error {
	v, err := Decode(data)
	if err != nil {
		return err
	}
	obj, ok := v.(*Obj)
	if !ok {
		return fmt.Errorf("jsonx: not an object")
	}
	*o = *obj
	return nil
}

// put appends v as JSON, through encoding/json with HTML escaping off. A nil
// slice is an empty array: a list field is always a list to the reader.
func put(buf *bytes.Buffer, v any) error {
	if items, ok := v.([]any); ok {
		buf.WriteByte('[')
		for i, item := range items {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := put(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
		buf.WriteString("[]")
		return nil
	}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	buf.Truncate(buf.Len() - 1) // Encode ends each value with a newline
	return nil
}

func marshal(v any) []byte {
	var buf bytes.Buffer
	if err := put(&buf, v); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// Compact is v as JSON with no whitespace.
func Compact(v any) string { return string(marshal(v)) }

// Pretty is v as JSON indented by two spaces.
func Pretty(v any) string {
	var out bytes.Buffer
	if err := json.Indent(&out, marshal(v), "", "  "); err != nil {
		panic(err)
	}
	return out.String()
}

// Dumps is the CLI output contract: compact, or indented under --pretty.
func Dumps(v any, pretty bool) string {
	if pretty {
		return Pretty(v)
	}
	return Compact(v)
}

// Sorted is v with every object's keys sorted: its objects become maps,
// which encoding/json writes in key order.
func Sorted(v any) any {
	dec := json.NewDecoder(bytes.NewReader(marshal(v)))
	dec.UseNumber()
	var plain any
	if err := dec.Decode(&plain); err != nil {
		panic(err)
	}
	return plain
}

// Decode parses JSON into nil, bool, json.Number, string, []any and *Obj,
// keeping object key order.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("jsonx: trailing data")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := New()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(key, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("jsonx: unexpected delimiter %v", t)
	default:
		return t, nil
	}
}
