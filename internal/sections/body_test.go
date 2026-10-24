package sections

import "testing"

func TestBodyAppendTo(t *testing.T) {
	cases := []struct{ name, in, out string }{
		{"into an empty section", "# Q\n\n## Open\n\n## Closed\n", "# Q\n\n## Open\n\n- a\n\n## Closed\n"},
		{"after the last line", "# Q\n\n## Open\n\n- x\n\n## Closed\n", "# Q\n\n## Open\n\n- x\n- a\n\n## Closed\n"},
		{"last section keeps the trailing newline", "# Q\n\n## Open\n\n- x\n", "# Q\n\n## Open\n\n- x\n- a\n"},
		{"stops at a subheading", "## Open\n\n- x\n\n### Later\n- y\n", "## Open\n\n- x\n- a\n\n### Later\n- y\n"},
		{"creates a missing section", "# Q\n\n- x\n\n\n", "# Q\n\n- x\n\n## Open\n\n- a\n"},
		{"skips a fenced heading", "## Open\n\n```\n## Open\n```\n", "## Open\n\n```\n## Open\n```\n- a\n"},
	}
	for _, c := range cases {
		b := NewBody(c.in)
		b.AppendTo(2, "Open", "- a")
		if got := b.String(); got != c.out {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.out)
		}
	}
}

func TestBodyRemoveDropsADoubledBlank(t *testing.T) {
	b := NewBody("## Open\n\n- x\n\n## Closed\n")
	b.Remove(2)
	if got := b.String(); got != "## Open\n\n## Closed\n" {
		t.Fatalf("%q", got)
	}
}

func TestBodyRanges(t *testing.T) {
	b := NewBody("intro\n## A\na\n### A1\nx\n## B\nb")
	h, ok := b.Find(2, "A")
	if !ok || h.Line != 1 {
		t.Fatal(h, ok)
	}
	if got := b.NextHeading(h.Line); got != 3 {
		t.Errorf("NextHeading = %d, want 3 (any level)", got)
	}
	if u, ok := b.Under(4); !ok || u.Text != "A1" {
		t.Errorf("Under(4) = %v %v", u, ok)
	}
	if _, ok := b.Under(0); ok {
		t.Error("the preamble is under no heading")
	}
	if b2, _ := b.Find(2, "B"); b.NextHeading(b2.Line) != len(b.Lines) {
		t.Error("the last section runs to the end")
	}
	if b.String() != "intro\n## A\na\n### A1\nx\n## B\nb" {
		t.Error("String does not round-trip")
	}
}
