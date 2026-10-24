package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

// An id written anywhere in a queue note is never issued again, even on a
// line that does not parse: in another queue (whose own
// writes it does not block), and in the same note outside ## Open and
// ## Closed.
func TestANewIDNeverRepeatsOneWrittenInALineThatDoesNotParse(t *testing.T) {
	const front = "doc_id: queue-example-tracker\ntitle: Q\ndoc_type: queue\nsystem: x\n" +
		"environment: other\nstatus: active\nsensitivity: internal\ntags:\n  - queue"
	const unreadable = "- [ ] Typed by hand {kind=next}  [id:: ex-1]\n"
	for name, notes := range map[string]map[string]string{
		"in another queue": {
			"07 Backlog/Queue.md": "# Backlog Queue\n\n## Open\n\n" + unreadable + "\n## Closed\n",
		},
		"in the same note": {
			trackerQueue: "# Example Tracker Queue\n\n## Open\n\n## Closed\n\n## Imported\n\n" + unreadable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, v := queueVault(t)
			for rel, body := range notes {
				id := "queue-example-tracker"
				if rel != trackerQueue {
					id = "queue-backlog"
				}
				tk.Doc(t, filepath.Join(root, rel), strings.Replace(front, "queue-example-tracker", id, 1), body)
			}
			r := core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("New work")})
			if !r.Bool("ok") || r.Str("id") != "ex-2" {
				t.Fatalf("issued an id already written in a queue note: %s", jsonx.Compact(r))
			}
		})
	}
}

// unparsedLine is item-shaped, but its kind is in a shape the grammar does
// not read.
const unparsedLine = "- [ ] Typed by hand {kind=next}  [id:: ex-2]"

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
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "\n\n## Closed", "\n"+unparsedLine+"\n\n## Closed", 1))
	before := tk.Read(t, path)
	for _, a := range []core.QueueArgs{
		{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Another")},
		{Action: "set", ID: id, Owner: new("worker")},
		{Action: "close", ID: id, Resolution: "example-tracker@abc1234"},
	} {
		refusedWith(t, core.Queue(v, a), "{kind=next}", "fix or remove the line in the note")
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused write changed the note")
	}
	// Reading still works, and says a line was left out.
	if r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "list", Project: "Example Tracker"})); r.Int("unparsed_lines") != 1 {
		t.Fatal(jsonx.Compact(r))
	}
}

// A note carrying an id on two item lines takes no write until a person
// renumbers one, so no write hands an id to a second item.
func TestANoteCarryingAnIDTwiceIsNotWritten(t *testing.T) {
	root, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "First")
	path := filepath.Join(root, trackerQueue)
	tk.Write(t, path, strings.Replace(tk.Read(t, path), "\n\n## Closed", "\n- [ ] Second #queue/next  [id:: ex-1]\n\n## Closed", 1))
	refusedWith(t, core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Third")}), `\"ex-1\" on more than one item line`)
	refusedWith(t, core.Queue(v, core.QueueArgs{Action: "set", ID: "ex-1", Owner: new("worker")}), `\"ex-1\" on more than one item line`)
}
