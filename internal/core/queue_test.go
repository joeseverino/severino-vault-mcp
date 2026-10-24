package core_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

const trackerQueue = "01 Projects/Example Tracker/Queue.md"

// queueVault is a vault with two projects: Example Tracker (prefix ex, two
// repos) and example-site (no index keys, so its folder name is the prefix).
func queueVault(t *testing.T) (string, *core.Vault) {
	t.Helper()
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/Example Tracker/index.md"),
		"doc_id: project-example-tracker\ntitle: Example Tracker\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nqueue_prefix: ex\nrepos: [example-tracker, example-tracker-ext]",
		"# Example Tracker\n")
	tk.Doc(t, filepath.Join(root, "01 Projects/example-site/index.md"),
		"doc_id: project-example-site\ntitle: Example Site\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nrepos:\n  - example-site",
		"# Example Site\n")
	return root, tk.Vault(root)
}

//go:fix inline
func text(s string) *string { return new(s) }

func add(t *testing.T, v *core.Vault, project, kind, what string, extra ...func(*core.QueueArgs)) string {
	t.Helper()
	a := core.QueueArgs{Action: "add", Project: project, Kind: kind, Text: new(what)}
	for _, f := range extra {
		f(&a)
	}
	return mustOK(t, core.Queue(v, a)).Str("id")
}

func ids(r *jsonx.Obj) []string {
	out := []string{}
	items, _ := r.Get("items")
	list, _ := items.([]*jsonx.Obj)
	for _, o := range list {
		out = append(out, o.Str("id"))
	}
	return out
}

func TestTheFirstAddCreatesAQueueThatValidates(t *testing.T) {
	root, v := queueVault(t)
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Write the store"),
		Links: map[string]string{"branch": "pass/queue"}}))
	if r.Str("id") != "ex-1" || !r.Bool("created_queue") || r.Str("doc_id") != "queue-example-tracker" || r.Str("relative_path") != trackerQueue {
		t.Fatal(jsonx.Compact(r))
	}
	if second := add(t, v, "Example Tracker", "decision", "Ship it?"); second != "ex-2" {
		t.Fatal(second)
	}
	note := tk.Read(t, filepath.Join(root, trackerQueue))
	if !strings.Contains(note, "## Open\n\n- [ ] Write the store  [kind:: next]  [branch:: pass/queue]  [id:: ex-1]\n- [ ] Ship it?  [kind:: decision]  [id:: ex-2]\n\n## Closed\n") {
		t.Fatal(note)
	}
	if _, problems, ok := core.CheckDoc(v, filepath.Join(root, trackerQueue)); !ok || len(problems) > 0 {
		t.Fatal(problems)
	}
	if site := add(t, v, "example-site", "next", "Draft"); site != "example-site-1" {
		t.Fatal(site)
	}
}

// Every add allocates its id under the note's lock, so adds racing from
// separate processes never hand out the same id or drop a line.
func TestConcurrentAddsGetDistinctIDs(t *testing.T) {
	root, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "Seed")
	const n = 16
	var wg sync.WaitGroup
	got := make([]string, n)
	for i := range n {
		wg.Go(func() {
			r := core.Queue(tk.Vault(root), core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new(fmt.Sprintf("Racer %d", i))})
			if !r.Bool("ok") {
				t.Error(jsonx.Compact(r))
			}
			got[i] = r.Str("id")
		})
	}
	wg.Wait()
	slices.Sort(got)
	if len(slices.Compact(slices.Clone(got))) != n {
		t.Fatalf("duplicate ids: %v", got)
	}
	list := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Full: true}))
	if tk.Get(list, "count") != n+1 || len(ids(list)) != n+1 {
		t.Fatalf("lines lost: %v", ids(list))
	}
}

// A claim is checked and written under the lock: of two racing claimers,
// exactly one takes the item and the other is told who holds it.
func TestExactlyOneOfTwoClaimersWins(t *testing.T) {
	for range 5 {
		root, v := queueVault(t)
		id := add(t, v, "Example Tracker", "next", "Contested")
		var wg sync.WaitGroup
		results := make([]*jsonx.Obj, 2)
		for i, who := range []string{"alpha", "beta"} {
			wg.Go(func() {
				results[i] = core.Queue(tk.Vault(root), core.QueueArgs{Action: "set", ID: id, Claim: true, Owner: new(who)})
			})
		}
		wg.Wait()
		wins := 0
		for _, r := range results {
			if r.Bool("ok") {
				wins++
			} else if !strings.Contains(r.Str("error"), id+" is owned by ") || r.Str("owner") == "" {
				t.Fatal(jsonx.Compact(r))
			}
		}
		if wins != 1 {
			t.Fatalf("%d claimers won: %s %s", wins, jsonx.Compact(results[0]), jsonx.Compact(results[1]))
		}
	}
}

func TestClaimingYourOwnItemAgainIsANoOp(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Mine", func(a *core.QueueArgs) { a.Owner = new("alpha") })
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Claim: true, Owner: new("alpha")}))
	if !r.Bool("no_op") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReadyIsOpenNextWorkWhoseBlockersAreAllClosed(t *testing.T) {
	_, v := queueVault(t)
	first := add(t, v, "Example Tracker", "next", "First")
	site := add(t, v, "example-site", "next", "Elsewhere")
	blocked := add(t, v, "Example Tracker", "follow-up", "After both", func(a *core.QueueArgs) { a.BlockedBy = []string{first, site} })
	add(t, v, "Example Tracker", "decision", "A question is never ready")
	ready := func() []string {
		return ids(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Ready: true, Full: true})))
	}
	if got := ready(); !slices.Equal(got, []string{first}) {
		t.Fatal(got)
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: first, Resolution: "example-tracker@abc1234"}))
	if got := ready(); len(got) != 0 {
		t.Fatalf("a blocker in another queue is still open: %v", got)
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: site, Status: "dropped", Resolution: "not needed"}))
	if got := ready(); !slices.Equal(got, []string{blocked}) {
		t.Fatal(got)
	}
}

func TestCloseMovesTheLineToClosedAndSetReopensIt(t *testing.T) {
	root, v := queueVault(t)
	keep := add(t, v, "Example Tracker", "next", "Stays open")
	done := add(t, v, "Example Tracker", "next", "Gets done")
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: done, Resolution: "example-tracker@abc1234"}))
	if tk.Get(r, "item.status") != "closed" || tk.Get(r, "item.resolution") != "example-tracker@abc1234" {
		t.Fatal(jsonx.Compact(r))
	}
	note := tk.Read(t, filepath.Join(root, trackerQueue))
	open, closed, _ := strings.Cut(note, "## Closed")
	if !strings.Contains(open, "[id:: "+keep+"]") || strings.Contains(open, "[id:: "+done+"]") ||
		!strings.Contains(closed, "\n- [x] Gets done  [kind:: next]  [resolution:: example-tracker@abc1234]  [id:: "+done+"]\n") {
		t.Fatal(note)
	}
	if strings.Contains(note, "\n\n\n") {
		t.Fatalf("moving a line left a gap:\n%s", note)
	}
	again := core.Queue(v, core.QueueArgs{Action: "close", ID: done, Resolution: "something else"})
	if again.Bool("ok") || !strings.Contains(again.Str("error"), "already closed") {
		t.Fatal(jsonx.Compact(again))
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: done, Status: "open"}))
	note = tk.Read(t, filepath.Join(root, trackerQueue))
	open, _, _ = strings.Cut(note, "## Closed")
	if !strings.Contains(open, "- [ ] Gets done  [kind:: next]  [id:: "+done+"]\n") {
		t.Fatal(note)
	}
}

// The Tasks plugin, in its Dataview format, ticks a box by rewriting the line
// with an [x] and a completion date; the queue reads that as closed.
func TestABoxTickedInObsidianClosesTheItemAndSurvivesTheNextWrite(t *testing.T) {
	root, v := queueVault(t)
	first := add(t, v, "Example Tracker", "next", "Ticked by hand")
	blocked := add(t, v, "Example Tracker", "next", "Waits on it", func(a *core.QueueArgs) { a.BlockedBy = []string{first} })
	path := filepath.Join(root, trackerQueue)
	note := tk.Read(t, path)
	line := "- [ ] Ticked by hand  [kind:: next]  [id:: " + first + "]"
	ticked := "- [x] Ticked by hand  [kind:: next]  [id:: " + first + "]  [completion:: 2026-10-22]"
	if !strings.Contains(note, line+"\n") {
		t.Fatal(note)
	}
	tk.Write(t, path, strings.Replace(note, line, ticked, 1))

	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Ready: true, Full: true}))
	if got := ids(r); !slices.Equal(got, []string{blocked}) || r.Has("unparsed_lines") {
		t.Fatalf("a ticked blocker still blocks: %s", jsonx.Compact(r))
	}
	// Another item's write keeps the ticked line as it is.
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: blocked, Owner: new("worker")}))
	if !strings.Contains(tk.Read(t, path), ticked+"\n") {
		t.Fatal(tk.Read(t, path))
	}
	// close records a resolution, keeps the date, and files the line under ## Closed.
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: first, Resolution: "example-tracker@abc1234"}))
	_, closed, _ := strings.Cut(tk.Read(t, path), "## Closed")
	if !strings.Contains(closed, "- [x] Ticked by hand  [kind:: next]  [resolution:: example-tracker@abc1234]  [id:: "+first+"]  [completion:: 2026-10-22]\n") {
		t.Fatal(tk.Read(t, path))
	}
}

func TestADelegatedItemIsFoundSetAndClosedByItsAgent(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "delegated", "Build it", func(a *core.QueueArgs) {
		a.Agent, a.Owner, a.Links = "a1b2c3", new("worker"), map[string]string{"branch": "pass/build"}
	})
	if dup := core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "delegated", Text: new("Again"), Agent: "a1b2c3"}); dup.Bool("ok") {
		t.Fatal("a second open item for one agent", jsonx.Compact(dup))
	}
	found := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Agent: "a1b2c3", Full: true}))
	if !slices.Equal(ids(found), []string{id}) {
		t.Fatal(jsonx.Compact(found))
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", Agent: "a1b2c3", Status: "returned", Links: map[string]string{"commit": "abc1234,def5678"}}))
	if tk.Get(r, "item.status") != "returned" || tk.Get(r, "item.links.branch") != "pass/build" || tk.Get(r, "item.links.commit") != "abc1234,def5678" {
		t.Fatal(jsonx.Compact(r))
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", Agent: "a1b2c3", Resolution: "example-tracker@def5678"}))
	if r := core.Queue(v, core.QueueArgs{Action: "close", Agent: "a1b2c3", Resolution: "x"}); r.Bool("ok") || !strings.Contains(r.Str("error"), "it is done") {
		t.Fatal(jsonx.Compact(r))
	}
	next := add(t, v, "Example Tracker", "next", "Not delegated")
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: next, Status: "returned"}); r.Bool("ok") || !strings.Contains(r.Str("error"), "only a delegated item") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestACwdResolvesThroughItsRepoToTheProject(t *testing.T) {
	root, v := queueVault(t)
	code := tk.Dir(t)
	repo := filepath.Join(code, "example-tracker-ext")
	worktree := filepath.Join(code, "elsewhere", "agent-7")
	tk.Write(t, filepath.Join(repo, "internal", "x.go"), "package x\n")
	tk.Write(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(code, "example-site", ".git", "worktrees", "agent-7")+"\n")
	tk.Write(t, filepath.Join(code, "unmapped", "README.md"), "x\n")
	for cwd, project := range map[string]string{
		filepath.Join(repo, "internal"):   "Example Tracker",
		filepath.Join(worktree, "a", "b"): "example-site",
	} {
		r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Cwd: cwd, Kind: "next", Text: new("From " + cwd)}))
		if r.Str("project") != project || r.Has("fallback") {
			t.Fatal(cwd, jsonx.Compact(r))
		}
	}
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Repo: "example-tracker", Kind: "next", Text: new("By repo")})); r.Str("project") != "Example Tracker" {
		t.Fatal(jsonx.Compact(r))
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Cwd: filepath.Join(code, "unmapped"), Kind: "next", Text: new("Nowhere")}))
	if r.Str("project") != "backlog" || r.Str("id") != "backlog-1" || r.Str("doc_id") != "queue-backlog" || !strings.Contains(r.Str("fallback"), "07 Backlog/Queue.md") {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, "07 Backlog/Queue.md")), "- [ ] Nowhere  [kind:: next]  [id:: backlog-1]") {
		t.Fatal("the backlog queue was not written")
	}
}

func TestListAllSpansEveryQueueAndMasksFields(t *testing.T) {
	_, v := queueVault(t)
	add(t, v, "Example Tracker", "decision", "Pick a name")
	add(t, v, "example-site", "decision", "Pick a colour")
	add(t, v, "example-site", "next", "Not a decision")
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true, Kind: "decision"}))
	if !slices.Equal(ids(r), []string{"ex-1", "example-site-1"}) || tk.Get(r, "items.0.project") != nil || tk.Get(r, "items.0.text") != "Pick a name" {
		t.Fatal(jsonx.Compact(r))
	}
	r = mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true, Limit: 1, Fields: []string{"id", "links.pr"}}))
	if tk.Get(r, "count") != 3 || len(ids(r)) != 1 || jsonx.Compact(tk.Get(r, "items.0")) != `{"id":"ex-1","links":{}}` {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.Queue(v, core.QueueArgs{Action: "list"}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestPromoteGivesTheItemABodyDoc(t *testing.T) {
	root, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Design the import")
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "promote", ID: id, Content: "## Plan\n\nRead every task."}))
	if r.Str("body") != "note-ex-1" || tk.Get(r, "item.body") != "note-ex-1" {
		t.Fatal(jsonx.Compact(r))
	}
	doc := tk.Read(t, filepath.Join(root, "01 Projects/Example Tracker/Queue/ex-1.md"))
	if !strings.Contains(doc, "doc_id: note-ex-1") || !strings.Contains(doc, "# Design the import\n\n## Plan") {
		t.Fatal(doc)
	}
	if again := core.Queue(v, core.QueueArgs{Action: "promote", ID: id}); again.Bool("ok") || !strings.Contains(again.Str("error"), "already has the body") {
		t.Fatal(jsonx.Compact(again))
	}
}

func TestQueueRefusesARestrictedQueue(t *testing.T) {
	root, v := queueVault(t)
	tk.Doc(t, filepath.Join(root, trackerQueue), "doc_id: queue-example-tracker\ntitle: Q\ndoc_type: architecture_note\nsystem: x\n"+
		"environment: other\nstatus: active\nsensitivity: restricted", "# Q\n\n## Open\n\n- [ ] Secret  [kind:: next]  [id:: ex-1]\n\n## Closed\n")
	if r := core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x")}); r.Bool("ok") || !strings.Contains(r.Str("error"), "restricted") {
		t.Fatal(jsonx.Compact(r))
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true}))
	if len(ids(r)) != 0 || jsonx.Compact(tk.Get(r, "skipped_restricted")) != `["queue-example-tracker"]` {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestEveryQueueFailureCarriesNextSteps(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "x")
	for _, a := range []core.QueueArgs{
		{Action: "nope"},
		{Action: "add", Project: "Example Tracker", Text: new("no kind")},
		{Action: "add", Project: "Example Tracker", Kind: "next"},
		{Action: "add", Project: "No Such", Kind: "next", Text: new("x")},
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("two\nlines")},
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x"), Owner: new("two words")},
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x"), Links: map[string]string{"colour": "red"}},
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x"), BlockedBy: []string{"not an id"}},
		{Action: "set", ID: "ex-99", Status: "parked"},
		{Action: "set", ID: "zz-1", Status: "parked"},
		{Action: "set", ID: id, Status: "closed"},
		{Action: "set", ID: id, Claim: true},
		{Action: "set"},
		{Action: "close", ID: id},
		{Action: "list", All: true, Status: "done"},
		{Action: "list", All: true, Kind: "chore"},
		{Action: "list", All: true, Fields: []string{"nope"}},
	} {
		r := core.Queue(v, a)
		if r.Bool("ok") || !r.Has("next_steps") {
			t.Errorf("%+v: %s", a, jsonx.Compact(r))
		}
	}
}
