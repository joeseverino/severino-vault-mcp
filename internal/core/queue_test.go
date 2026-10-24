package core_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

const trackerQueue = "01 Projects/Example Tracker/Queue.md"

// filed is the created field every add in queueVault writes, on the date
// it pins.
const filed = "  [created:: 2026-10-22]"

// queueVault is a vault with two projects: Example Tracker (prefix ex, two
// repos) and example-site (no index keys, so its folder name is the prefix).
func queueVault(t *testing.T) (string, *core.Vault) {
	t.Helper()
	root := tk.Dir(t)
	now := clock.Now
	clock.Now = func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local) }
	t.Cleanup(func() { clock.Now = now })
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
	if !strings.Contains(note, "## Open\n\n- [ ] Write the store #queue/next  [branch:: pass/queue]  [id:: ex-1]"+filed+"\n- [ ] Ship it? #queue/decision  [id:: ex-2]"+filed+"\n\n## Closed\n") {
		t.Fatal(note)
	}
	if _, problems, ok := core.CheckDoc(v, filepath.Join(root, trackerQueue)); !ok || len(problems) > 0 {
		t.Fatal(problems)
	}
	// The note carries only what a queue note needs, and no H1: its file
	// name and breadcrumb name it.
	n := frontmatter.NewNote(note)
	if got := n.FM.Keys(); !slices.Equal(slices.Sorted(slices.Values(got)), []string{"doc_id", "doc_type", "last_reviewed", "status", "system", "title"}) {
		t.Fatalf("queue note fields %v", got)
	}
	if !strings.HasPrefix(note[n.Offset:], "## Open\n") && !strings.HasPrefix(note[n.Offset:], "\n## Open\n") {
		t.Fatalf("queue note body:\n%s", note[n.Offset:])
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

func TestCloseMovesTheLineToClosedAndSetCannotReopenIt(t *testing.T) {
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
		!strings.Contains(closed, "\n- [x] Gets done #queue/next  [resolution:: example-tracker@abc1234]  [id:: "+done+"]"+filed+"\n") {
		t.Fatal(note)
	}
	if strings.Contains(note, "\n\n\n") {
		t.Fatalf("moving a line left a gap:\n%s", note)
	}
	again := core.Queue(v, core.QueueArgs{Action: "close", ID: done, Resolution: "something else"})
	if again.Bool("ok") || !strings.Contains(again.Str("error"), "already closed") {
		t.Fatal(jsonx.Compact(again))
	}
	before := tk.Read(t, filepath.Join(root, trackerQueue))
	for _, status := range []string{"open", "returned"} {
		r := core.Queue(v, core.QueueArgs{Action: "set", ID: done, Status: status})
		if r.Bool("ok") || !strings.Contains(r.Str("error"), "closed") || !r.Has("next_steps") {
			t.Fatal(status, jsonx.Compact(r))
		}
	}
	if tk.Read(t, filepath.Join(root, trackerQueue)) != before {
		t.Fatal("a refused reopen changed the note")
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
	line := "- [ ] Ticked by hand #queue/next  [id:: " + first + "]" + filed
	ticked := "- [x] Ticked by hand #queue/next  [id:: " + first + "]" + filed + "  [completion:: 2026-10-22]"
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
	if !strings.Contains(closed, "- [x] Ticked by hand #queue/next  [resolution:: example-tracker@abc1234]  [id:: "+first+"]"+filed+"  [completion:: 2026-10-22]\n") {
		t.Fatal(tk.Read(t, path))
	}
}

func TestADelegatedItemIsFoundSetAndClosedByItsAgent(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "delegated", "Build it", func(a *core.QueueArgs) {
		a.Agent, a.Owner, a.Links = "a1b2c3", new("worker"), map[string]string{"branch": "pass/build"}
	})
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
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "backlog", Kind: "next", Text: new("Nowhere")}))
	if r.Str("project") != "backlog" || r.Str("id") != "backlog-1" || r.Str("doc_id") != "queue-backlog" {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, "07 Backlog/Queue.md")), "- [ ] Nowhere #queue/next  [id:: backlog-1]") {
		t.Fatal("the backlog queue was not written")
	}
}

// A relative cwd or origin ("." from inside a repo) resolves to the same
// repository as its absolute path.
func TestARelativeCwdOrOriginResolvesAgainstTheProcessDirectory(t *testing.T) {
	_, v := queueVault(t)
	repo := filepath.Join(tk.Dir(t), "example-tracker-ext")
	tk.Write(t, filepath.Join(repo, "internal", "x.go"), "package x\n")
	tk.Write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	t.Chdir(filepath.Join(repo, "internal"))
	for _, cwd := range []string{".", "..", "../internal"} {
		r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Cwd: cwd}))
		if r.Str("project") != "Example Tracker" || r.Has("fallback") {
			t.Fatal(cwd, jsonx.Compact(r))
		}
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "delegated", Agent: "o1", Origin: "..", Text: new("From a repo")}))
	if got := tk.Get(r, "item.origin"); got != "example-tracker-ext" {
		t.Fatal(got, jsonx.Compact(r))
	}
}

// A repo two projects list names no queue: the call is refused naming both.
func TestARepoTwoProjectsListIsRefusedNamingBoth(t *testing.T) {
	root, v := queueVault(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/example-docs/index.md"),
		"doc_id: project-example-docs\ntitle: Example Docs\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nrepos: [example-site]", "# Example Docs\n")
	r := core.Queue(v, core.QueueArgs{Action: "add", Repo: "example-site", Kind: "next", Text: new("Where?")})
	if r.Bool("ok") || r.Str("error") != "projects \"example-docs\", \"example-site\" each list repo \"example-site\"; keep it in one index.md's repos" {
		t.Fatal(jsonx.Compact(r))
	}
}

// A write from a repo no project maps is refused with the call that maps it,
// never filed in the backlog where the repo's sessions would not see it; a
// list there reads the backlog and warns that it did.
func TestAnUnmappedRepoIsRefusedOnWriteAndWarnedOnList(t *testing.T) {
	root, v := queueVault(t)
	code := tk.Dir(t)
	repo := filepath.Join(code, "example-unmapped")
	tk.Write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	tk.Write(t, filepath.Join(repo, "cmd", "x.go"), "package x\n")
	worktree := filepath.Join(code, "wt", "agent-9")
	tk.Write(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(repo, ".git", "worktrees", "agent-9")+"\n")
	for _, a := range []core.QueueArgs{
		{Action: "add", Cwd: filepath.Join(repo, "cmd"), Kind: "next", Text: new("Nowhere")},
		{Action: "add", Cwd: worktree, Kind: "delegated", Agent: "a9", Text: new("Nowhere")},
		{Action: "add", Repo: "example-unmapped", Kind: "next", Text: new("Nowhere")},
	} {
		r := core.Queue(v, a)
		steps := jsonx.Compact(tk.Get(r, "next_steps"))
		if r.Bool("ok") || !strings.Contains(steps, `set_frontmatter(relative_path=\"01 Projects/<project>/index.md\", add_repos=[\"example-unmapped\"])`) ||
			!strings.Contains(steps, `project=\"backlog\"`) {
			t.Fatalf("%+v: %s", a, jsonx.Compact(r))
		}
	}
	if _, err := os.Stat(filepath.Join(root, "07 Backlog/Queue.md")); err == nil {
		t.Fatal("an unmapped add wrote the backlog queue")
	}
	add(t, v, "backlog", "next", "Cross-cutting")
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Cwd: repo}))
	if r.Str("project") != "backlog" || r.Str("fallback") != "07 Backlog/Queue.md" || !strings.Contains(r.Str("warning"), "add_repos") ||
		!slices.Equal(ids(r), []string{"backlog-1"}) {
		t.Fatal(jsonx.Compact(r))
	}
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "backlog"})); r.Has("warning") || r.Has("fallback") {
		t.Fatal(jsonx.Compact(r))
	}
}

// A worker sent from one repo to work in another is filed in the queue of
// the repo it works in, carrying its origin; the sending session's list with
// include_origin shows it beside its own queue's items, with its project.
func TestASessionSeesTheWorkersItSentToOtherRepos(t *testing.T) {
	root, v := queueVault(t)
	code := tk.Dir(t)
	site := filepath.Join(code, "example-site")
	tk.Write(t, filepath.Join(site, ".git", "HEAD"), "ref: refs/heads/main\n")
	worktree := filepath.Join(code, "wt", "agent-1")
	tk.Write(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(code, "example-tracker", ".git", "worktrees", "agent-1")+"\n")

	own := add(t, v, "example-site", "next", "Site work")
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Cwd: worktree, Kind: "delegated", Agent: "w1", Text: new("Tracker work"),
		Origin: filepath.Join(site, "sub")}))
	sent := r.Str("id")
	if r.Str("project") != "Example Tracker" || tk.Get(r, "item.origin") != "example-site" {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, trackerQueue)), "[agent:: w1]  [origin:: example-site]  [id:: "+sent+"]") {
		t.Fatal(tk.Read(t, filepath.Join(root, trackerQueue)))
	}
	// Work the tracker itself filed with that origin is not a delegated worker.
	add(t, v, "Example Tracker", "next", "Not a worker", func(a *core.QueueArgs) { a.Origin = "example-site" })

	if got := ids(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Cwd: site}))); !slices.Equal(got, []string{own}) {
		t.Fatalf("without include_origin: %v", got)
	}
	r = mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Cwd: site, IncludeOrigin: true}))
	if !slices.Equal(ids(r), []string{sent, own}) && !slices.Equal(ids(r), []string{own, sent}) {
		t.Fatal(jsonx.Compact(r))
	}
	for _, it := range tk.Hits(r, "items") {
		if it.Str("id") == sent && (it.Str("project") != "Example Tracker" || it.Str("origin") != "example-site") {
			t.Fatal(jsonx.Compact(it))
		}
	}
	if got := ids(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Origin: "example-site", Kind: "delegated"}))); !slices.Equal(got, []string{sent}) {
		t.Fatalf("origin filter: %v", got)
	}
	if r := core.Queue(v, core.QueueArgs{Action: "list", All: true, IncludeOrigin: true}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
}

// One worker's item spanning repos closes only once every repo has landed:
// a partial close records each repo@sha and leaves it open, a whole close
// short of every repo is refused, and a parent waits on it through
// blocked_by.
func TestAMultiRepoItemClosesWhenEveryRepoHasLanded(t *testing.T) {
	root, v := queueVault(t)
	id := add(t, v, "Example Tracker", "delegated", "Span", func(a *core.QueueArgs) {
		a.Agent, a.Repos = "w2", []string{"example-a,example-b"}
	})
	parent := add(t, v, "example-site", "next", "Ship both", func(a *core.QueueArgs) { a.BlockedBy = []string{id} })
	ready := func() []string {
		return ids(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "example-site", Ready: true})))
	}
	closeIt := func(partial bool, resolution string) *jsonx.Obj {
		return core.Queue(v, core.QueueArgs{Action: "close", Agent: "w2", Partial: partial, Resolution: resolution})
	}
	if r := closeIt(false, "example-a@abc1234"); r.Bool("ok") || !strings.Contains(r.Str("error"), "example-b") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := closeIt(true, "example-z@abc1234"); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal("a landing in a repo the item does not list", jsonx.Compact(r))
	}
	r := mustOK(t, closeIt(true, "example-a@abc1234"))
	if tk.Get(r, "item.status") != "open" || tk.Get(r, "item.resolution") != "example-a@abc1234" || jsonx.Compact(tk.Get(r, "item.repos")) != `["example-a","example-b"]` {
		t.Fatal(jsonx.Compact(r))
	}
	if open, _, _ := strings.Cut(tk.Read(t, filepath.Join(root, trackerQueue)), "## Closed"); !strings.Contains(open,
		"- [ ] Span #queue/delegated  [agent:: w2]  [repos:: example-a,example-b]  [resolution:: example-a@abc1234]  [id:: "+id+"]") {
		t.Fatal(tk.Read(t, filepath.Join(root, trackerQueue)))
	}
	if r := mustOK(t, closeIt(true, "example-a@abc1234")); !r.Bool("no_op") {
		t.Fatal(jsonx.Compact(r))
	}
	if got := ready(); len(got) != 0 {
		t.Fatalf("the parent is ready before its child closed: %v", got)
	}
	r = mustOK(t, closeIt(true, "example-b@def5678"))
	if tk.Get(r, "item.status") != "closed" || tk.Get(r, "item.resolution") != "example-a@abc1234,example-b@def5678" {
		t.Fatal(jsonx.Compact(r))
	}
	if got := ready(); !slices.Equal(got, []string{parent}) {
		t.Fatalf("the parent is not ready once its child closed: %v", got)
	}
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: id, Partial: true, Resolution: "example-b@def5678"})); !r.Bool("no_op") {
		t.Fatal(jsonx.Compact(r))
	}

	whole := add(t, v, "Example Tracker", "delegated", "Both at once", func(a *core.QueueArgs) { a.Repos = []string{"example-a", "example-b"} })
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: whole, Resolution: "example-a@1111111,example-b@2222222"})); tk.Get(r, "item.status") != "closed" {
		t.Fatal(jsonx.Compact(r))
	}
	single := add(t, v, "Example Tracker", "next", "One repo")
	if r := core.Queue(v, core.QueueArgs{Action: "close", ID: single, Partial: true, Resolution: "example-tracker@abc1234"}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.Queue(v, core.QueueArgs{Action: "close", ID: single, Partial: true, Resolution: "not a landing"}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
}

// A queue note is doc_type queue: find leaves it out unless doc_type asks for
// it.
func TestQueueNotesStayOutOfFindUnlessAskedFor(t *testing.T) {
	root, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "Zanzibar quokka")
	if note := tk.Read(t, filepath.Join(root, trackerQueue)); !strings.Contains(note, "doc_type: queue\n") {
		t.Fatal(note)
	}
	found := func(a core.FindArgs) []string {
		a.By, a.Limit = "relevance", 10
		r := core.Find(v, "Example Tracker Queue", a)
		var out []string
		for _, h := range tk.Hits(r, "hits") {
			out = append(out, h.Str("doc_id"))
		}
		return out
	}
	if got := found(core.FindArgs{}); slices.Contains(got, "queue-example-tracker") {
		t.Fatalf("find returned the queue note: %v", got)
	}
	if got := found(core.FindArgs{DocType: "queue"}); !slices.Equal(got, []string{"queue-example-tracker"}) {
		t.Fatalf("doc_type queue: %v", got)
	}
}

// An id never changes once issued: after a project's queue_prefix is set or
// changed, its old items are still set, closed and counted as blockers by
// their old ids, and new items take the new prefix.
func TestItemsKeepTheirIDsWhenTheQueuePrefixChanges(t *testing.T) {
	root, v := queueVault(t)
	first := add(t, v, "example-site", "next", "Before the rename")
	second := add(t, v, "example-site", "next", "Waits on the first", func(a *core.QueueArgs) { a.BlockedBy = []string{first} })
	if first != "example-site-1" {
		t.Fatal(first)
	}
	index := filepath.Join(root, "01 Projects/example-site/index.md")
	tk.Write(t, index, strings.Replace(tk.Read(t, index), "repos:\n", "queue_prefix: site\nrepos:\n", 1))

	if third := add(t, v, "example-site", "next", "After the rename"); third != "site-1" {
		t.Fatal(third)
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: second, Owner: new("worker")}))
	ready := func() []string {
		return ids(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "example-site", Ready: true, Full: true})))
	}
	if got := ready(); !slices.Equal(got, []string{first, "site-1"}) {
		t.Fatal(got)
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "close", ID: first, Resolution: "example-site@abc1234"}))
	if got := ready(); !slices.Equal(got, []string{second, "site-1"}) {
		t.Fatalf("a closed old-prefix blocker still blocks: %v", got)
	}
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: "example-site-9", Owner: new("x")}); r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
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

// The default list cuts a long text to its first clause or about 100
// characters, marked with an ellipsis; asking for text, or the CLI's whole
// result, gives it as written.
func TestTheDefaultListCutsALongTextAndFieldsGiveItWhole(t *testing.T) {
	_, v := queueVault(t)
	short := "Ship the store"
	clause := "Move the store behind one interface for every reader; then retire the old reader, the shim and the flag"
	words := strings.Repeat("ünïcode word ", 12)
	for _, s := range []string{short, clause, words} {
		add(t, v, "Example Tracker", "next", s)
	}
	texts := func(r *jsonx.Obj) []string {
		var out []string
		for i := range 3 {
			out = append(out, tk.Get(r, fmt.Sprintf("items.%d.text", i)).(string))
		}
		return out
	}
	lean := texts(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker"})))
	if lean[0] != short || lean[1] != "Move the store behind one interface for every reader…" {
		t.Fatalf("%q", lean)
	}
	if n := len([]rune(lean[2])); !strings.HasSuffix(lean[2], "…") || !strings.HasPrefix(words, strings.TrimSuffix(lean[2], "…")+" ") || n > 101 || n < 80 {
		t.Fatalf("%q (%d characters)", lean[2], n)
	}
	want := []string{short, clause, strings.TrimSpace(words)}
	for _, view := range []core.View{{Fields: []string{"id", "text"}}, {Full: true}} {
		if got := texts(mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", View: view}))); !slices.Equal(got, want) {
			t.Fatalf("%+v: %q", view, got)
		}
	}
}

// contains finds an item by a piece of its id as well as of its text.
func TestContainsMatchesAnItemsIDAsWellAsItsText(t *testing.T) {
	_, v := queueVault(t)
	for i := range 12 {
		add(t, v, "Example Tracker", "next", fmt.Sprintf("Item number %d", i+1))
	}
	add(t, v, "example-site", "next", "Mentions ex-1 in its text")
	for _, c := range []struct {
		contains string
		want     []string
	}{
		{"EX-1", []string{"ex-1", "ex-10", "ex-11", "ex-12", "example-site-1"}},
		{"12", []string{"ex-12"}},
		{"number 7", []string{"ex-7"}},
	} {
		r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true, Contains: c.contains, Fields: []string{"id"}}))
		if !slices.Equal(ids(r), c.want) {
			t.Errorf("contains %q: %v, want %v", c.contains, ids(r), c.want)
		}
	}
}

// A hook that knows only a worktree's branch finds its item in whichever
// queue holds it, by the exact branch and never a prefix of it.
func TestListByBranchFindsTheItemAcrossQueues(t *testing.T) {
	_, v := queueVault(t)
	branch := func(b string) func(*core.QueueArgs) {
		return func(a *core.QueueArgs) { a.Links = map[string]string{"branch": b} }
	}
	add(t, v, "Example Tracker", "next", "Other work", branch("pass/queue-cli-2"))
	want := add(t, v, "example-site", "next", "This work", branch("pass/queue-cli"))
	add(t, v, "example-site", "next", "No branch")
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Links: map[string]string{"branch": "pass/queue-cli"}}))
	if !slices.Equal(ids(r), []string{want}) {
		t.Fatal(jsonx.Compact(r))
	}
	r = mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Links: map[string]string{"branch": "pass/queue-cli"}}))
	if len(ids(r)) != 0 {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestQueueRefusesARestrictedQueue(t *testing.T) {
	root, v := queueVault(t)
	tk.Doc(t, filepath.Join(root, trackerQueue), "doc_id: queue-example-tracker\ntitle: Q\ndoc_type: architecture_note\nsystem: x\n"+
		"environment: other\nstatus: active\nsensitivity: restricted", "# Q\n\n## Open\n\n- [ ] Secret #queue/next  [id:: ex-1]\n\n## Closed\n")
	if r := core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x")}); r.Bool("ok") || !strings.Contains(r.Str("error"), "restricted") {
		t.Fatal(jsonx.Compact(r))
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true}))
	if len(ids(r)) != 0 || jsonx.Compact(tk.Get(r, "skipped_restricted")) != `["queue-example-tracker"]` {
		t.Fatal(jsonx.Compact(r))
	}
}

// An item's body names the doc holding its detail, written as a link to it;
// "" removes it.
func TestABodyNamesADoc(t *testing.T) {
	root, v := queueVault(t)
	blocker := add(t, v, "Example Tracker", "next", "Detailed", func(a *core.QueueArgs) { a.Body = new("project-example-tracker") })
	note := tk.Read(t, filepath.Join(root, trackerQueue))
	if !strings.Contains(note, "- [ ] Detailed ([[01 Projects/Example Tracker/index|notes]]) #queue/next  [id:: "+blocker+"]") {
		t.Fatal(note)
	}
	got := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true, Full: true}))
	if tk.Get(got, "items.0.body") != "project-example-tracker" {
		t.Fatal(jsonx.Compact(got))
	}
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: blocker, Body: new("")}))
	if got := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", All: true, Full: true})); tk.Has(got, "items.0.body") {
		t.Fatalf("an empty body stayed: %s", jsonx.Compact(got))
	}

	for want, a := range map[string]core.QueueArgs{
		"body \"note-missing\" names no doc in the vault": {Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x"), Body: new("note-missing")},
		"body \"nope\" must start with one of":            {Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x"), Body: new("nope")},
		"blocked_by \"task-unknown\" is not an item id":   {Action: "add", Project: "Example Tracker", Kind: "next", Text: new("x"), BlockedBy: []string{"task-unknown"}},
		"id \"task-unknown\" is not <prefix>-<number>":    {Action: "close", ID: "task-unknown", Resolution: "x"},
		"no queue holds an item \"ex-99\"":                {Action: "close", ID: "ex-99", Resolution: "x"},
	} {
		r := core.Queue(v, a)
		if r.Bool("ok") || !strings.Contains(r.Str("error"), want) || !r.Has("next_steps") {
			t.Errorf("%s: %s", want, jsonx.Compact(r))
		}
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

// A body is written as a link to its note, by file name where that is unique,
// and reads back as the doc_id; a link to no indexed note stays as written.
func TestABodyIsALinkToItsNote(t *testing.T) {
	root, v := queueVault(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/Example Tracker/Store Design.md"),
		"doc_id: project-store-design\ntitle: Store Design\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal", "# Store Design\n")
	id := add(t, v, "Example Tracker", "next", "Build the store", func(a *core.QueueArgs) { a.Body = new("project-store-design") })
	path := filepath.Join(root, trackerQueue)
	line := "- [ ] Build the store ([[Store Design|notes]]) #queue/next  [id:: " + id + "]" + filed
	if note := tk.Read(t, path); !strings.Contains(note, line+"\n") {
		t.Fatal(note)
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Full: true}))
	if tk.Get(r, "items.0.body") != "project-store-design" {
		t.Fatal(jsonx.Compact(r))
	}
	gone := strings.Replace(line, "Store Design|", "Deleted Note|", 1)
	tk.Write(t, path, strings.Replace(tk.Read(t, path), line, gone, 1))
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: id, Owner: new("worker")}))
	if note := tk.Read(t, path); !strings.Contains(note, "([[Deleted Note|notes]]) #queue/next #owner/worker  [id:: "+id+"]") {
		t.Fatal(note)
	}
	if r := core.Queue(v, core.QueueArgs{Action: "set", ID: id, Owner: new("a.b")}); r.Bool("ok") || !strings.Contains(r.Str("error"), "letters, digits") {
		t.Fatal(jsonx.Compact(r))
	}
}
