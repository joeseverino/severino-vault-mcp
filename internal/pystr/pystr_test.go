package pystr

import (
	"slices"
	"testing"
)

func TestSplitLinesMatchesPython(t *testing.T) {
	if got := SplitLines("a\r\nb\rc\n\nd\n"); !slices.Equal(got, []string{"a", "b", "c", "", "d"}) {
		t.Fatal(got)
	}
	if got := SplitLines(""); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestStrAndTruthy(t *testing.T) {
	if Str(true) != "True" || Str(nil) != "None" || Str([]any{"a", true}) != `"a", "True"` || Truthy("") || !Truthy([]any{nil}) || Truthy([]any{}) {
		t.Fatal("str/truthy")
	}
}

func TestDecodeUsesUniversalNewlines(t *testing.T) {
	if Decode([]byte("a\r\nb\rc\xff")) != "a\nb\nc�" {
		t.Fatal(Decode([]byte("a\r\nb\rc\xff")))
	}
}

func TestQuotedNamesAListWithCommas(t *testing.T) {
	if got := Quoted([]string{"01 Projects", `say "hi"`}); got != `"01 Projects", "say \"hi\""` {
		t.Fatal(got)
	}
	if got := Quoted(nil); got != "" {
		t.Fatal(got)
	}
}
