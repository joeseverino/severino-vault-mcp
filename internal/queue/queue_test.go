package queue

import (
	"reflect"
	"slices"
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
		{ID: "b-3", Kind: "follow-up", Status: "open", Text: `Text with · dots, → arrows, {braces}, [brackets], (parens), a \ backslash and a trailing \`},
		{ID: "b-4", Kind: "next", Status: "open", Text: "ends in a field [kind:: decision]"},
		{ID: "b-5", Kind: "next", Status: "dropped", Text: "x (pr:: 1)", Resolution: "superseded by b-6", Links: map[string]string{"pr": "9"}},
		{ID: "b-6", Kind: "next", Status: "open", Text: "looks done ✅ 2026-10-22"},
		{ID: "b-7", Kind: "next", Status: "closed", Text: "Ticked in Obsidian", Extra: []string{"[note:: kept]", "[completion:: 2026-10-22]", "✅ 2026-10-22"}},
		{ID: "b-8", Kind: "next", Status: "open", Text: "Call f(x) (see [docs]) and std::vec (later)"},
		{ID: "b-9", Kind: "next", Status: "open", Text: "ends in parens (for now)"},
		{ID: "b-10", Kind: "next", Status: "open", Text: "note:: leading key [x (y) k:: v] ✅2026-10-22"},
		{ID: "b-11", Kind: "next", Status: "open", Text: "(a) [b]"},
		{ID: "b-12", Kind: "delegated", Status: "returned", Text: "Spans repos", Agent: "a1", Origin: "example-site",
			Repos: []string{"example-a", "example-b"}, Resolution: "example-a@abc1234"},
		{ID: "b-13", Kind: "next", Status: "open", Owner: "joe", Text: "Has notes", BodyLink: "Example Note (draft)"},
		{ID: "b-14", Kind: "next", Status: "open", Text: "about #queue/decision and #Owner/joe, #queue, #queued and #owner-x"},
		{ID: "b-15", Kind: "next", Status: "open", Text: "ends like a body ([[Other|notes]])", BodyLink: "projects/Example"},
		{ID: "b-16", Kind: "next", Status: "open", Text: "#queue/next at the start, a #plain tag at the end #later"},
		{ID: "b-17", Kind: "follow-up", Status: "open", Owner: "work_er-2", Text: "Owner with _ and -", Body: "project-x"},
		{ID: "b-18", Kind: "delegated", Status: "open", Owner: "worker", Agent: "a1", Text: "Worker's piece", Created: "2026-10-22",
			Base: "feat/store", Owns: []string{"internal/queue", "docs/Work Queue (draft).md"}, BlockedBy: []string{"b-17"},
			Extra: []string{"[due:: 2026-11-01]"}},
		{ID: "b-19", Kind: "next", Status: "closed", Text: "Filed and ticked", Created: "2026-10-01", Resolution: "done",
			Extra: []string{"[completion:: 2026-10-22]"}},
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

// Only what a parser would read as a field or a date is escaped; ordinary
// brackets and parentheses read in Obsidian as typed.
func TestTextEscapesOnlyWhatAParserWouldRead(t *testing.T) {
	for text, want := range map[string]string{
		"Call f(x) (see [docs])":   "Call f(x) (see [docs])",
		"ends in parens (for now)": "ends in parens (for now)",
		"std::vec (later)":         `std\::vec (later)`,
		"(see) std::vec":           "(see) std::vec",
		"a [kind:: decision]":      `a \[kind:: decision]`,
		"x (pr:: 1)":               `x \(pr:: 1)`,
		"[x (y) k:: v]":            `\[x (y) k:: v]`,
		"looks done ✅ 2026-10-22":  `looks done \✅ 2026-10-22`,
		"a ✅ mark":                 "a ✅ mark",
		`a \ backslash`:            `a \\ backslash`,
		"note:: a leading field":   `note\:: a leading field`,
	} {
		if got := Escape(text); got != want {
			t.Errorf("Escape(%q) = %q, want %q", text, got, want)
		}
		if Unescape(Escape(text)) != text {
			t.Errorf("%q does not unescape to itself", text)
		}
	}
}

// Inside a [key:: value] field only what ends it is escaped: ] and the
// backslash. Parentheses and [ stay as typed.
func TestAResolutionEscapesOnlyWhatEndsItsField(t *testing.T) {
	for resolution, want := range map[string]string{
		"merged (#213)":            "[resolution:: merged (#213)]",
		"landed (b668469b)":        "[resolution:: landed (b668469b)]",
		"see [docs (v2)":           "[resolution:: see [docs (v2)]",
		"a [k:: v] b":              `[resolution:: a [k:: v\] b]`,
		`ends in \`:                `[resolution:: ends in \\]`,
		"(paren at the start)":     "[resolution:: (paren at the start)]",
		"x) unbalanced ( and ] ok": `[resolution:: x) unbalanced ( and \] ok]`,
	} {
		it := Item{ID: "hq-1", Kind: "next", Status: "closed", Text: "t", Resolution: resolution}
		line := Format(it)
		if !strings.Contains(line, "  "+want+"  [id:: hq-1]") {
			t.Errorf("%q writes %s", resolution, line)
		}
		if got, ok := Parse(line); !ok || got.Resolution != resolution {
			t.Errorf("%q reads back as %q (%v)", resolution, got.Resolution, ok)
		}
	}
}

// A note written when every bracket and parenthesis in a value was escaped
// still reads as typed.
func TestAResolutionWithTheOldEscapingStillReads(t *testing.T) {
	line := `- [x] t  [kind:: next]  [resolution:: merged \(#213\) after \[review\]]  [id:: hq-1]`
	if it, ok := Parse(line); !ok || it.Resolution != "merged (#213) after [review]" {
		t.Fatalf("%+v %v", it, ok)
	}
}

func TestEveryStatusIsATasksCheckbox(t *testing.T) {
	want := map[string]string{"open": "- [ ] ", "returned": "- [/] ", "closed": "- [x] ", "dropped": "- [-] "}
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

// The parked status is gone: its [>] mark reads as open, so an older note
// never fails, and the line is written back with the open mark.
func TestARetiredParkedLineReadsAsOpen(t *testing.T) {
	line := "- [>] Someday #queue/next  [id:: hq-3]"
	it, ok := Parse(line)
	if !ok || it.Status != "open" || slices.Contains(Statuses, "parked") {
		t.Fatalf("%+v %v", it, ok)
	}
	if got := Format(it); got != "- [ ] Someday #queue/next  [id:: hq-3]" {
		t.Fatal(got)
	}
}

// The created date is Tasks' own field, after dependsOn where Tasks reads
// it; the emoji form a person may type reads as the same field.
func TestATranscriptPathRoundTrips(t *testing.T) {
	it := Item{ID: "hq-4", Kind: "delegated", Status: "open", Text: "Built", Transcript: "/Users/example/.claude/projects/p/agent-a1.jsonl"}
	line := Format(it)
	if !strings.Contains(line, "[transcript:: /Users/example/.claude/projects/p/agent-a1.jsonl]") {
		t.Fatal(line)
	}
	if got, ok := Parse(line); !ok || !reflect.DeepEqual(got, it) {
		t.Fatalf("%+v %v", got, ok)
	}
}

func TestTheCreatedDateIsTasksOwnField(t *testing.T) {
	it := Item{ID: "hq-4", Kind: "next", Status: "open", Text: "Filed", BlockedBy: []string{"hq-1"}, Created: "2026-10-22",
		Base: "main", Owns: []string{"a.go", "b/c.go"}}
	if got := Format(it); got != "- [ ] Filed #queue/next  [base:: main]  [owns:: a.go,b/c.go]  [id:: hq-4]  [dependsOn:: hq-1]  [created:: 2026-10-22]" {
		t.Fatal(got)
	}
	typed, ok := Parse("- [ ] Filed #queue/next  [base:: main]  [owns:: a.go, b/c.go]  [id:: hq-4]  [dependsOn:: hq-1]  ➕ 2026-10-22")
	if !ok || !reflect.DeepEqual(typed, it) {
		t.Fatalf("%+v %v", typed, ok)
	}
	for _, bad := range []Item{
		{ID: "hq-5", Kind: "next", Status: "open", Text: "x", Created: "22 Oct"},
		{ID: "hq-5", Kind: "next", Status: "open", Text: "x", Owns: []string{"a,b"}},
		{ID: "hq-5", Kind: "next", Status: "open", Text: "x", Owns: []string{"a]"}},
		{ID: "hq-5", Kind: "next", Status: "open", Text: "x", Base: "two words"},
		{ID: "hq-5", Kind: "next", Status: "open", Text: "x", Transcript: "relative/t.jsonl"},
		{ID: "hq-5", Kind: "next", Status: "open", Text: "x", Extra: []string{"➕ 2026-10-22"}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v validated", bad)
		}
	}
	if _, ok := Parse("- [ ] Twice #queue/next  [id:: hq-6]  [created:: 2026-10-22]  ➕ 2026-10-21"); ok {
		t.Error("a line with two created dates parsed")
	}
}

func TestTheLineReadsAsDocumented(t *testing.T) {
	it := Item{ID: "hq-17", Kind: "delegated", Status: "open", Owner: "worker", Text: "Build the store",
		Links: map[string]string{"branch": "pass/queue"}, BlockedBy: []string{"hq-12"}, Agent: "a1b2"}
	if got := Format(it); got != "- [ ] Build the store #queue/delegated #owner/worker  [agent:: a1b2]  [branch:: pass/queue]  [id:: hq-17]  [dependsOn:: hq-12]" {
		t.Fatal(got)
	}
	linked := it
	linked.BodyLink, linked.Body = "Store Design", "project-store"
	if got := Format(linked); !strings.HasPrefix(got, "- [ ] Build the store ([[Store Design|notes]]) #queue/delegated #owner/worker  [agent:: a1b2]") ||
		strings.Contains(got, "[body::") {
		t.Fatal(got)
	}
	withOrigin := it
	withOrigin.Origin, withOrigin.Repos = "example-site", []string{"example-a", "example-b"}
	if got := Format(withOrigin); !strings.Contains(got, "[agent:: a1b2]  [origin:: example-site]  [repos:: example-a,example-b]  [branch:: pass/queue]") {
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

// A tag in the queue's namespaces, in any case, is escaped in the text so a
// Tasks query for a kind or owner never matches an item's words, and so is a
// trailing link shaped like the body link; other tags and links, and words
// that only start like a namespace, stay as typed.
func TestTextTagsInTheQueueNamespacesAreEscaped(t *testing.T) {
	for text, want := range map[string]string{
		"see #queue/decision":        `see \#queue/decision`,
		"#owner/joe first":           `\#owner/joe first`,
		"a #QUEUE/Next b":            `a \#QUEUE/Next b`,
		"the #queue parent":          `the \#queue parent`,
		"a #queued #owners #queue_x": "a #queued #owners #queue_x",
		"x#queue/next":               "x#queue/next",
		"a #urgent tag":              "a #urgent tag",
		"ends ([[Note|notes]])":      `ends \([[Note|notes]])`,
		"a ([[Note|other]])":         "a ([[Note|other]])",
		"a [[Note|notes]] mid":       "a [[Note|notes]] mid",
	} {
		if got := Escape(text); got != want {
			t.Errorf("Escape(%q) = %q, want %q", text, got, want)
		}
		it := Item{ID: "hq-1", Kind: "next", Status: "open", Text: text}
		if got, ok := Parse(Format(it)); !ok || got.Kind != "next" || got.Owner != "" || got.BodyLink != "" || got.Text != text {
			t.Errorf("%q reads back as %+v (%v)", text, got, ok)
		}
	}
}

// Lines written before kind and owner were tags still read, and the next
// write gives them the current form.
func TestTheOlderFieldFormStillReads(t *testing.T) {
	for line, want := range map[string]string{
		"- [ ] Old  [kind:: delegated]  [owner:: worker]  [agent:: a1]  [id:: hq-1]  [dependsOn:: hq-0]":         "- [ ] Old #queue/delegated #owner/worker  [agent:: a1]  [id:: hq-1]  [dependsOn:: hq-0]",
		"- [x] Old  [kind:: next]  [body:: project-x]  [alias:: task-x]  [id:: hq-2]  [completion:: 2026-10-22]": "- [x] Old #queue/next  [body:: project-x]  [alias:: task-x]  [id:: hq-2]  [completion:: 2026-10-22]",
		"- [ ] Mentions #owner/x  [kind:: next]  [id:: hq-3]":                                                    `- [ ] Mentions \#owner/x #queue/next  [id:: hq-3]`,
	} {
		it, ok := Parse(line)
		if !ok {
			t.Fatalf("%q does not parse", line)
		}
		if got := Format(it); got != want {
			t.Errorf("%q rewrites as\n%s\nwant\n%s", line, got, want)
		}
	}
}

// Tasks moves the tags it finds among the fields at the end of a line to the
// end of the description when it rewrites the line; a hand edit may leave
// them anywhere in that run. Every placement reads the same.
func TestTagsReadWhereverTasksOrAHumanLeavesThem(t *testing.T) {
	want := Item{ID: "hq-17", Kind: "decision", Status: "closed", Owner: "joe", Text: "Ship it?", BlockedBy: []string{"hq-12"},
		Extra: []string{"[completion:: 2026-10-22]"}}
	for _, line := range []string{
		"- [x] Ship it? #queue/decision #owner/joe  [id:: hq-17]  [dependsOn:: hq-12]  [completion:: 2026-10-22]",
		"- [x] Ship it? #queue/decision #owner/joe [id:: hq-17] [dependsOn:: hq-12] [completion:: 2026-10-22]",
		"- [x] Ship it?  [id:: hq-17]  [dependsOn:: hq-12] #owner/joe #queue/decision  [completion:: 2026-10-22]",
		"- [x] Ship it?  [id:: hq-17]  [dependsOn:: hq-12]  [completion:: 2026-10-22] #queue/decision #owner/joe",
	} {
		if it, ok := Parse(line); !ok || !reflect.DeepEqual(it, want) {
			t.Errorf("%q parsed as %+v (%v)", line, it, ok)
		}
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
		"- [ ] two kind tags #queue/next #queue/decision  [id:: hq-1]",
		"- [ ] two owners #queue/next #owner/a #owner/b  [id:: hq-1]",
		"- [ ] owner both ways #queue/next #owner/a  [owner:: a]  [id:: hq-1]",
		"- [ ] unknown kind tag #queue/nope  [id:: hq-1]",
		"- [ ] empty kind tag #queue/  [id:: hq-1]",
		"- [ ] owner with a dot #queue/next #owner/a.b  [id:: hq-1]",
		"- [ ] escaped kind tag \\#queue/next  [id:: hq-1]",
		"- [ ] body both ways ([[Note|notes]]) #queue/next  [body:: project-x]  [id:: hq-1]",
		"- [ ] #queue/next  [id:: hq-1]",
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
	for _, s := range []string{"a", `\`, "→", "{x}", "a · b", `\→`, "[k:: v]", "(k:: v)", "x ✅ 2026-10-22", `a\]`, "f(x) (y)", "[a] (b)", "k:: v", "[x (y) k:: v]", "x ✅2026-10-22", "merged (#213)", "landed (b668469b)", "a [k:: v", "a [k:: v] b",
		"#queue/next", "a #owner/x", "#Queue", "([[n|notes]])", "a ([[n|notes]])", "#queue/next  [id:: x-1]"} {
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

// Text, owner and body link together: whatever validates comes back as given.
func FuzzTagsAndLinkRoundTrip(f *testing.F) {
	for _, s := range [][3]string{{"a #queue/next", "joe", "Note"}, {"x ([[n|notes]])", "w-1", "dir/Note (v2)"}, {"#owner/x", "ü_2", "a b"}} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, text, owner, link string) {
		it := Item{ID: "f-1", Kind: "decision", Status: "open", Text: text, Owner: owner, BodyLink: link}
		if it.Validate() != nil {
			return
		}
		got, ok := Parse(Format(it))
		if !ok || !reflect.DeepEqual(got, it) {
			t.Fatalf("%+v came back as %+v (%v): %s", it, got, ok, Format(it))
		}
	})
}

// Every way a line writes an id counts toward the next one, whether or not
// the line parses; a dependsOn reference is not an id the line carries.
func TestTopCountsEveryIDFieldInTheText(t *testing.T) {
	text := "- [ ] a  [id:: ex-2]\n- [ ] b (id:: ex-7)\n- [ ] c 🆔 ex-4\n- [ ] d  [dependsOn:: ex-9]\n- [ ] e  [id:: other-30]"
	if got := Top(text, "ex"); got != 7 {
		t.Fatal(got)
	}
	if got := IDsIn("- [ ] {kind=next}  [id:: ex-2]  [dependsOn:: ex-1]"); !reflect.DeepEqual(got, []string{"ex-2"}) {
		t.Fatal(got)
	}
}

func TestOnlyAKnownQueueFormatIsWritable(t *testing.T) {
	for v, ok := range map[string]bool{"": true, "1": true, "2": true, "3": true, "4": false, "0": false, "one": false} {
		if err := CheckVersion(v); (err == nil) != ok {
			t.Errorf("%q: %v", v, err)
		}
	}
}

func TestAnItemShapedLineNeedNotParse(t *testing.T) {
	for line, want := range map[string]bool{"- [ ] x": true, "- [?] {kind=next}": true, "- [x]": true, "- [] x": false, "  - [ ] nested": false, "- [ ]x": false, "* [ ] x": false} {
		if ItemShaped(line) != want {
			t.Errorf("%q", line)
		}
	}
}
