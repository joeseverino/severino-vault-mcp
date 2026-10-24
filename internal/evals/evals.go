// Package evals measures how well a retriever answers the questions agents
// ask of the vault: a case file of queries, each naming the doc_ids that
// answer it, scored against a Backend with rank metrics and response sizes.
// It is deterministic and model-free, so two runs over the same vault and
// cases give the same numbers.
package evals

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

// Limit is the hit count each find asks for, matching the MCP tool's default.
const Limit = 10

// Case is one question with the docs that answer it.
type Case struct {
	ID     string   `json:"id"`
	Query  string   `json:"query"`
	Mode   string   `json:"mode"` // relevance or text
	Want   []string `json:"want"`
	Source string   `json:"source,omitempty"`
	// System and Project narrow the search as find's filters of the same names do.
	System  string `json:"system,omitempty"`
	Project string `json:"project,omitempty"`
}

// Filter is a case's narrowing of the search.
type Filter struct{ System, Project string }

// LoadCases reads a case file and checks every case is scorable.
func LoadCases(path string) ([]Case, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the case file is operator input
	if err != nil {
		return nil, err
	}
	var f struct {
		Cases []Case `json:"cases"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, c := range f.Cases {
		switch {
		case c.ID == "" || c.Query == "" || len(c.Want) == 0:
			return nil, fmt.Errorf("%s: case %q needs id, query and want", path, c.ID)
		case c.Mode != "relevance" && c.Mode != "text":
			return nil, fmt.Errorf("%s: case %q: mode must be relevance or text", path, c.ID)
		case seen[c.ID]:
			return nil, fmt.Errorf("%s: duplicate case id %q", path, c.ID)
		}
		seen[c.ID] = true
	}
	if len(f.Cases) == 0 {
		return nil, fmt.Errorf("%s: no cases", path)
	}
	return f.Cases, nil
}

// Found is one find call's answer: the ranked doc_ids, best first ("" for a
// result no doc_id names), the section each hit points at ("" when it names
// none), and the size of what the agent would read.
type Found struct {
	IDs      []string
	Sections []string
	Tokens   int
}

// Backend is a retriever under test. Find ranks docs for a query within a
// filter; Read returns the token cost of reading one doc the way an agent using this
// backend would: the whole doc with no section, else just that section where
// the backend can scope a read.
type Backend interface {
	Name() string
	Find(query, mode string, f Filter, limit int) (Found, error)
	Read(docID, section string) (int, error)
}

// Opener builds a backend over one vault's config.
type Opener func(config.Config) (Backend, error)

// Backends are the retrievers a run can score, by name. A new retriever (a
// qmd BM25 backend, say) is one Opener added here; the cases and metrics stay
// as they are.
var Backends = map[string]Opener{
	"ours": OpenOurs,
	"grep": OpenGrep,
}

// BackendNames lists Backends in a stable order.
func BackendNames() []string {
	names := make([]string, 0, len(Backends))
	for n := range Backends {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Tokens estimates a response's token count as chars/4.
func Tokens(s string) int { return (len(s) + 3) / 4 }

// Outcome is one case scored: the rank of the first wanted doc (0 when none
// is in the hits), the top hit, and the token costs.
type Outcome struct {
	Case       Case
	Rank       int
	Top        string
	Hits       int
	FindTokens int
	ReadTokens int // of the top hit, whole; 0 with no hits
	// SectionTokens reads only the section the top hit names: the
	// find-then-read-the-section path. Whole when the hit names none.
	SectionTokens int
}

// Report is one backend's run.
type Report struct {
	Backend  string
	Outcomes []Outcome
	Metrics  Metrics
}

// Metrics summarize a run.
type Metrics struct {
	Cases               int
	HitAt1, HitAt3, MRR float64
	ZeroHitRate         float64
	FindMedian, FindP90 int
	ReadMedian, ReadP90 int
	SectMedian, SectP90 int
}

// Run scores every case against b.
func Run(b Backend, cases []Case) (Report, error) {
	r := Report{Backend: b.Name()}
	for _, c := range cases {
		f, err := b.Find(c.Query, c.Mode, Filter{c.System, c.Project}, Limit)
		if err != nil {
			return r, fmt.Errorf("%s: case %s: %w", b.Name(), c.ID, err)
		}
		o := Outcome{Case: c, Hits: len(f.IDs), FindTokens: f.Tokens}
		for i, id := range f.IDs {
			if slices.Contains(c.Want, id) {
				o.Rank = i + 1
				break
			}
		}
		if len(f.IDs) > 0 {
			o.Top = f.IDs[0]
			if o.ReadTokens, err = b.Read(o.Top, ""); err != nil {
				return r, fmt.Errorf("%s: case %s: read %s: %w", b.Name(), c.ID, o.Top, err)
			}
			o.SectionTokens = o.ReadTokens
			if len(f.Sections) > 0 && f.Sections[0] != "" {
				if o.SectionTokens, err = b.Read(o.Top, f.Sections[0]); err != nil {
					return r, fmt.Errorf("%s: case %s: read %s#%s: %w", b.Name(), c.ID, o.Top, f.Sections[0], err)
				}
			}
		}
		r.Outcomes = append(r.Outcomes, o)
	}
	r.Metrics = Summarize(r.Outcomes)
	return r, nil
}

// Summarize computes the metrics over scored cases.
func Summarize(outs []Outcome) Metrics {
	m := Metrics{Cases: len(outs)}
	if len(outs) == 0 {
		return m
	}
	var find, read, sect []int
	var h1, h3, zero int
	var rr float64
	for _, o := range outs {
		if o.Rank == 1 {
			h1++
		}
		if o.Rank >= 1 && o.Rank <= 3 {
			h3++
		}
		if o.Rank > 0 {
			rr += 1 / float64(o.Rank)
		}
		find = append(find, o.FindTokens)
		if o.Hits == 0 {
			zero++
		} else {
			read = append(read, o.ReadTokens)
			sect = append(sect, o.SectionTokens)
		}
	}
	n := float64(len(outs))
	m.HitAt1, m.HitAt3, m.MRR, m.ZeroHitRate = float64(h1)/n, float64(h3)/n, rr/n, float64(zero)/n
	m.FindMedian, m.FindP90 = percentile(find, 50), percentile(find, 90)
	m.ReadMedian, m.ReadP90 = percentile(read, 50), percentile(read, 90)
	m.SectMedian, m.SectP90 = percentile(sect, 50), percentile(sect, 90)
	return m
}

// percentile is the nearest-rank percentile; 0 for no values.
func percentile(vals []int, p int) int {
	if len(vals) == 0 {
		return 0
	}
	s := slices.Clone(vals)
	slices.Sort(s)
	k := (p*len(s) + 99) / 100
	return s[max(k, 1)-1]
}

// Table renders reports as a compact text table, then each backend's misses
// when misses is set.
func Table(reports []Report, misses bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s %5s %6s %6s %6s %6s %12s %12s %12s\n", "backend", "cases", "hit@1", "hit@3", "MRR", "zero",
		"find p50/p90", "read p50/p90", "sect p50/p90")
	for _, r := range reports {
		m := r.Metrics
		fmt.Fprintf(&b, "%-8s %5d %6.3f %6.3f %6.3f %6.3f %12s %12s %12s\n", r.Backend, m.Cases, m.HitAt1, m.HitAt3, m.MRR,
			m.ZeroHitRate, fmt.Sprintf("%d/%d", m.FindMedian, m.FindP90), fmt.Sprintf("%d/%d", m.ReadMedian, m.ReadP90),
			fmt.Sprintf("%d/%d", m.SectMedian, m.SectP90))
	}
	if misses {
		for _, r := range reports {
			for _, o := range r.Outcomes {
				if o.Rank == 1 {
					continue
				}
				rank := "-"
				if o.Rank > 0 {
					rank = fmt.Sprint(o.Rank)
				}
				fmt.Fprintf(&b, "miss %-6s %-26s rank=%-2s top=%s\n", r.Backend, o.Case.ID, rank, o.Top)
			}
		}
	}
	return b.String()
}

// JSON renders reports for machines.
func JSON(reports []Report) *jsonx.Obj {
	out := []*jsonx.Obj{}
	for _, r := range reports {
		m := r.Metrics
		cases := []*jsonx.Obj{}
		for _, o := range r.Outcomes {
			cases = append(cases, jsonx.New("id", o.Case.ID, "mode", o.Case.Mode, "rank", o.Rank, "top", o.Top,
				"hits", o.Hits, "find_tokens", o.FindTokens, "read_tokens", o.ReadTokens,
				"section_tokens", o.SectionTokens))
		}
		out = append(out, jsonx.New("backend", r.Backend, "metrics", jsonx.New(
			"cases", m.Cases, "hit_at_1", m.HitAt1, "hit_at_3", m.HitAt3, "mrr", m.MRR, "zero_hit_rate", m.ZeroHitRate,
			"find_tokens_median", m.FindMedian, "find_tokens_p90", m.FindP90,
			"read_tokens_median", m.ReadMedian, "read_tokens_p90", m.ReadP90,
			"section_tokens_median", m.SectMedian, "section_tokens_p90", m.SectP90), "cases", cases))
	}
	return jsonx.New("ok", true, "limit", Limit, "reports", out)
}

// FixtureConfig is a hermetic config over a fixture vault: no config file,
// no aliases beyond the fixture's own and no restricted unlock. The index
// is cached for the run, since nothing writes to the fixture.
func FixtureConfig(root string) config.Config {
	env := config.Hermetic(root)
	env["SVMC_CACHE_SECONDS"] = "300"
	return config.Load("", env)
}

// RealConfig is the operator's own config pointed at path, with restricted
// unlock off so a run never prompts and never releases a restricted body.
func RealConfig(path string) config.Config {
	env := config.Locked(config.OSEnv())
	env["SVMC_VAULT_PATH"] = path
	return config.Load("", env)
}

// CheckWants refuses cases naming a doc_id the vault does not index, so a
// typo or a renamed doc reads as an error rather than as a retriever miss.
func CheckWants(cfg config.Config, cases []Case) error {
	idx := vault.NewLoader(cfg).Index(false)
	var missing []string
	for _, c := range cases {
		for _, id := range c.Want {
			if _, ok := idx.ByDocID[id]; !ok {
				missing = append(missing, c.ID+": "+id)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("wanted doc_ids not in the vault: %s", strings.Join(missing, ", "))
	}
	return nil
}

// RunAll checks the cases against cfg's vault, then scores them against each
// named backend.
func RunAll(cfg config.Config, cases []Case, names []string) ([]Report, error) {
	if err := CheckWants(cfg, cases); err != nil {
		return nil, err
	}
	var reports []Report
	for _, n := range names {
		open, ok := Backends[n]
		if !ok {
			return nil, fmt.Errorf("unknown backend %q; one of %s", n, strings.Join(BackendNames(), ", "))
		}
		b, err := open(cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		r, err := Run(b, cases)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, nil
}
