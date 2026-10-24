package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) ([]Case, string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "vault"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := LoadCases(filepath.Join("testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cases, root
}

// The floors sit a small margin under what `ours` scores on the fixture, so
// a ranking or response-size regression fails CI. Raise them when a change
// improves the numbers.
var floors = struct {
	hit1, hit3, mrr, zeroMax float64
	findP90Max, readP90Max   int
	sectP90Max               int
}{hit1: 0.80, hit3: 0.89, mrr: 0.85, zeroMax: 0.05, findP90Max: 650, readP90Max: 620, sectP90Max: 150}

func TestEvalFloors(t *testing.T) {
	cases, root := fixture(t)
	reports, err := RunAll(FixtureConfig(root), cases, BackendNames())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + Table(reports, true))
	for _, r := range reports {
		if r.Backend != "ours" {
			continue
		}
		m := r.Metrics
		if m.HitAt1 < floors.hit1 || m.HitAt3 < floors.hit3 || m.MRR < floors.mrr || m.ZeroHitRate > floors.zeroMax {
			t.Errorf("ours fell below the ranking floors %+v: %+v", floors, m)
		}
		if m.FindP90 > floors.findP90Max || m.ReadP90 > floors.readP90Max || m.SectP90 > floors.sectP90Max {
			t.Errorf("ours responses grew past the size ceilings %+v: %+v", floors, m)
		}
		return
	}
	t.Fatal("no ours report")
}

// SVMC_EVAL_VAULT runs the same harness over a real vault, read-only, with
// cases kept outside the repository. It reports; it asserts nothing.
func TestEvalRealVault(t *testing.T) {
	root := os.Getenv("SVMC_EVAL_VAULT")
	if root == "" {
		t.Skip("SVMC_EVAL_VAULT not set")
	}
	path := os.Getenv("SVMC_EVAL_CASES")
	if path == "" {
		path = filepath.Join("..", "..", "evals", "local", "cases.json")
	}
	cases, err := LoadCases(path)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := RunAll(RealConfig(root), cases, BackendNames())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + Table(reports, true))
}

func TestSummarizeScoresRanksAndSizes(t *testing.T) {
	outs := []Outcome{
		{Rank: 1, Hits: 3, FindTokens: 10, ReadTokens: 100, SectionTokens: 10},
		{Rank: 2, Hits: 3, FindTokens: 20, ReadTokens: 200, SectionTokens: 20},
		{Rank: 4, Hits: 5, FindTokens: 30, ReadTokens: 300, SectionTokens: 30},
		{Rank: 0, Hits: 0, FindTokens: 40},
	}
	m := Summarize(outs)
	if m.HitAt1 != 0.25 || m.HitAt3 != 0.5 || m.ZeroHitRate != 0.25 {
		t.Fatalf("%+v", m)
	}
	if want := (1 + 0.5 + 0.25) / 4; m.MRR != want {
		t.Fatalf("MRR %v, want %v", m.MRR, want)
	}
	// Reads count only cases with a top hit; percentiles are nearest-rank.
	if m.FindMedian != 20 || m.FindP90 != 40 || m.ReadMedian != 200 || m.ReadP90 != 300 || m.SectMedian != 20 || m.SectP90 != 30 {
		t.Fatalf("%+v", m)
	}
}

type fake struct{ ids []string }

func (fake) Name() string { return "fake" }
func (f fake) Find(string, string, Filter, int) (Found, error) {
	return Found{IDs: f.ids, Tokens: 5}, nil
}
func (fake) Read(string, string) (int, error) { return 7, nil }

func TestRunRanksTheFirstWantedHit(t *testing.T) {
	r, err := Run(fake{[]string{"a", "b", "c"}}, []Case{
		{ID: "x", Query: "q", Mode: "relevance", Want: []string{"c", "b"}},
		{ID: "y", Query: "q", Mode: "text", Want: []string{"z"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcomes[0].Rank != 2 || r.Outcomes[1].Rank != 0 || r.Outcomes[0].Top != "a" || r.Outcomes[0].ReadTokens != 7 {
		t.Fatalf("%+v", r.Outcomes)
	}
}

func TestLoadCasesRefusesMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown field": `{"cases":[{"id":"a","query":"q","mode":"text","want":["x"],"wants":["y"]}]}`,
		"bad mode":      `{"cases":[{"id":"a","query":"q","mode":"fuzzy","want":["x"]}]}`,
		"no want":       `{"cases":[{"id":"a","query":"q","mode":"text","want":[]}]}`,
		"duplicate id":  `{"cases":[{"id":"a","query":"q","mode":"text","want":["x"]},{"id":"a","query":"r","mode":"text","want":["x"]}]}`,
		"empty":         `{"cases":[]}`,
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCases(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRunAllRefusesWantsTheVaultLacks(t *testing.T) {
	_, root := fixture(t)
	_, err := RunAll(FixtureConfig(root), []Case{{ID: "a", Query: "q", Mode: "text", Want: []string{"rb-not-in-vault"}}}, []string{"ours"})
	if err == nil || !strings.Contains(err.Error(), "rb-not-in-vault") {
		t.Fatalf("got %v", err)
	}
	if _, err := RunAll(FixtureConfig(root), []Case{{ID: "a", Query: "q", Mode: "text", Want: []string{"infra-dns"}}}, []string{"qmd"}); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

// The grep baseline reads files with no sensitivity gate, which is what an
// agent with Grep gets; the eval reports that cost rather than hiding it.
func TestGrepBaselineSeesRestrictedBodies(t *testing.T) {
	_, root := fixture(t)
	g, err := OpenGrep(FixtureConfig(root))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := g.Find("encrypted media signing session", "text", Filter{}, Limit)
	if len(f.IDs) == 0 || f.IDs[0] != "infra-local-pki" {
		t.Fatalf("%v", f.IDs)
	}
}
