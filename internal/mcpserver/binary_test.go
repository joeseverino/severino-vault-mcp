package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// fakeBinary points the server's executable at a file the test can replace,
// as an install of a newer binary does while a server runs.
func fakeBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(tk.Dir(t), "severino-vault-mcp")
	tk.Write(t, bin, "old build")
	was := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = was })
	return bin
}

// installOver replaces bin the way an install does: a new file renamed over it.
func installOver(t *testing.T, bin string) {
	t.Helper()
	next := bin + ".new"
	tk.Write(t, next, "new build, longer")
	if err := os.Rename(next, bin); err != nil {
		t.Fatal(err)
	}
}

// Once the binary under a running server is replaced, every tool that writes
// refuses with the restart it needs, and every tool that only reads still
// answers, marked so the answer isn't taken as the new binary's.
func TestAReplacedBinaryStopsWritesAndMarksReads(t *testing.T) {
	reg, root := labsOnly(t)
	tk.MultisectionDoc(t, root)
	bin := fakeBinary(t)
	s := connect(t, reg)
	registered := names(tools(t, s))
	write := map[string]any{"action": "add", "project": "backlog", "kind": "next", "text": "Before the install"}
	if r, isErr := call(t, s, "queue", write); isErr || !r.Bool("ok") || r.Has("warning") {
		t.Fatalf("before the install: %s", jsonx.Compact(r))
	}
	if r, _ := call(t, s, "find", map[string]any{"query": "backup"}); r.Has("warning") {
		t.Fatalf("before the install: %s", jsonx.Compact(r))
	}

	installOver(t, bin)
	queueNote := filepath.Join(root, "07 Backlog", "Queue.md")
	before := tk.Read(t, queueNote)
	writes := map[string]map[string]any{
		"queue":           {"action": "add", "project": "backlog", "kind": "next", "text": "After the install"},
		"set_frontmatter": {"doc_id": "rb-backup-ops", "title": "Renamed"},
	}
	for _, name := range coreTools {
		if _, given := writes[name]; !given && !readOnly(name) {
			writes[name] = map[string]any{}
		}
	}
	for name, args := range writes {
		r, isErr := call(t, s, name, args)
		if !isErr || r.Bool("ok") || !strings.Contains(r.Str("error"), "replaced") || !strings.Contains(r.Str("error"), "restart") {
			t.Errorf("%s after the install: %s", name, jsonx.Compact(r))
		}
		if p := stepProblems(r, registered); len(p) > 0 {
			t.Errorf("%s: %v", name, p)
		}
	}
	if tk.Read(t, queueNote) != before {
		t.Fatal("a refused write changed the queue note")
	}
	for name, args := range map[string]map[string]any{
		"find":       {"query": "backup"},
		"read_doc":   {"doc_id": "rb-backup-ops"},
		"task_board": {},
	} {
		r, isErr := call(t, s, name, args)
		if isErr || !strings.Contains(r.Str("warning"), "replaced") {
			t.Errorf("%s after the install: %s", name, jsonx.Compact(r))
		}
	}
	if r, _ := call(t, s, "read_doc", map[string]any{"doc_id": "rb-backup-ops"}); r.Str("doc_id") != "rb-backup-ops" {
		t.Fatalf("a read still answers: %s", jsonx.Compact(r))
	}
}

// A binary rewritten in place keeps its inode; its size or mtime still tells.
func TestABinaryRewrittenInPlaceCountsAsReplaced(t *testing.T) {
	bin := fakeBinary(t)
	w := watchBinary()
	if w == nil || w.replaced() {
		t.Fatal("the binary as started reads as replaced")
	}
	tk.Write(t, bin, "a rebuilt binary of another size")
	if !w.replaced() {
		t.Fatal("a rewrite in place reads as the same binary")
	}
}
