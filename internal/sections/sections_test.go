package sections

import (
	"slices"
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

func TestResolveBySlugAndHeadingPath(t *testing.T) {
	secs := Parse(multi, 1)
	if Resolve(secs, "troubleshooting").Heading != "Troubleshooting" || Resolve(secs, "Routine operations").Slug != "routine-operations" ||
		Resolve(secs, "no-such-section") != nil {
		t.Fatal("resolve")
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
