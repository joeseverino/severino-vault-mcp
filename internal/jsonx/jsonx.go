// Package jsonx is the module's one JSON home: an insertion-ordered object for
// dynamic payloads, and an encoder whose output matches Python's json.dumps
// byte for byte (key order, ensure_ascii escapes, separators), so committed
// contracts and CLI output stay identical across the port.
package jsonx

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
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

// Merge sets every key of other in other's order (Python's {**a, **b}).
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
	for k, v := range o.vals {
		c.vals[k] = v
	}
	return c
}

// MarshalJSON emits the object in insertion order.
func (o *Obj) MarshalJSON() ([]byte, error) {
	return Encode(o, Options{})
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

// Options select the json.dumps flavor.
type Options struct {
	Indent      int  // 0 = compact with (",", ":") separators
	SortKeys    bool // sort object keys
	EnsureASCII bool // escape non-ASCII as \uXXXX
}

// Compact is Python's json.dumps(obj, separators=(",", ":")).
func Compact(v any) string { return mustEncode(v, Options{EnsureASCII: true}) }

// Pretty is Python's json.dumps(obj, indent=2).
func Pretty(v any) string { return mustEncode(v, Options{Indent: 2, EnsureASCII: true}) }

// Canonical is Python's json.dumps(obj, indent=2, sort_keys=True).
func Canonical(v any) string {
	return mustEncode(v, Options{Indent: 2, SortKeys: true, EnsureASCII: true})
}

// Dumps is the CLI output contract: compact, or indented under --pretty.
func Dumps(v any, pretty bool) string {
	if pretty {
		return Pretty(v)
	}
	return Compact(v)
}

func mustEncode(v any, opts Options) string {
	b, err := Encode(v, opts)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Encode serializes v.
func Encode(v any, opts Options) ([]byte, error) {
	var sb strings.Builder
	if err := encode(&sb, v, opts, 0); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func encode(sb *strings.Builder, v any, opts Options, depth int) error {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case string:
		writeString(sb, x, opts.EnsureASCII)
	case int:
		sb.WriteString(strconv.Itoa(x))
	case int64:
		sb.WriteString(strconv.FormatInt(x, 10))
	case int32:
		sb.WriteString(strconv.FormatInt(int64(x), 10))
	case uint64:
		sb.WriteString(strconv.FormatUint(x, 10))
	case float64:
		writeFloat(sb, x)
	case json.Number:
		sb.WriteString(x.String())
	case json.RawMessage:
		decoded, err := Decode(x)
		if err != nil {
			return err
		}
		return encode(sb, decoded, opts, depth)
	case *Obj:
		if x == nil {
			sb.WriteString("null")
			return nil
		}
		keys := x.Keys()
		if opts.SortKeys {
			slices.Sort(keys)
		}
		return writeObject(sb, keys, func(k string) any { return x.vals[k] }, opts, depth)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return writeObject(sb, keys, func(k string) any { return x[k] }, opts, depth)
	case map[string]string:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return writeObject(sb, keys, func(k string) any { return x[k] }, opts, depth)
	case map[string]int:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return writeObject(sb, keys, func(k string) any { return x[k] }, opts, depth)
	case []any:
		return writeArray(sb, len(x), func(i int) any { return x[i] }, opts, depth)
	case []string:
		return writeArray(sb, len(x), func(i int) any { return x[i] }, opts, depth)
	case []*Obj:
		return writeArray(sb, len(x), func(i int) any { return x[i] }, opts, depth)
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			if rv.Kind() == reflect.Slice && rv.IsNil() {
				sb.WriteString("[]")
				return nil
			}
			return writeArray(sb, rv.Len(), func(i int) any { return rv.Index(i).Interface() }, opts, depth)
		case reflect.Pointer:
			if rv.IsNil() {
				sb.WriteString("null")
				return nil
			}
		}
		// Anything else goes through encoding/json and back, so its key
		// order follows its own MarshalJSON or struct field order.
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		decoded, err := Decode(raw)
		if err != nil {
			return err
		}
		return encode(sb, decoded, opts, depth)
	}
	return nil
}

func writeObject(sb *strings.Builder, keys []string, get func(string) any, opts Options, depth int) error {
	if len(keys) == 0 {
		sb.WriteString("{}")
		return nil
	}
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		newline(sb, opts, depth+1)
		writeString(sb, k, opts.EnsureASCII)
		if opts.Indent > 0 {
			sb.WriteString(": ")
		} else {
			sb.WriteByte(':')
		}
		if err := encode(sb, get(k), opts, depth+1); err != nil {
			return err
		}
	}
	newline(sb, opts, depth)
	sb.WriteByte('}')
	return nil
}

func writeArray(sb *strings.Builder, n int, get func(int) any, opts Options, depth int) error {
	if n == 0 {
		sb.WriteString("[]")
		return nil
	}
	sb.WriteByte('[')
	for i := range n {
		if i > 0 {
			sb.WriteByte(',')
		}
		newline(sb, opts, depth+1)
		if err := encode(sb, get(i), opts, depth+1); err != nil {
			return err
		}
	}
	newline(sb, opts, depth)
	sb.WriteByte(']')
	return nil
}

func newline(sb *strings.Builder, opts Options, depth int) {
	if opts.Indent <= 0 {
		return
	}
	sb.WriteByte('\n')
	sb.WriteString(strings.Repeat(" ", opts.Indent*depth))
}

func writeFloat(sb *strings.Builder, f float64) {
	switch {
	case math.IsNaN(f):
		sb.WriteString("NaN")
	case math.IsInf(f, 1):
		sb.WriteString("Infinity")
	case math.IsInf(f, -1):
		sb.WriteString("-Infinity")
	case f == math.Trunc(f) && math.Abs(f) < 1e16:
		sb.WriteString(strconv.FormatFloat(f, 'f', 1, 64))
	default:
		sb.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
	}
}

const hexDigits = "0123456789abcdef"

func writeString(sb *strings.Builder, s string, ensureASCII bool) {
	sb.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			switch {
			case r < 0x20:
				writeU(sb, r)
			case r < 0x80 || !ensureASCII:
				sb.WriteRune(r)
			case r > 0xFFFF:
				hi, lo := utf16.EncodeRune(r)
				writeU(sb, hi)
				writeU(sb, lo)
			default:
				writeU(sb, r)
			}
		}
	}
	sb.WriteByte('"')
}

func writeU(sb *strings.Builder, r rune) {
	sb.WriteString(`\u`)
	for shift := 12; shift >= 0; shift -= 4 {
		sb.WriteByte(hexDigits[(r>>shift)&0xF])
	}
}

// Decode parses JSON into nil, bool, json.Number, string, []any and *Obj,
// keeping object key order.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
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
