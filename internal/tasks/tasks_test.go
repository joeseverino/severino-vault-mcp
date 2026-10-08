package tasks_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	"github.com/joeseverino/severino-vault-mcp/internal/tasks"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

func taskVault(t *testing.T) (string, func() *vault.Loader) {
	root := tk.Dir(t)
	_ = os.MkdirAll(filepath.Join(root, "01 Projects", "cordon"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "07 Backlog"), 0o755)
	tk.Write(t, filepath.Join(root, "01 Projects", "cordon", "index.md"),
		"---\ndoc_id: project-cordon\ntitle: Cordon\ndoc_type: architecture_note\n---\n# Cordon\n")
	return root, func() *vault.Loader { return tk.Loader(root, "SVMC_INDEXED_DIRS", "01 Projects:07 Backlog") }
}

func add(l *vault.Loader, title string, mods ...func(*tasks.New)) *jsonx.Obj {
	n := tasks.New{Title: title}
	for _, m := range mods {
		m(&n)
	}
	return tasks.Add(l, n, tasks.Sections{})
}

func project(p string) func(*tasks.New) { return func(n *tasks.New) { n.Project = p } }
func related(r ...string) func(*tasks.New) {
	return func(n *tasks.New) { n.RelatedProjects = r }
}

func TestSchemaModelsTheTaskProfile(t *testing.T) {
	p := schema.Labs
	if !p.HasDocType("task") || !slices.Contains(p.DocIDPrefixes, "task-") {
		t.Fatal("task doc type")
	}
	if !slices.Equal(schema.Sorted(p.TaskStatuses), []string{"active", "done", "open", "parked", "wontfix"}) {
		t.Fatal(p.TaskStatuses)
	}
	for _, s := range []string{"open", "parked", "done", "wontfix"} {
		if p.HasStatus(s) {
			t.Fatalf("doc statuses share %s", s)
		}
	}
	ts, _ := p.AsDict().Get("task_statuses")
	if !slices.Equal(ts.([]string), schema.Sorted(p.TaskStatuses)) {
		t.Fatal(ts)
	}
}

func TestAddToProjectColocatesAndLinks(t *testing.T) {
	root, l := taskVault(t)
	r := add(l(), "Tighten v4 semantics", project("cordon"), func(n *tasks.New) { n.Effort = "M" })
	if !r.Bool("ok") || r.Str("relative_path") != "01 Projects/cordon/tasks/task-tighten-v4-semantics.md" {
		t.Fatal(jsonx.Compact(r))
	}
	fm := tk.Read(t, filepath.Join(root, r.Str("relative_path")))
	for _, want := range []string{"doc_type: task", "status: open", "- cordon", "effort: M"} {
		if !strings.Contains(fm, want) {
			t.Fatalf("missing %q in %s", want, fm)
		}
	}
}

func TestAddTaskBodySectionsFillAtWriteTime(t *testing.T) {
	root, l := taskVault(t)
	r := tasks.Add(l(), tasks.New{Title: "Body at write time", Project: "cordon"}, tasks.Sections{
		Problem: "Scaffold-only filings go hollow.", Fix: "Pass the sections in the one call.",
		Principle: "One validated write path.", Source: "Cohesion retro 2026-07-03.",
	})
	body := tk.Read(t, filepath.Join(root, r.Str("relative_path")))
	for _, want := range []string{"**Problem.** Scaffold-only filings go hollow.", "**Fix.** Pass the sections in the one call.",
		"**Principle.** One validated write path.", "**Source.** Cohesion retro 2026-07-03."} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestAddTaskWithoutBodyArgsKeepsBlankScaffold(t *testing.T) {
	root, l := taskVault(t)
	r := add(l(), "Still scaffolded", project("cordon"))
	body := tk.Read(t, filepath.Join(root, r.Str("relative_path")))
	if !strings.Contains(body, "**Problem.** \n") || !strings.Contains(body, "**Fix.** \n") {
		t.Fatal(body)
	}
}

func TestAddCrossCuttingGoesToTheBucket(t *testing.T) {
	_, l := taskVault(t)
	r := add(l(), "Add CI parity gate", related("cordon", "tools"))
	if r.Str("relative_path") != "07 Backlog/task-add-ci-parity-gate.md" || r.Str("project") != "cross" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestAddSingleRelatedProjectAutoColocates(t *testing.T) {
	_, l := taskVault(t)
	r := add(l(), "Only cordon", related("cordon"))
	if r.Str("relative_path") != "01 Projects/cordon/tasks/task-only-cordon.md" || r.Str("project") != "cordon" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestAddSingleUnknownRelatedProjectStaysCrossCutting(t *testing.T) {
	_, l := taskVault(t)
	r := add(l(), "Ghost proj", related("nope"))
	if r.Str("relative_path") != "07 Backlog/task-ghost-proj.md" || r.Str("project") != "cross" {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestAddRejectsUnknownProjectBadEffortAndDuplicate(t *testing.T) {
	_, l := taskVault(t)
	if r := add(l(), "x", project("nope")); r.Bool("ok") || !strings.Contains(r.Str("error"), "no such project") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := add(l(), "x", project("cordon"), func(n *tasks.New) { n.Effort = "XL" }); r.Bool("ok") || !strings.Contains(r.Str("error"), "effort") {
		t.Fatal(jsonx.Compact(r))
	}
	add(l(), "Dup", project("cordon"))
	if r := add(l(), "Dup", project("cordon")); r.Bool("ok") || !strings.Contains(r.Str("error"), "already exists") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestListGroupsByProjectOverBothSources(t *testing.T) {
	_, l := taskVault(t)
	add(l(), "Tighten v4", project("cordon"))
	add(l(), "Cross thing", related("cordon", "tools"))
	board := tasks.List(l(), tasks.Filter{})
	if tk.Get(board, "total") != 2 || jsonx.Compact(tk.Get(board, "counts.project")) != `{"cordon":1,"cross":1}` {
		t.Fatal(jsonx.Compact(board))
	}
	by := map[string]string{}
	for _, task := range tk.Hits(board, "tasks") {
		by[task.Str("slug")] = task.Str("project")
	}
	if by["tighten-v4"] != "cordon" || by["cross-thing"] != "cross" {
		t.Fatal(by)
	}
}

func slugs(board *jsonx.Obj, key string) []string {
	var out []string
	for _, task := range tk.Hits(board, key) {
		out = append(out, task.Str("slug"))
	}
	return out
}

func TestListDefaultHidesParkedAndDone(t *testing.T) {
	_, l := taskVault(t)
	add(l(), "Live one", project("cordon"))
	add(l(), "Shelved", project("cordon"))
	tasks.SetStatus(l(), "task-shelved", "parked")
	if got := slugs(tasks.List(l(), tasks.Filter{}), "tasks"); !slices.Equal(got, []string{"live-one"}) {
		t.Fatal(got)
	}
	got := slugs(tasks.List(l(), tasks.Filter{IncludeAll: true}), "tasks")
	slices.Sort(got)
	if !slices.Equal(got, []string{"live-one", "shelved"}) {
		t.Fatal(got)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestClosingFilesIntoDoneAndReopenMovesBack(t *testing.T) {
	root, l := taskVault(t)
	add(l(), "Filing", project("cordon"))
	done := tasks.SetStatus(l(), "filing", "done")
	if done.Str("relative_path") != "01 Projects/cordon/tasks/done/task-filing.md" || !exists(filepath.Join(root, done.Str("relative_path"))) ||
		exists(filepath.Join(root, "01 Projects/cordon/tasks/task-filing.md")) {
		t.Fatal(jsonx.Compact(done))
	}
	reopened := tasks.SetStatus(l(), "filing", "open")
	if reopened.Str("relative_path") != "01 Projects/cordon/tasks/task-filing.md" || exists(filepath.Join(root, "01 Projects/cordon/tasks/done/task-filing.md")) {
		t.Fatal(jsonx.Compact(reopened))
	}
}

func TestReconcileRehomesHandEditedStatuses(t *testing.T) {
	root, l := taskVault(t)
	add(l(), "Hand done", project("cordon"))
	p := filepath.Join(root, "01 Projects/cordon/tasks/task-hand-done.md")
	tk.Write(t, p, strings.Replace(tk.Read(t, p), "status: open", "status: done", 1))
	if r := tasks.Reconcile(l()); tk.Get(r, "moved") != 1 || !exists(filepath.Join(root, "01 Projects/cordon/tasks/done/task-hand-done.md")) {
		t.Fatal(jsonx.Compact(r))
	}
	if r := tasks.Reconcile(l()); tk.Get(r, "moved") != 0 {
		t.Fatal("not idempotent")
	}
}

func TestShippedListsRecentlyDoneKeptInPlace(t *testing.T) {
	root, l := taskVault(t)
	add(l(), "Shipped it", project("cordon"))
	tasks.SetStatus(l(), "shipped-it", "done")
	board := tasks.List(l(), tasks.Filter{})
	if !exists(filepath.Join(root, "01 Projects/cordon/tasks/done/task-shipped-it.md")) || slices.Contains(slugs(board, "tasks"), "shipped-it") ||
		!slices.Equal(slugs(board, "shipped"), []string{"shipped-it"}) {
		t.Fatal(jsonx.Compact(board))
	}
}

func TestMoveToDoneStampsClosedThenReopenClears(t *testing.T) {
	root, l := taskVault(t)
	add(l(), "Ship it", project("cordon"))
	done := tasks.SetStatus(l(), "ship-it", "done")
	fm := tk.Read(t, filepath.Join(root, done.Str("relative_path")))
	if !done.Bool("ok") || done.Str("status") != "done" || !strings.Contains(fm, "status: done") || !strings.Contains(fm, "closed: ") {
		t.Fatal(fm)
	}
	reopened := tasks.SetStatus(l(), "task-ship-it", "open")
	fm = tk.Read(t, filepath.Join(root, reopened.Str("relative_path")))
	if !strings.Contains(fm, "status: open") || strings.Contains(fm, "closed:") {
		t.Fatal(fm)
	}
}

func TestMoveRejectsBadStatusMissingTaskAndNonTasks(t *testing.T) {
	_, l := taskVault(t)
	add(l(), "Real", project("cordon"))
	if tasks.SetStatus(l(), "real", "sideways").Bool("ok") || tasks.SetStatus(l(), "ghost", "done").Bool("ok") {
		t.Fatal("accepted")
	}
	if r := tasks.SetStatus(l(), "project-cordon", "done"); r.Bool("ok") || !strings.Contains(r.Str("error"), "not a task") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestListProjectsIsTheColocationUniverseWithOpenCounts(t *testing.T) {
	root, l := taskVault(t)
	_ = os.MkdirAll(filepath.Join(root, "01 Projects", "tools"), 0o755)
	add(l(), "A cordon thing", project("cordon"))
	add(l(), "Another cordon thing", project("cordon"))
	add(l(), "Cross thing")
	got := map[string]any{}
	for _, p := range tk.Hits(tasks.Projects(l()), "projects") {
		got[p.Str("slug")], _ = p.Get("open")
	}
	if len(got) != 2 || got["cordon"] != 2 || got["tools"] != 0 {
		t.Fatal(got)
	}
}

func TestDeleteRemovesATaskAndRefusesNonTasks(t *testing.T) {
	root, l := taskVault(t)
	add(l(), "Junk", project("cordon"))
	rel := "01 Projects/cordon/tasks/task-junk.md"
	if r := tasks.Delete(l(), "junk"); !r.Bool("ok") || !r.Bool("deleted") || exists(filepath.Join(root, rel)) {
		t.Fatal(jsonx.Compact(r))
	}
	if slices.Contains(slugs(tasks.List(l(), tasks.Filter{}), "tasks"), "junk") {
		t.Fatal("still listed")
	}
	if tasks.Delete(l(), "project-cordon").Bool("ok") || tasks.Delete(l(), "ghost").Bool("ok") {
		t.Fatal("deleted a non-task")
	}
}

func TestPromoteNoteCreatesATaskPreservingBodyAndRemovesSource(t *testing.T) {
	root, l := taskVault(t)
	note := filepath.Join(root, "00 Inbox", "2026-06-23 idea.md")
	tk.Write(t, note, "---\ndoc_id: inbox-x\ncreated: 2026-06-23\n---\n\nWire the retry backoff.\n")
	r := tasks.Promote(l(), "00 Inbox/2026-06-23 idea.md", tasks.New{Title: "Wire the retry backoff", Project: "cordon"})
	if !r.Bool("ok") || r.Str("relative_path") != "01 Projects/cordon/tasks/task-wire-the-retry-backoff.md" || r.Str("promoted_from") != "00 Inbox/2026-06-23 idea.md" {
		t.Fatal(jsonx.Compact(r))
	}
	body := tk.Read(t, filepath.Join(root, r.Str("relative_path")))
	if !strings.Contains(body, "doc_type: task") || !strings.Contains(body, "Wire the retry backoff") || exists(note) {
		t.Fatal(body)
	}
}

func TestCreateReceiptIsStableAndSorted(t *testing.T) {
	_, l := taskVault(t)
	r := add(l(), "Receipt check", project("cordon"))
	receipt, _ := r.Get("receipt")
	ro := receipt.(*jsonx.Obj)
	cf, _ := ro.Get("changed_fields")
	if !slices.IsSorted(cf.([]string)) || len(ro.Str("idempotency_key")) != 64 || tk.Get(ro, "entity.id") != "task-receipt-check" {
		t.Fatal(jsonx.Compact(ro))
	}
}

func TestPromoteRefusesPathsOutsideTheVault(t *testing.T) {
	root, loader := taskVault(t)
	outside := filepath.Join(filepath.Dir(root), "outside-note.md")
	tk.Write(t, outside, "# stray\n")
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.Symlink(outside, filepath.Join(root, "07 Backlog", "link.md")); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{"../outside-note.md", "07 Backlog/../../outside-note.md", "07 Backlog/link.md"} {
		r := tasks.Promote(loader(), src, tasks.New{Title: "stolen", Effort: "S", Priority: "med"})
		if r.Bool("ok") {
			t.Fatalf("Promote(%q) accepted: %s", src, jsonx.Compact(r))
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("the outside note was deleted")
	}
}

func TestAddRefusesAProjectNameThatLeavesTheVault(t *testing.T) {
	root, loader := taskVault(t)
	if err := os.MkdirAll(filepath.Join(root, "01 Projects", "cordon", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := add(loader(), "escape attempt", project("../../.."))
	if r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	outside := filepath.Join(filepath.Dir(root), "tasks")
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a directory was created outside the vault")
	}
}

func TestAddRefusesATasksDirSymlinkedOutside(t *testing.T) {
	root, loader := taskVault(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "01 Projects", "cordon", "tasks")); err != nil {
		t.Fatal(err)
	}
	r := add(loader(), "linked out", project("cordon"))
	if r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("task landed outside the vault: %v", entries)
	}
}
