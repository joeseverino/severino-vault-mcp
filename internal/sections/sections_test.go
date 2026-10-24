package sections

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/fuzzseed"
)

const multi = `Overview line before any heading.

## Routine operations

Run the daily job to keep things current.

### Backing commands

Use ` + "`./backup nightly`" + ` to snapshot the data.

## Troubleshooting

Check the resolver logs first when latency spikes.
`

func slugs(secs []Section) []string {
	var out []string
	for _, s := range secs {
		out = append(out, s.Slug)
	}
	return out
}

func TestSplitsAtH2AndKeepsH3Inside(t *testing.T) {
	secs := Parse(multi, 5)
	if !slices.Equal(slugs(secs), []string{"overview", "routine-operations", "troubleshooting"}) {
		t.Fatal(slugs(secs))
	}
	if secs[1].Level != 2 || !strings.Contains(secs[1].Body, "Backing commands") || secs[1].StartLine != 7 {
		t.Fatalf("%+v", secs[1])
	}
}

func TestDisambiguatesDuplicateHeadings(t *testing.T) {
	if got := slugs(Parse("## Notes\n\nfirst\n\n## Notes\n\nsecond\n", 1)); !slices.Equal(got, []string{"notes", "notes-2"}) {
		t.Fatal(got)
	}
}

func TestIgnoresHeadingsInsideCodeFences(t *testing.T) {
	if got := slugs(Parse("## Real\n\n```sh\n## not a heading\necho hi\n```\n\nstill real\n", 1)); !slices.Equal(got, []string{"real"}) {
		t.Fatal(got)
	}
}

func TestSubsplitsOversizedH2AtH3(t *testing.T) {
	filler := strings.Repeat(strings.Repeat("word ", 60)+"\n", 3)
	secs := ParseCap("## Big\n\nlead\n\n### One\n\n"+filler+"\n### Two\n\n"+filler, 1, 40)
	var paths []string
	for _, s := range secs {
		paths = append(paths, s.HeadingPath)
		if s.Level != 2 && s.Level != 3 {
			t.Fatalf("level %d", s.Level)
		}
	}
	if !slices.ContainsFunc(paths, func(p string) bool { return strings.HasPrefix(p, "Big > One") }) ||
		!slices.ContainsFunc(paths, func(p string) bool { return strings.HasPrefix(p, "Big > Two") }) {
		t.Fatal(paths)
	}
}

// addressed is a body with every form a read can name: the text before the
// first heading, an H2 split at its H3s, a hard-wrapped H2 and a heading
// text two sections share.
func addressed() string {
	filler := strings.Repeat("Steady words fill this paragraph for the cap. ", 22)
	long := strings.Repeat("word ", 400) + "\n"
	return "Lead line.\n\n## Routine operations\n\nlead\n\n### Backing commands\n\n" + filler +
		"\n\n### Restore\n\n" + filler + "\n\n## Troubleshooting\n\nresolver logs\n\n## Big\n\n" + long + long +
		"\n## A\n\n### Shape\n\none\n\n## B\n\n### Shape\n\ntwo"
}

func TestAddressNamesASectionEveryWayAReaderWrites(t *testing.T) {
	body := addressed()
	out := Outline(body)
	byName := map[string]Target{}
	for _, e := range out {
		byName[e.Section.Slug] = e.Target
	}
	if out[0].Section.Level != 0 || out[0].Target.Line != -1 || !strings.HasPrefix(out[0].Target.Span(body), "Lead line.") {
		t.Fatalf("the outline opens with the lead: %+v", out[0])
	}
	routine := Targets(body, "Routine operations")[0]
	var parts []string
	for _, e := range out {
		if e.Section.Part > 0 {
			parts = append(parts, e.Section.Slug)
		}
	}
	for ref, want := range map[string]Target{
		"overview":                              out[0].Target,
		"Troubleshooting":                       byName["troubleshooting"],
		"troubleshooting":                       byName["troubleshooting"],
		"  TROUBLESHOOTING ":                    byName["troubleshooting"],
		"Routine operations > Backing commands": byName["routine-operations-backing-commands"],
		"routine-operations-restore":            byName["routine-operations-restore"],
		"trouble":                               byName["troubleshooting"],
		"routine-operations-back":               byName["routine-operations-backing-commands"],
		"routine":                               routine,
		"Routine Op":                            routine,
		"1":                                     out[0].Target,
		"3":                                     out[2].Target,
		strconv.Itoa(len(out)):                  out[len(out)-1].Target,
		"big-part-2":                            byName["big-part-2"],
		"Big (part 1)":                          byName["big-part-1"],
		"Big":                                   Targets(body, "Big")[0],
		"b-shape":                               Targets(body, "B > Shape")[0],
	} {
		got := Address(body, ref)
		if len(got) != 1 || got[0] != want {
			t.Errorf("Address(%q) = %+v, want %+v", ref, got, want)
		}
	}
	if len(parts) < 2 {
		t.Fatalf("Big does not wrap: %v", parts)
	}
	if routine.Start != out[1].Target.Start || routine.End != out[3].Target.End || byName["routine-operations"] != routine {
		t.Fatalf("an H2 split at its H3s reads as its whole block: %+v", routine)
	}
	for _, ref := range []string{"0", "99", "no-such-section", "", "  ", "-1"} {
		if got := Address(body, ref); len(got) != 0 {
			t.Errorf("Address(%q) = %+v, want nothing", ref, got)
		}
	}
	for ref, want := range map[string][]string{"Shape": {"a-shape", "b-shape"}, "big-p": parts} {
		var names []string
		for _, t := range Address(body, ref) {
			names = append(names, t.Name())
		}
		if !slices.Equal(names, want) {
			t.Errorf("Address(%q) names %v, want the ambiguous %v", ref, names, want)
		}
	}
}

func TestAddressPrefersAHeadingNamedAsANumberToThePosition(t *testing.T) {
	body := "## One\n\na\n\n## 3\n\nthree\n\n## Two\n\nb"
	if got := Address(body, "3"); len(got) != 1 || got[0].Text != "3" {
		t.Fatalf("%+v", got)
	}
	if got := Address(body, "2"); len(got) != 1 || got[0].Text != "3" {
		t.Fatalf("position 2 is the second outline entry: %+v", got)
	}
}

func TestSummaryIsTheFirstSentenceCapped(t *testing.T) {
	secs := Parse(multi, 1)
	if got := Summary(secs[2]); got != "Check the resolver logs first when latency spikes." {
		t.Fatal(got)
	}
	long := Parse("## L\n\n"+strings.Repeat("word ", 60)+"\n", 1)
	if got := Summary(long[0]); !strings.HasSuffix(got, "…") || len([]rune(got)) != 120 {
		t.Fatalf("%d %q", len([]rune(got)), got)
	}
}

func FuzzParseCap(f *testing.F) {
	for _, text := range fuzzseed.Markdown(f) {
		f.Add(text, 1, 800)
	}
	f.Add(multi, 5, 20)
	f.Add("## a\n### b\n```\n## not a heading\n```\n", 1, 1)
	f.Fuzz(func(t *testing.T, body string, startLine, tokenCap int) {
		tokenCap = 1 + (tokenCap%4000+4000)%4000
		secs := ParseCap(body, startLine, tokenCap)
		for _, s := range secs {
			_ = Summary(s)
		}
	})
}

// Every slug in the server comes from SlugifyWith: section anchors, doctor's
// proposed doc_id, queue id prefixes (plain) and task file names (Unicode,
// capped at 60).
func TestSlugify(t *testing.T) {
	long := strings.Repeat("ab ", 30)
	cases := []struct{ in, plain, task string }{
		{"Example Project (2)", "example-project-2", "example-project-2"},
		{"  --Hello,  World!--  ", "hello-world", "hello-world"},
		{"Café über naïve", "caf-ber-na-ve", "café-über-naïve"},
		{"日本語 title", "title", "日本語-title"},
		{"", "", ""},
		{"!!!", "", ""},
		{"a_b-c.d", "a-b-c-d", "a-b-c-d"},
		{"x\xffy", "x-y", "x-y"},
		{long, strings.TrimSuffix(strings.Repeat("ab-", 30), "-"), strings.TrimSuffix(strings.Repeat("ab-", 20), "-")},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.plain {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.plain)
		}
		if got := SlugifyWith(c.in, SlugOptions{Unicode: true, MaxLen: 60}); got != c.task {
			t.Errorf("SlugifyWith(%q, unicode, 60) = %q, want %q", c.in, got, c.task)
		}
	}
}

func TestTargetsJoinTheHardWrappedPartsOfAHeading(t *testing.T) {
	long := strings.Repeat("word ", 400) + "\n"
	body := "## Big\n\n" + long + long + "\n## Next\n\nkept\n"
	parsed := strings.TrimSuffix(body, "\n")
	secs := Parse(parsed, 0)
	if secs[0].Part != 1 || secs[1].Part != 2 {
		t.Fatalf("the doc does not wrap: %+v", secs)
	}
	whole := Targets(parsed, "Big")
	if len(whole) != 1 || whole[0].Start != 0 || whole[0].End != secs[len(secs)-2].End() || whole[0].Path != "Big" {
		t.Fatalf("by heading: %+v", whole)
	}
	part := Targets(parsed, secs[1].Slug)
	if len(part) != 1 || part[0].Start != secs[1].StartLine || part[0].End != secs[1].End() || part[0].Line != -1 {
		t.Fatalf("by part slug: %+v", part)
	}
}

func TestTargetsFindAHeadingInsideAWholeSectionByPath(t *testing.T) {
	body := "## A\n\n### Shape\n\none\n\n#### Deep\n\nd\n\n## B\n\n### Shape\n\ntwo"
	if got := Targets(body, "Shape"); len(got) != 2 || got[0].Path != "A > Shape" || got[1].Path != "B > Shape" {
		t.Fatalf("%+v", got)
	}
	got := Targets(body, "B > Shape")
	if len(got) != 1 || got[0].Line != 12 || got[0].End != 15 {
		t.Fatalf("%+v", got)
	}
	if got := Targets(body, "a-shape-deep"); len(got) != 1 || got[0].Path != "A > Shape > Deep" || got[0].End != 10 {
		t.Fatalf("%+v", got)
	}
}

func TestTargetsNameAHeadingsWholeBlockPastTheTokenCap(t *testing.T) {
	filler := strings.Repeat("Steady words fill this paragraph for the cap. ", 22)
	body := "## Big\n\nlead\n\n### One\n\n" + filler + "\n\n#### Deep\n\nd\n\n### Two\n\n" + filler + "\n\n## Next\n\nkept"
	secs := Parse(body, 0)
	if len(secs) < 3 || secs[0].Slug != "big" || !strings.HasPrefix(secs[1].Slug, "big-one") {
		t.Fatalf("the H2 is not split at its H3s: %+v", secs)
	}
	next := slices.Index(Lines(body), "## Next")
	for _, ref := range []string{"Big", "big"} {
		got := Targets(body, ref)
		if len(got) != 1 || got[0].Start != 0 || got[0].End != next || got[0].Path != "Big" || got[0].Name() != "big" {
			t.Fatalf("%s: %+v", ref, got)
		}
	}
	two := slices.Index(Lines(body), "### Two")
	got := Targets(body, "Big > One")
	if len(got) != 1 || got[0].End != two || !strings.Contains(got[0].Span(body), "#### Deep") {
		t.Fatalf("an H3 holds its H4: %+v", got)
	}
}
