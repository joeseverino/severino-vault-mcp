package core_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/fsx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

const hookLine = "- 2026-10-21 · hook · repo-c · appended mid-write\n"

// Each server rewrite of a log note keeps a line an append-only hook adds
// while it runs, whether the hook takes the note's lock or skips it.
func TestRewritesKeepALineAppendedMidWrite(t *testing.T) {
	writes := map[string]func(v *core.Vault) *jsonx.Obj{
		"section edit": func(v *core.Vault) *jsonx.Obj {
			return write.EditSection(v.Loader, logRel, "Entries", "- 2026-10-21 · worker · repo-d · added by section edit\n", true, schema.Labs)
		},
		"log close": func(v *core.Vault) *jsonx.Obj {
			return core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "close", Match: "lesson one", Resolution: "fixed: def456"})
		},
	}
	wants := map[string]string{
		"section edit": "added by section edit",
		"log close":    "lesson one → fixed: def456",
	}
	for name, run := range writes {
		for _, locked := range []bool{true, false} {
			label := name + map[bool]string{true: " / locking appender", false: " / appender without the lock"}[locked]
			t.Run(label, func(t *testing.T) {
				v, root := logVault(t, exampleLog)
				wait := tk.AppendDuringWrite(t, root, logRel, hookLine, locked)
				r := run(v)
				wait()
				if !r.Bool("ok") {
					t.Fatal(jsonx.Compact(r))
				}
				text := tk.Read(t, filepath.Join(root, logRel))
				if !strings.Contains(text, hookLine) {
					t.Fatalf("the appended line was lost:\n%s", text)
				}
				if !strings.Contains(text, wants[name]) {
					t.Fatalf("the server's edit was lost:\n%s", text)
				}
			})
		}
	}
}

// A note another process keeps locked fails the edit with the retry as its
// next step, and leaves the note alone.
func TestALockedNoteFailsTheEditWithARetry(t *testing.T) {
	v, root := logVault(t, exampleLog)
	prev := fsx.LockWait
	t.Cleanup(func() { fsx.LockWait = prev })
	fsx.LockWait = 20 * time.Millisecond
	release := tk.HoldLock(t, root, logRel)
	defer release()
	r := core.WriteDoc(v, core.WriteDocArgs{Mode: "append_section", RelativePath: logRel, Heading: new("Entries"), Body: "- x\n"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), fsx.ErrLocked.Error()) {
		t.Fatal(jsonx.Compact(r))
	}
	steps, _ := r.Get("next_steps")
	if s, ok := steps.([]string); !ok || len(s) != 1 || !strings.HasPrefix(s[0], `write_doc(mode="append_section", doc_id="infra-example-log") again`) {
		t.Fatal(jsonx.Compact(r))
	}
	if got := tk.Read(t, filepath.Join(root, logRel)); got != logFront+"\n"+exampleLog {
		t.Fatalf("a refused edit changed the note:\n%s", got)
	}
}
