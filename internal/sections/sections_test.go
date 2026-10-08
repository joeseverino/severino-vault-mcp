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
