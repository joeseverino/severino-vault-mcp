package jsonx

import (
	"strings"
	"testing"
)

func TestCompactKeepsInsertionOrder(t *testing.T) {
	if got := Compact(New("b", 1, "a", 2)); got != `{"b":1,"a":2}` {
		t.Fatal(got)
	}
}

func TestPrettyIndentsLikePython(t *testing.T) {
	got := Pretty(New("a", 1, "b", []any{1, 2}, "c", New(), "d", []any{}))
	want := "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ],\n  \"c\": {},\n  \"d\": []\n}"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestCanonicalSortsKeys(t *testing.T) {
	got := Canonical(New("b", 1, "a", 2))
	if strings.Index(got, `"a"`) > strings.Index(got, `"b"`) || !strings.Contains(got, "\n") {
		t.Fatal(got)
	}
}

func TestEnsureASCIIEscapesLikePython(t *testing.T) {
	if got := Compact("— é 😀 <&> \u0001 \"q\" \\"); got != `"\u2014 \u00e9 \ud83d\ude00 <&> \u0001 \"q\" \\"` {
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
