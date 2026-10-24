package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

// One close over several items gives each its own result, closes every item
// it can whatever the others do, and rewrites each queue note once.
func TestOneCloseClosesSeveralItemsEachWithItsOwnResult(t *testing.T) {
	root, v := queueVault(t)
	for _, s := range []string{"First", "Second", "Third"} {
		add(t, v, "Example Tracker", "next", s)
	}
	add(t, v, "example-site", "next", "Elsewhere")
	writes := map[string]int{}
	prev := fsx.BeforeReplace
	t.Cleanup(func() { fsx.BeforeReplace = prev })
	fsx.BeforeReplace = func(rel string) { writes[rel]++ }

	r := core.Queue(v, core.QueueArgs{Action: "close", Resolution: "stale", Items: []core.QueueClose{
		{ID: "ex-1", Resolution: "example-tracker@abc1234"},
		{ID: "ex-2", Status: "dropped"},
		{ID: "ex-99", Resolution: "never filed"},
		{ID: "not an id"},
		{ID: "example-site-1"},
		{ID: "ex-1", Resolution: "again"},
	}})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "3 of 6") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
	for i, want := range []struct {
		ok                 bool
		status, resolution string
	}{
		{true, "closed", "example-tracker@abc1234"},
		{true, "dropped", "stale"},
		{false, "", ""},
		{false, "", ""},
		{true, "closed", "stale"},
		{false, "", ""},
	} {
		got, _ := tk.Get(r, "results").([]*jsonx.Obj)
		res := got[i]
		if res.Bool("ok") != want.ok || res.Str("id") == "" || want.ok && (res.Str("status") != want.status || res.Str("resolution") != want.resolution) ||
			!want.ok && !res.Has("next_steps") {
			t.Errorf("results[%d]: %s", i, jsonx.Compact(res))
		}
	}
	tracker := tk.Read(t, filepath.Join(root, trackerQueue))
	open, closed, _ := strings.Cut(tracker, "## Closed")
	if !strings.Contains(open, "Third") || !strings.Contains(closed, "[x] First") || !strings.Contains(closed, "[-] Second") ||
		!strings.Contains(closed, "[resolution:: example-tracker@abc1234]") {
		t.Fatal(tracker)
	}
	if writes[trackerQueue] != 1 || writes["01 Projects/example-site/Queue.md"] != 1 {
		t.Fatalf("each queue is rewritten once: %v", writes)
	}
}

// A close over several items whose queue changes under it, by a writer that
// skipped the lock, re-runs on what is there and reports what it wrote.
func TestACloseOverSeveralItemsReRunsOnAnOutsideAppend(t *testing.T) {
	root, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "First")
	add(t, v, "Example Tracker", "next", "Second")
	wait := tk.AppendDuringWrite(t, root, trackerQueue, "- [ ] Typed in Obsidian #queue/next  [id:: ex-9]", false)
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", Resolution: "done", Items: []core.QueueClose{{ID: "ex-1"}, {ID: "ex-2"}}}))
	wait()
	note := tk.Read(t, filepath.Join(root, trackerQueue))
	if tk.Get(r, "count") != 2 || !strings.Contains(note, "Typed in Obsidian") || strings.Count(note, "[resolution:: done]") != 2 {
		t.Fatal(jsonx.Compact(r), note)
	}
}

// A close over items already closed as asked changes nothing and writes
// nothing; one item with an id or agent beside items is refused.
func TestACloseOverSeveralItemsAlreadyClosedWritesNothing(t *testing.T) {
	root, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "First")
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: "ex-1", Resolution: "done"}))
	before := tk.Read(t, filepath.Join(root, trackerQueue))
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", Items: []core.QueueClose{{ID: "ex-1", Resolution: "done"}}}))
	if tk.Get(r, "results.0.no_op") != true || tk.Read(t, filepath.Join(root, trackerQueue)) != before {
		t.Fatal(jsonx.Compact(r))
	}
	for _, a := range []core.QueueArgs{
		{Action: "close", ID: "ex-1", Items: []core.QueueClose{{ID: "ex-1"}}},
		{Action: "close", Items: []core.QueueClose{}},
	} {
		if r := core.Queue(v, a); r.Bool("ok") || !r.Has("next_steps") {
			t.Fatal(jsonx.Compact(r))
		}
	}
}
