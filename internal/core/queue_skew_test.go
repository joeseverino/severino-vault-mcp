package core_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/queue"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// A process of any version can meet a queue note written by another: these
// pin that a write never goes past a note it does not fully read, and that
// an id never repeats one written anywhere, parsed or not.

// futureLine is an item from a line format this binary does not know: its
// kind is in a shape no grammar here reads.
const futureLine = "- [ ] Written by a newer binary {kind=next}  [id:: ex-2]"

func refusedWith(t *testing.T, r *jsonx.Obj, want ...string) {
	t.Helper()
	if r.Bool("ok") || !r.Has("next_steps") {
		t.Fatal(jsonx.Compact(r))
	}
	for _, w := range want {
		if !strings.Contains(jsonx.Compact(r), w) {
			t.Fatalf("refusal does not say %q: %s", w, jsonx.Compact(r))
		}
	}
}

func TestAWriteRefusesANoteWithAnItemLineItCannotRead(t *testing.T) {
	root, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Known")
	path := filepath.Join(root, trackerQueue)
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "\n\n## Closed", "\n"+futureLine+"\n\n## Closed", 1))
	before := tk.Read(t, path)
	for _, a := range []core.QueueArgs{
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Another")},
		{Action: "set", ID: id, Owner: new("worker")},
		{Action: "close", ID: id, Resolution: "example-tracker@abc1234"},
		{Action: "reformat", All: true},
	} {
		refusedWith(t, core.Queue(v, a), "{kind=next}", "reconnect it (/mcp) or upgrade the binary")
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused write changed the note")
	}
	// Reading still works, and says a line was left out.
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker"})); r.Int("unparsed_lines") != 1 {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestANoteInANewerQueueFormatIsNotWritten(t *testing.T) {
	root, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Known")
	path := filepath.Join(root, trackerQueue)
	note := tk.Read(t, path)
	current := fmt.Sprintf("\nqueue_format: %d\n", queue.Version)
	if !strings.Contains(note, current) {
		t.Fatalf("a created queue carries no queue_format:\n%s", note)
	}
	tk.Write(t, path, strings.Replace(note, current, fmt.Sprintf("\nqueue_format: %d\n", queue.Version+1), 1))
	before := tk.Read(t, path)
	for _, a := range []core.QueueArgs{
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Another")},
		{Action: "set", ID: id, Owner: new("worker")},
		{Action: "close", ID: id, Resolution: "example-tracker@abc1234"},
		{Action: "reformat", All: true},
	} {
		refusedWith(t, core.Queue(v, a), fmt.Sprintf("queue_format is %d and this binary knows format %d", queue.Version+1, queue.Version), "upgrade the binary")
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused write changed the note")
	}
}

// A write to a note from before queue_format stamps the format it wrote.
func TestAWriteStampsTheQueueFormat(t *testing.T) {
	root, v := queueVault(t)
	path := filepath.Join(root, trackerQueue)
	tk.Doc(t, path, oldQueueFront, "# Example Tracker Queue\n\n## Open\n\n- [ ] Known #queue/next  [id:: ex-1]\n\n## Closed\n")
	mustOK(t, core.Queue(v, core.QueueArgs{Action: "set", ID: "ex-1", Owner: new("worker")}))
	if note := tk.Read(t, path); !strings.Contains(note, fmt.Sprintf("\nqueue_format: %d\n", queue.Version)) {
		t.Fatal(note)
	}
}

// A note carrying an id on two item lines takes no write until a person
// renumbers one, so no write hands an id to a second item.
func TestANoteCarryingAnIDTwiceIsNotWritten(t *testing.T) {
	root, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "First")
	path := filepath.Join(root, trackerQueue)
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "\n\n## Closed", "\n- [ ] Second #queue/next  [id:: ex-1]\n\n## Closed", 1))
	refusedWith(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Third")}), "'ex-1' on more than one item line")
	refusedWith(t, core.Queue(v, core.QueueArgs{Action: "set", ID: "ex-1", Owner: new("worker")}), "'ex-1' on more than one item line")
}
