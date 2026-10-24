package search

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The byte scanner tokenizes exactly as the regexp [A-Za-z0-9]+ does.
func TestTermsMatchTheTokenRegexp(t *testing.T) {
	re := regexp.MustCompile(`[A-Za-z0-9]+`)
	for _, text := range []string{
		"", "   ", "Deploy HQ", "rb-add-dns_rewrite v2.3", "trailing word", "x",
		"café naïve über", "## Steps\n\n1. Run `mise run check`.\n", "ÅB9 —é— 10.0.0.1",
	} {
		var want []string
		for _, m := range re.FindAllString(text, -1) {
			want = append(want, strings.ToLower(m))
		}
		if got := slices.Collect(terms(text)); !slices.Equal(got, want) {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}
