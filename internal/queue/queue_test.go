package queue

import (
	"reflect"
	"strings"
	"testing"
)

func TestEveryItemRoundTripsThroughItsLine(t *testing.T) {
	items := []Item{
		{ID: "hq-1", Kind: "next", Status: "open", Text: "Plain text"},
		{ID: "hq-2", Kind: "delegated", Status: "returned", Owner: "worker", Text: "Build the store",
			Links:     map[string]string{"branch": "pass/queue", "commit": "abc1234,def5678", "pr": "https://example.com/pull/7", "slug": "store"},
			BlockedBy: []string{"hq-1", "other-project-4"}, Body: "note-hq-2", Alias: "task-old-slug", Agent: "a1b2c3"},
		{ID: "example-site-12", Kind: "decision", Status: "closed", Owner: "main", Text: "Ship it?", Resolution: "yes [after review] (Friday) · noted \\"},
		{ID: "b-3", Kind: "follow-up", Status: "parked", Text: `Text with · dots, → arrows, {braces}, [brackets], (parens), a \ backslash and a trailing \`},
		{ID: "b-4", Kind: "next", Status: "open", Text: "ends in a field [kind:: decision]"},
		{ID: "b-5", Kind: "next", Status: "dropped", Text: "x (pr:: 1)", Resolution: "superseded by b-6", Links: map[string]string{"pr": "9"}},
		{ID: "b-6", Kind: "next", Status: "open", Text: "looks done ✅ 2026-10-22"},
		{ID: "b-7", Kind: "next", Status: "closed", Text: "Ticked in Obsidian", Extra: []string{"[note:: kept]", "[completion:: 2026-10-22]", "✅ 2026-10-22"}},
	}
	for _, it := range items {
		if err := it.Validate(); err != nil {
			t.Fatalf("%s: %v", it.ID, err)
		}
		line := Format(it)
		got, ok := Parse(line)
		if !ok || !reflect.DeepEqual(got, it) {
			t.Errorf("round trip of %s:\nline %s\ngot  %+v\nwant %+v", it.ID, line, got, it)
		}
		if Format(got) != line {
			t.Errorf("format(parse(line)) changed %s", line)
		}
	}
}

func TestEveryStatusIsATasksCheckbox(t *testing.T) {
	want := map[string]string{"open": "- [ ] ", "returned": "- [/] ", "parked": "- [>] ", "closed": "- [x] ", "dropped": "- [-] "}
	for _, st := range Statuses {
		it := Item{ID: "hq-1", Kind: "delegated", Status: st, Text: "t"}
		if !strings.HasPrefix(Format(it), want[st]) {
			t.Errorf("%s writes %q", st, Format(it))
		}
		if got, ok := Parse(Format(it)); !ok || got.Status != st {
			t.Errorf("%s reads back as %+v", st, got)
		}
	}
}

func TestTheLineReadsAsDocumented(t *testing.T) {
	it := Item{ID: "hq-17", Kind: "delegated", Status: "open", Owner: "worker", Text: "Build the store",
		Links: map[string]string{"branch": "pass/queue"}, BlockedBy: []string{"hq-12"}, Agent: "a1b2"}
	if got := Format(it); got != "- [ ] Build the store  [kind:: delegated]  [owner:: worker]  [agent:: a1b2]  [branch:: pass/queue]  [id:: hq-17]  [dependsOn:: hq-12]" {
		t.Fatal(got)
	}
	it.Status, it.Resolution = "closed", "example-repo@abc1234"
	if got := Format(it); !strings.HasPrefix(got, "- [x] ") || !strings.HasSuffix(got, "[resolution:: example-repo@abc1234]  [id:: hq-17]  [dependsOn:: hq-12]") {
		t.Fatal(got)
	}
}

// What the Tasks plugin (Dataview format or its default emoji format), Dataview's
// completion tracking, or a plain Obsidian click writes when a box is ticked.
func TestABoxTickedInObsidianIsClosedAndKeepsItsDate(t *testing.T) {
	open := Format(Item{ID: "hq-17", Kind: "next", Status: "open", Owner: "worker", Text: "Build it", BlockedBy: []string{"hq-12"}})
	ticked := strings.Replace(open, "- [ ] ", "- [x] ", 1)
	for _, tc := range []struct{ line, extra string }{
		{ticked + "  [completion:: 2026-10-22]", "[completion:: 2026-10-22]"},
		{ticked + " ✅ 2026-10-22", "✅ 2026-10-22"},
		{ticked + " [completion:: 2026-10-22]", "[completion:: 2026-10-22]"},
		{strings.Replace(open, "- [ ] ", "- [-] ", 1) + "  [cancelled:: 2026-10-22]", "[cancelled:: 2026-10-22]"},
		{ticked, ""},
	} {
		it, ok := Parse(tc.line)
		if !ok || !Done(it.Status) || it.ID != "hq-17" || it.Owner != "worker" || !reflect.DeepEqual(it.BlockedBy, []string{"hq-12"}) {
			t.Fatalf("%q parsed as %+v (%v)", tc.line, it, ok)
		}
		if tc.extra != "" && !reflect.DeepEqual(it.Extra, []string{tc.extra}) {
			t.Errorf("%q kept %q", tc.line, it.Extra)
		}
		if tc.extra != "" && !strings.HasSuffix(Format(it), "[dependsOn:: hq-12]  "+tc.extra) {
			t.Errorf("rewriting dropped the date: %s", Format(it))
		}
		it.Reopen("open")
		if it.Extra != nil || Format(it) != open {
			t.Errorf("reopened %q as %s", tc.line, Format(it))
		}
	}
	// A field a human adds by hand stays ahead of id, where Tasks still reads it.
	it, ok := Parse(strings.Replace(open, "[id::", "[note:: mine]  [id::", 1) + "  [due:: 2026-11-01]")
	if !ok || !reflect.DeepEqual(it.Extra, []string{"[note:: mine]", "[due:: 2026-11-01]"}) {
		t.Fatalf("%+v %v", it, ok)
	}
	if got := Format(it); !strings.Contains(got, "[note:: mine]  [id:: hq-17]  [dependsOn:: hq-12]  [due:: 2026-11-01]") {
		t.Fatal(got)
	}
}

func TestLinesOutsideTheGrammarAreNotItems(t *testing.T) {
	for _, line := range []string{
		"",
		"- a plain bullet",
		"- [ ] a plain task",
		"- [ ]   [kind:: next]  [id:: hq-1]",
		"- [ ] no id  [kind:: next]",
		"- [ ] no kind  [id:: hq-1]",
		"- [ ] unknown kind  [kind:: nope]  [id:: hq-1]",
		"- [?] unknown status  [kind:: next]  [id:: hq-1]",
		"- [/] returned but not delegated  [kind:: next]  [id:: hq-1]",
		"- [ ] open with a resolution  [kind:: next]  [resolution:: yes]  [id:: hq-1]",
		"- [ ] no number  [kind:: next]  [id:: hq]",
		"- [ ] repeated  [kind:: next]  [pr:: 1]  [pr:: 2]  [id:: hq-1]",
		"- [ ] empty owner  [kind:: next]  [owner:: ]  [id:: hq-1]",
		"- [ ] fields hidden behind text  [kind:: next]  [id:: hq-1] trailing words",
	} {
		if it, ok := Parse(line); ok {
			t.Errorf("%q parsed as %+v", line, it)
		}
	}
}

func TestIDsSplitAtTheLastDash(t *testing.T) {
	if p, n, ok := SplitID("example-site-12"); !ok || p != "example-site" || n != 12 {
		t.Fatal(p, n, ok)
	}
	if _, _, ok := SplitID("task-old-slug"); ok {
		t.Fatal("an alias is not an id")
	}
}

func FuzzTextRoundTrips(f *testing.F) {
	for _, s := range []string{"a", `\`, "→", "{x}", "a · b", `\→`, "[k:: v]", "(k:: v)", "x ✅ 2026-10-22", `a\]`} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, text, resolution string) {
		it := Item{ID: "f-1", Kind: "next", Status: "closed", Text: text, Resolution: resolution}
		if it.Validate() != nil {
			return
		}
		got, ok := Parse(Format(it))
		if !ok || got.Text != text || got.Resolution != resolution {
			t.Fatalf("%q, %q came back as %q, %q (%v)", text, resolution, got.Text, got.Resolution, ok)
		}
	})
}
