// Package brief is the doc-side vault state in one payload: recent commits,
// docs overdue for review, inbox backlog, and the task summary.
package brief

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/query"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

func ageDays(iso string) (int, bool) {
	if len(iso) < 10 {
		return 0, false
	}
	t, err := clock.ParseDate(iso[:10])
	if err != nil {
		return 0, false
	}
	return clock.DaysBetween(t, clock.Today()), true
}

// Vault builds the brief.
func Vault(l *vault.Loader, days, reviewAfterDays, recentLimit int) *jsonx.Obj {
	reviewAfterDays = max(0, reviewAfterDays)
	idx := l.Index(false)

	changes := query.RecentChanges(l, days, recentLimit)
	commits, _ := changes.Get("commits")
	if commits == nil {
		commits = []*jsonx.Obj{}
	}
	changesErr, hasErr := changes.Get("error")

	type stale struct {
		o   *jsonx.Obj
		age int
	}
	var staleDocs []stale
	for _, d := range idx.Docs {
		if !d.HasLastReviewed {
			continue
		}
		if age, ok := ageDays(d.LastReviewed); ok && age > reviewAfterDays {
			staleDocs = append(staleDocs, stale{jsonx.New(
				"doc_id", d.DocID, "title", d.Title, "obsidian_path", d.RelativePath,
				"last_reviewed", d.LastReviewed, "age_days", age), age})
		}
	}
	slices.SortStableFunc(staleDocs, func(a, b stale) int { return b.age - a.age })
	docs := []*jsonx.Obj{}
	for _, s := range staleDocs {
		docs = append(docs, s.o)
	}

	inbox := 0
	if matches, err := filepath.Glob(filepath.Join(l.Config.VaultPath, "00 Inbox", "*.md")); err == nil {
		if info, err := os.Stat(filepath.Join(l.Config.VaultPath, "00 Inbox")); err == nil && info.IsDir() {
			inbox = len(matches)
		}
	}

	board := tasks.List(l, tasks.Filter{})
	counts, _ := board.Get("counts")
	staleCount, _ := counts.(*jsonx.Obj).Get("stale")
	taskList, _ := board.Get("tasks")
	slugs := []string{}
	for _, t := range taskList.([]*jsonx.Obj) {
		if t.Bool("stale") && len(slugs) < 8 {
			slugs = append(slugs, t.Str("slug"))
		}
	}
	count, _ := board.Get("count")
	total, _ := board.Get("total")

	commitCount := 0
	if c, ok := commits.([]*jsonx.Obj); ok {
		commitCount = len(c)
	}
	recent := jsonx.New("days", days, "count", commitCount, "commits", commits)
	if hasErr && changesErr != nil && changesErr != "" {
		recent.Set("error", changesErr)
	}
	return jsonx.New(
		"ok", true,
		"vault_doc_count", len(idx.Docs),
		"recent_changes", recent,
		"docs_to_review", jsonx.New("after_days", reviewAfterDays, "count", len(docs), "docs", docs),
		"inbox", jsonx.New("count", inbox),
		"tasks", jsonx.New("open", count, "total", total, "stale", staleCount, "stale_slugs", slugs),
	)
}
