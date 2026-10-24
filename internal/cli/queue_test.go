package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func decode(t *testing.T, out string) *jsonx.Obj {
	t.Helper()
	if strings.Count(strings.TrimSpace(out), "\n") > 0 {
		t.Fatalf("not one compact line:\n%s", out)
	}
	v, err := jsonx.Decode([]byte(out))
	if err != nil {
		t.Fatal(err, out)
	}
	return v.(*jsonx.Obj)
}

// The calls a hook makes over a worker's life, from inside the worker's
// worktree: launch adds the item, hand-back returns it, landing closes it,
// and session start lists what is left.
func TestAHookTracksAWorkerThroughTheCLI(t *testing.T) {
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/Example Tracker/index.md"),
		"doc_id: project-example-tracker\ntitle: Example Tracker\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nqueue_prefix: ex\nrepos: [example-tracker]", "# Example Tracker\n")
	code := tk.Dir(t)
	worktree := filepath.Join(code, "example-tracker", ".claude", "worktrees", "agent-a1")
	tk.Write(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(code, "example-tracker", ".git", "worktrees", "agent-a1")+"\n")

	exit, out, _ := run(t, root, "", "queue", "add", "--cwd", worktree, "--kind", "delegated", "--agent", "a1",
		"--text=-dash-led goal: build the store", "--branch", "pass/store", "--owner", "worker", "--origin", "example-launcher",
		"--base", "main", "--owns", "internal/store,docs/store.md", "cmd/store")
	r := decode(t, out)
	if exit != 0 || r.Str("id") != "ex-1" || r.Str("project") != "Example Tracker" || tk.Get(r, "item.links.branch") != "pass/store" ||
		tk.Get(r, "item.base") != "main" || jsonx.Compact(tk.Get(r, "item.owns")) != `["internal/store","docs/store.md","cmd/store"]` || !tk.Has(r, "item.created") {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "set", "ex-1", "--agent", "a1", "--owns="); exit != 0 || tk.Has(decode(t, out), "item.owns") {
		t.Fatal(exit, out)
	}
	launcher := filepath.Join(code, "example-launcher")
	tk.Write(t, filepath.Join(launcher, ".git", "HEAD"), "ref: refs/heads/main\n")
	exit, out, _ = run(t, root, "", "queue", "list", "--cwd", launcher, "--include-origin", "--fields", "id", "project")
	if r := decode(t, out); exit != 0 || !strings.Contains(out, `"items":[{"id":"ex-1","project":"Example Tracker"}]`) || r.Str("warning") == "" {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "add", "--cwd", launcher, "--kind", "next", "--text", "x"); exit != 1 || !strings.Contains(out, "add_repos") {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "set", "--agent", "a1", "--status", "returned", "--commit", "abc1234,def5678")
	if exit != 0 || tk.Get(decode(t, out), "item.status") != "returned" {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "list", "--cwd", worktree, "--status", "returned", "--fields", "id", "links")
	if exit != 0 || !strings.Contains(out, `"items":[{"id":"ex-1","links":{"branch":"pass/store","commit":"abc1234,def5678"}}]`) {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "list", "--branch", "pass/store", "--fields", "id", "project")
	if exit != 0 || !strings.Contains(out, `"items":[{"id":"ex-1","project":"Example Tracker"}]`) {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "close", "--agent", "a1", "--resolution", "example-tracker@def5678")
	if exit != 0 || tk.Get(decode(t, out), "item.resolution") != "example-tracker@def5678" {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "list", "--all", "--limit", "5"); exit != 0 || jsonx.Compact(tk.Get(decode(t, out), "count")) != "0" {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "close", "ex-1", "--resolution", "again"); exit != 1 || !strings.Contains(out, `"next_steps"`) {
		t.Fatal(exit, out)
	}
	if exit, _, errb := run(t, root, "", "queue", "finish", "ex-1"); exit != 2 || !strings.Contains(errb, "invalid choice") {
		t.Fatal(exit, errb)
	}

	// One worker across two repos: each landing is recorded separately.
	exit, out, _ = run(t, root, "", "queue", "add", "--project", "Example Tracker", "--kind", "delegated", "--agent", "a2",
		"--text", "Span two repos", "--repos", "example-tracker,example-other")
	if exit != 0 || jsonx.Compact(tk.Get(decode(t, out), "item.repos")) != `["example-tracker","example-other"]` {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "close", "--agent", "a2", "--partial", "--resolution", "example-tracker@abc1234")
	if exit != 0 || tk.Get(decode(t, out), "item.status") != "open" {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "close", "--agent", "a2", "--partial", "--resolution", "example-other@def5678")
	if exit != 0 || tk.Get(decode(t, out), "item.status") != "closed" {
		t.Fatal(exit, out)
	}
}

// A brief that starts from a queued item takes it over in one call, and
// landing closes it by agent, leaving one closed item.
func TestALaunchClaimsAQueuedItemThenLandingClosesIt(t *testing.T) {
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/Example Tracker/index.md"),
		"doc_id: project-example-tracker\ntitle: Example Tracker\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nqueue_prefix: ex\nrepos: [example-tracker]", "# Example Tracker\n")
	for _, kind := range []string{"next", "decision"} {
		if exit, out, _ := run(t, root, "", "queue", "add", "--project", "Example Tracker", "--kind", kind, "--text", "Queued "+kind); exit != 0 {
			t.Fatal(exit, out)
		}
	}
	// A decision is not taken over as delegated work.
	if exit, out, _ := run(t, root, "", "queue", "set", "ex-2", "--kind", "delegated", "--agent", "a1"); exit != 1 || !strings.Contains(out, `"next_steps"`) {
		t.Fatal(exit, out)
	}
	// Another owner's claim changes nothing.
	run(t, root, "", "queue", "set", "ex-1", "--owner", "other")
	if exit, out, _ := run(t, root, "", "queue", "set", "ex-1", "--claim", "--owner", "worker", "--agent", "a1", "--kind", "delegated", "--branch", "pass/x"); exit != 1 {
		t.Fatal(exit, out)
	}
	exit, out, _ := run(t, root, "", "queue", "list", "--all", "--fields", "id", "kind", "agent", "owner")
	if exit != 0 || !strings.Contains(out, `{"id":"ex-1","kind":"next","owner":"other"}`) {
		t.Fatal(exit, out)
	}
	run(t, root, "", "queue", "set", "ex-1", "--owner=")
	exit, out, _ = run(t, root, "", "queue", "set", "ex-1", "--claim", "--owner", "worker", "--agent", "a1", "--kind", "delegated", "--branch", "pass/x")
	r := decode(t, out)
	if exit != 0 || tk.Get(r, "item.kind") != "delegated" || tk.Get(r, "item.agent") != "a1" || tk.Get(r, "item.owner") != "worker" || tk.Get(r, "item.links.branch") != "pass/x" {
		t.Fatal(exit, out)
	}
	// A second worker of the same type cannot take the item from the first.
	if exit, out, _ = run(t, root, "", "queue", "set", "ex-1", "--claim", "--owner", "worker", "--agent", "a9", "--kind", "delegated"); exit != 1 || decode(t, out).Str("agent") != "a1" {
		t.Fatal(exit, out)
	}
	// A released item goes back to the queue, and the next worker claims it.
	exit, out, _ = run(t, root, "", "queue", "set", "ex-1", "--release", "--agent", "a1")
	if r := decode(t, out); exit != 0 || tk.Get(r, "item.kind") != "next" || tk.Has(r, "item.agent") {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "set", "ex-1", "--claim", "--owner", "worker", "--agent", "a1", "--kind", "delegated"); exit != 0 {
		t.Fatal(exit, out)
	}
	// An agent holds any number of items; by agent alone, a single-item
	// verb then names them all and asks for one.
	if exit, out, _ = run(t, root, "", "queue", "add", "--project", "Example Tracker", "--kind", "delegated", "--agent", "a1", "--text", "Second piece"); exit != 0 {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "close", "--agent", "a1", "--resolution", "x"); exit != 1 || !strings.Contains(out, "ex-1") || !strings.Contains(out, "ex-3") {
		t.Fatal(exit, out)
	}
	// Closing a held item by id goes through and says who held it.
	if exit, out, _ = run(t, root, "", "queue", "close", "ex-3", "--status", "dropped", "--resolution", "folded into ex-1"); exit != 0 || !strings.Contains(decode(t, out).Str("warning"), "a1") {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "close", "--agent", "a1", "--resolution", "example-tracker@abc1234")
	if exit != 0 || tk.Get(decode(t, out), "item.status") != "closed" {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "list", "--all", "--status", "closed", "--fields", "id")
	if exit != 0 || !strings.Contains(out, `"items":[{"id":"ex-1"}]`) {
		t.Fatal(exit, out)
	}
}

// A body names the doc holding an item's detail, and --body= removes it.
func TestABodyIsSetAndRemovedThroughTheCLI(t *testing.T) {
	root := tk.Dir(t)
	tk.Doc(t, filepath.Join(root, "01 Projects/Example Tracker/index.md"),
		"doc_id: project-example-tracker\ntitle: Example Tracker\ndoc_type: architecture_note\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nqueue_prefix: ex", "# Example Tracker\n")
	exit, out, _ := run(t, root, "", "queue", "add", "--project", "Example Tracker", "--kind", "next", "--text", "Detailed", "--body=project-example-tracker")
	if r := decode(t, out); exit != 0 || tk.Get(r, "item.body") != "project-example-tracker" {
		t.Fatal(exit, out)
	}
	exit, out, _ = run(t, root, "", "queue", "set", "ex-1", "--body=")
	if r := decode(t, out); exit != 0 || r.Str("id") != "ex-1" || tk.Has(r, "item.body") || strings.Contains(out, `"body"`) {
		t.Fatal(exit, out)
	}
	if exit, out, _ = run(t, root, "", "queue", "close", "ex-1", "--resolution", "merged (#213)"); exit != 0 || tk.Get(decode(t, out), "item.resolution") != "merged (#213)" {
		t.Fatal(exit, out)
	}
}
