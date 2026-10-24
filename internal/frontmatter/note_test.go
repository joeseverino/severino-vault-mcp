package frontmatter

import "testing"

// A Note carries Split's view and the body's exact bytes from one read.
func TestNewNoteAgreesWithSplit(t *testing.T) {
	for _, text := range []string{"---\ndoc_id: a\n---\n\n# A\nbody\n", "no block\n", ""} {
		n := NewNote(text)
		fm, body, start := Split(text)
		if n.Body != body || n.StartLine != start || (n.FM == nil) != (fm == nil) {
			t.Errorf("%q: note %+v disagrees with Split", text, n)
		}
		if n.Text[n.Offset:] != text[BodyOffset(text):] {
			t.Errorf("%q: offset %d", text, n.Offset)
		}
	}
}
