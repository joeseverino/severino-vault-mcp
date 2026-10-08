package frontmatter

import (
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/fuzzseed"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

func TestParseScalarsListsAndBlocks(t *testing.T) {
	fm := ParseBlock(`doc_id: rb-x
title: "Quoted: yes"
flag: true
off: false
empty:
nothing: null
inline: [a, "b", c]
block:
  - one
  - two
notes: >-
  First line.
  Second line.
literal: |
  keep
  lines
notice: 30
# a comment
`)
	got := jsonx.Compact(fm)
	want := `{"doc_id":"rb-x","title":"Quoted: yes","flag":true,"off":false,"empty":[],"nothing":null,"inline":["a","b","c"],` +
		`"block":["one","two"],"notes":"First line. Second line.","literal":"keep\nlines","notice":"30"}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestQuotedTrueIsStillABool(t *testing.T) {
	if v := Scalar(`"true"`); v != true {
		t.Fatalf("%#v", v)
	}
}

func TestSplitReturnsBodyAndStartLine(t *testing.T) {
	fm, body, start := Split("---\na: b\n---\n\n# Title\n\ntext\n")
	if fm.Str("a") != "b" || body != "# Title\n\ntext" || start != 4 {
		t.Fatalf("%v %q %d", jsonx.Compact(fm), body, start)
	}
	if fm, body, start := Split("# no frontmatter\n"); fm != nil || body != "# no frontmatter\n" || start != 1 {
		t.Fatal("no-frontmatter split")
	}
	if fm, _, _ := Split("---\nunclosed: yes\n"); fm != nil {
		t.Fatal("unclosed block parsed")
	}
}

func TestSerializeOrdersAndEscapes(t *testing.T) {
	got := Serialize(jsonx.New("extra", "x", "title", `Driver: "A"`, "doc_id", "renewal-quoted", "tags", []any{}, "flag", true, "gone", nil))
	want := "---\ndoc_id: renewal-quoted\ntitle: \"Driver: \\\"A\\\"\"\ntags: []\nextra: x\nflag: true\ngone: null\n---\n\n"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestEscapeRules(t *testing.T) {
	for in, want := range map[string]string{"": `""`, "plain": "plain", "a: b": `"a: b"`, " pad": `" pad"`, "#tag": `"#tag"`} {
		if got := Escape(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestBodyOffset(t *testing.T) {
	text := "---\na: b\n---\n\n\nbody\n"
	if off := BodyOffset(text); text[off:] != "body\n" {
		t.Fatalf("%q", text[off:])
	}
	if BodyOffset("no block\n") != 0 {
		t.Fatal("offset without a block")
	}
}

func FuzzSplit(f *testing.F) {
	for _, text := range fuzzseed.Markdown(f) {
		f.Add(text)
	}
	f.Add("---\ndoc_id: x\ntags: [a, \"b\"]\nnotes: >-\n  one\n  two\n---\nbody\n")
	f.Add("---\n---\n")
	f.Add("[")
	f.Fuzz(func(t *testing.T, text string) {
		fm, _, line := Split(text)
		if line < 1 {
			t.Fatalf("body line %d", line)
		}
		if fm != nil {
			_ = Serialize(fm)
		}
		if off := BodyOffset(text); off < 0 || off > len(text) {
			t.Fatalf("BodyOffset %d outside 0..%d", off, len(text))
		}
		_ = ParseBlock(text)
	})
}

func TestParseBlockSurvivesAnEmptyListItem(t *testing.T) {
	for block, want := range map[string]string{
		"A:\n - ":       `{"A":[null]}`,
		"A:\n - \u00a0": `{"A":[null]}`,
		"A:\n -":        `{"A":[]}`,
	} {
		if got := jsonx.Compact(ParseBlock(block)); got != want {
			t.Errorf("%q parsed as %s, want %s", block, got, want)
		}
	}
}
