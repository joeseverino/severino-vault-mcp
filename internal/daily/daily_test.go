package daily_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/daily"
	"github.com/joeseverino/severino-vault-mcp/internal/fuzzseed"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func cfg(t *testing.T) config.Config { return config.Load("", tk.Env(tk.Dir(t))) }

func note(c config.Config, day string) string {
	return filepath.Join(c.VaultPath, c.DailyNotesDir, day+".md")
}

func TestFirstRunCreatesTheNoteWithTheRegion(t *testing.T) {
	c := cfg(t)
	r := daily.Write(c, "> [!info] hello", "2026-06-25")
	if !r.Bool("ok") || !r.Bool("created") || !r.Bool("inserted") {
		t.Fatal(jsonx.Compact(r))
	}
	text := tk.Read(t, note(c, "2026-06-25"))
	for _, want := range []string{"doc_id: daily-20260625", "MIRROR:BEGIN daily-brief", "MIRROR:END daily-brief", "> [!info] hello"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestRerunReplacesRegionAndPreservesCaptureArea(t *testing.T) {
	c := cfg(t)
	daily.Write(c, "first content", "2026-06-25")
	p := note(c, "2026-06-25")
	tk.Write(t, p, tk.Read(t, p)+"\n## My notes\n- did a thing\n")
	r := daily.Write(c, "second content (updated)", "2026-06-25")
	after := tk.Read(t, p)
	if !r.Bool("ok") || r.Bool("inserted") || !strings.Contains(after, "second content (updated)") || strings.Contains(after, "first content") ||
		!strings.Contains(after, "## My notes\n- did a thing") {
		t.Fatal(after)
	}
}

func TestSameContentIsANoOp(t *testing.T) {
	c := cfg(t)
	daily.Write(c, "same", "2026-06-25")
	if r := daily.Write(c, "same", "2026-06-25"); !r.Bool("ok") || r.Bool("changed") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestInvalidDateIsAnError(t *testing.T) {
	if r := daily.Write(cfg(t), "x", "not-a-date"); r.Bool("ok") || !strings.Contains(r.Str("error"), "invalid date") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestResolveDate(t *testing.T) {
	cases := map[string][2]string{
		"what did I do on 2026-05-04": {"2026-05-04", "iso_date"},
		"notes from 5/4/2026":         {"2026-05-04", "us_date"},
		"yesterday please":            {"2026-06-19", "yesterday"},
		"today":                       {"2026-06-20", "today"},
		"friday":                      {"2026-06-19", "weekday"},
		"last saturday":               {"2026-06-13", "weekday"},
		"saturday":                    {"2026-06-20", "weekday"},
		"anything":                    {"2026-06-20", "default_today"},
	}
	for q, want := range cases {
		d, how, err := daily.ResolveDate(q, "2026-06-20")
		if err != nil || d.Format("2006-01-02") != want[0] || how != want[1] {
			t.Errorf("%q: %s %s %v", q, d.Format("2006-01-02"), how, err)
		}
	}
}

func TestWriteRefusesADailyDirSymlinkedOutside(t *testing.T) {
	c := cfg(t)
	outside := t.TempDir()
	dir := filepath.Join(c.VaultPath, c.DailyNotesDir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	r := daily.Write(c, "content", "2026-06-25")
	if r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("note landed outside the vault: %v", entries)
	}
}

func FuzzUpsertRegion(f *testing.F) {
	begin, end := daily.Markers(daily.RegionID)
	for _, text := range fuzzseed.Markdown(f) {
		f.Add(text, "> [!info] brief")
	}
	f.Add(begin+"\nold\n"+end+"\n", "new")
	f.Add("---\ndoc_id: daily-20260625\n---\n\n- did a thing\n", "")
	f.Fuzz(func(t *testing.T, text, content string) {
		out, _ := daily.UpsertRegion(text, daily.RegionID, content)
		if !strings.Contains(out, begin) || !strings.Contains(out, end) {
			t.Fatalf("region markers missing from %q", out)
		}
	})
}
