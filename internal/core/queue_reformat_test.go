package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

const oldQueueFront = "doc_id: queue-example-tracker\ntitle: Example Tracker Queue\ndoc_type: queue\nsystem: Example Tracker\n" +
	"environment: other\nstatus: active\nsensitivity: internal\nlast_reviewed: 2026-01-01\nrelated_projects:\n  - Example Tracker\ntags:\n  - queue"

// A note written in the older field grammar, with a box ticked in Obsidian
// and a closed item, before queue_format existed.
const oldQueueBody = "# Example Tracker Queue\n\n## Open\n\n" +
	"- [ ] Build it  [kind:: delegated]  [owner:: worker]  [agent:: a1]  [branch:: pass/x]  [id:: ex-1]\n" +
	"- [x] Ticked by hand  [kind:: next]  [body:: project-example-site]  [alias:: task-old]  [id:: ex-2]  [dependsOn:: ex-1]  [completion:: 2026-10-22]\n" +
	"- [ ] mentions #queue/next  [kind:: follow-up]  [id:: ex-3]\n\n" +
	"## Closed\n\n" +
	"- [x] Shipped  [kind:: decision]  [owner:: main]  [resolution:: yes (Friday)]  [id:: ex-4]  ✅ 2026-10-20\n"

const newQueueBody = "# Example Tracker Queue\n\n## Open\n\n" +
	"- [ ] Build it #queue/delegated #owner/worker  [agent:: a1]  [branch:: pass/x]  [id:: ex-1]\n" +
	"- [x] Ticked by hand ([[01 Projects/example-site/index|notes]]) #queue/next  [alias:: task-old]  [id:: ex-2]  [dependsOn:: ex-1]  [completion:: 2026-10-22]\n" +
	"- [ ] mentions \\#queue/next #queue/follow-up  [id:: ex-3]\n\n" +
	"## Closed\n\n" +
	"- [x] Shipped #queue/decision #owner/main  [resolution:: yes (Friday)]  [id:: ex-4]  ✅ 2026-10-20\n"

func TestReformatRewritesEveryLineOnceAndThenNothing(t *testing.T) {
	root, v := queueVault(t)
	path := filepath.Join(root, trackerQueue)
	tk.Doc(t, path, oldQueueFront, oldQueueBody)
	before := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Status: "all", Full: true}))

	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", All: true}))
	if r.Int("count") != 1 || tk.Get(r, "changed.0.relative_path") != trackerQueue || tk.Get(r, "changed.0.lines") != 4 || r.Has("unparsed_lines") {
		t.Fatal(jsonx.Compact(r))
	}
	note := tk.Read(t, path)
	if !strings.Contains(note, "\nqueue_format: 1\n") {
		t.Fatalf("reformat did not stamp the format:\n%s", note)
	}
	if _, body, _ := strings.Cut(note, "\n---\n\n"); body != newQueueBody {
		t.Fatalf("reformatted as\n%s", note)
	}
	after := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker", Status: "all", Full: true}))
	if jsonx.Compact(after) != jsonx.Compact(before) {
		t.Fatalf("the items changed:\n%s\n%s", jsonx.Compact(before), jsonx.Compact(after))
	}

	// A second run, by project, writes nothing: not even last_reviewed.
	again := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", Project: "Example Tracker"}))
	if again.Int("count") != 0 || tk.Read(t, path) != note {
		t.Fatal(jsonx.Compact(again))
	}
}

func TestReformatDropsAliasesOnlyWhenAsked(t *testing.T) {
	root, v := queueVault(t)
	path := filepath.Join(root, trackerQueue)
	tk.Doc(t, path, oldQueueFront+"\nqueue_format: 1", newQueueBody)
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", All: true})); r.Int("count") != 0 || r.Has("aliases_dropped") {
		t.Fatal(jsonx.Compact(r))
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", All: true, DropAliases: true}))
	if r.Int("count") != 1 || tk.Get(r, "aliases_dropped") != 1 || tk.Get(r, "changed.0.lines") != 1 {
		t.Fatal(jsonx.Compact(r))
	}
	if note := tk.Read(t, path); strings.Contains(note, "[alias::") || !strings.Contains(note, "#queue/next  [id:: ex-2]  [dependsOn:: ex-1]") {
		t.Fatal(note)
	}
}

func TestReformatNamesItsQueuesAndSkipsRestricted(t *testing.T) {
	root, v := queueVault(t)
	if r := core.Queue(v, core.QueueArgs{Action: "reformat"}); r.Bool("ok") || !strings.Contains(r.Str("error"), "name the queue") {
		t.Fatal(jsonx.Compact(r))
	}
	if r := core.Queue(v, core.QueueArgs{Action: "reformat", All: true, Project: "Example Tracker"}); r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	path := filepath.Join(root, trackerQueue)
	tk.Doc(t, path, strings.Replace(oldQueueFront, "sensitivity: internal", "sensitivity: restricted", 1), oldQueueBody)
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", All: true}))
	if r.Int("count") != 0 || jsonx.Compact(tk.Get(r, "skipped_restricted")) != `["queue-example-tracker"]` {
		t.Fatal(jsonx.Compact(r))
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "[kind:: delegated]") {
		t.Fatal("a restricted queue was rewritten")
	}
}

// A note already in the current grammar but written before queue_format is
// stamped by reformat, which writes nothing else.
func TestReformatStampsTheFormatOnANoteWithoutIt(t *testing.T) {
	root, v := queueVault(t)
	path := filepath.Join(root, trackerQueue)
	tk.Doc(t, path, oldQueueFront, newQueueBody)
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", All: true}))
	if r.Int("count") != 1 || tk.Get(r, "changed.0.lines") != 0 {
		t.Fatal(jsonx.Compact(r))
	}
	note := tk.Read(t, path)
	if _, body, _ := strings.Cut(note, "\n---\n\n"); body != newQueueBody || !strings.Contains(note, "\nqueue_format: 1\n") {
		t.Fatal(note)
	}
	if again := mustOK(t, core.Queue(v, core.QueueArgs{Action: "reformat", All: true})); again.Int("count") != 0 || tk.Read(t, path) != note {
		t.Fatal(jsonx.Compact(again))
	}
}

// Reformat reads the older grammar it knows, and refuses a note holding an
// item line no grammar it knows reads, leaving every byte as it was.
func TestReformatRefusesALineItCannotRead(t *testing.T) {
	root, v := queueVault(t)
	path := filepath.Join(root, trackerQueue)
	body := strings.Replace(oldQueueBody, "\n\n## Closed", "\n- [ ] not an item at all\n\n## Closed", 1)
	tk.Doc(t, path, oldQueueFront, body)
	before := tk.Read(t, path)
	r := core.Queue(v, core.QueueArgs{Action: "reformat", All: true})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "'- [ ] not an item at all'") || !strings.Contains(jsonx.Compact(r), "(/mcp)") {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused reformat wrote the note")
	}
}
