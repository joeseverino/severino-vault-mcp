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
		"--text=-dash-led goal: build the store", "--branch", "pass/store", "--owner", "worker")
	r := decode(t, out)
	if exit != 0 || r.Str("id") != "ex-1" || r.Str("project") != "Example Tracker" || tk.Get(r, "item.links.branch") != "pass/store" {
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
}
