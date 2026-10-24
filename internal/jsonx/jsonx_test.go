package jsonx

import "testing"

func TestCompactKeepsInsertionOrder(t *testing.T) {
	if got := Compact(New("b", 1, "a", 2)); got != `{"b":1,"a":2}` {
		t.Fatal(got)
	}
}

func TestPrettyIndentsByTwo(t *testing.T) {
	got := Pretty(New("a", 1, "b", []any{1, 2}, "c", New(), "d", []any{}))
	want := "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ],\n  \"c\": {},\n  \"d\": []\n}"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestSortedSortsKeysAtEveryLevel(t *testing.T) {
	got := Compact(Sorted(New("b", 1, "a", New("z", []any{New("y", 1, "x", 2)}, "m", 3))))
	if got != `{"a":{"m":3,"z":[{"x":2,"y":1}]},"b":1}` {
		t.Fatal(got)
	}
}

func TestStringsKeepTheirText(t *testing.T) {
	if got := Compact(New("s", "— é 😀 <&> \u0001 \"q\" \\")); got != `{"s":"— é 😀 <&> \u0001 \"q\" \\"}` {
		t.Fatal(got)
	}
}

func TestANilListIsEmpty(t *testing.T) {
	var none []string
	var objs []*Obj
	if got := Compact(New("a", none, "b", []any{objs}, "c", (*Obj)(nil))); got != `{"a":[],"b":[[]],"c":null}` {
		t.Fatal(got)
	}
}

func TestDecodeKeepsOrderAndRoundTrips(t *testing.T) {
	v, err := Decode([]byte(`{"z":1,"a":[true,null,"x"],"m":{"k":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Compact(v); got != `{"z":1,"a":[true,null,"x"],"m":{"k":2}}` {
		t.Fatal(got)
	}
	if _, err := Decode([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMergeAndDelete(t *testing.T) {
	o := New("a", 1, "b", 2).Merge(New("b", 3, "c", 4))
	o.Delete("a")
	if got := Compact(o); got != `{"b":3,"c":4}` {
		t.Fatal(got)
	}
}
