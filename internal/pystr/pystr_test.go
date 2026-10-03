package pystr

import (
	"slices"
	"testing"
)

func TestReprMatchesPython(t *testing.T) {
	for in, want := range map[string]string{"plain": `'plain'`, "it's": `"it's"`, `both ' and "`: `'both \' and "'`, "tab\there": `'tab\there'`} {
		if got := Repr(in); got != want {
			t.Errorf("%q: %s", in, got)
		}
	}
	if got := ReprList([]string{"01 Projects", "x"}); got != `['01 Projects', 'x']` {
		t.Fatal(got)
	}
}

func TestSplitLinesMatchesPython(t *testing.T) {
	if got := SplitLines("a\r\nb\rc\n\nd\n"); !slices.Equal(got, []string{"a", "b", "c", "", "d"}) {
		t.Fatal(got)
	}
	if got := SplitLines(""); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestStrAndTruthy(t *testing.T) {
	if Str(true) != "True" || Str(nil) != "None" || Str([]any{"a"}) != "['a']" || Truthy("") || !Truthy([]any{nil}) || Truthy([]any{}) {
		t.Fatal("str/truthy")
	}
}

func TestDecodeUsesUniversalNewlines(t *testing.T) {
	if Decode([]byte("a\r\nb\rc\xff")) != "a\nb\nc�" {
		t.Fatal(Decode([]byte("a\r\nb\rc\xff")))
	}
}
