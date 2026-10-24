package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

// An id written anywhere in a queue note is never issued again, even on a
// line no grammar this binary knows reads: in another queue (whose own
// writes it does not block), and in the same note outside ## Open and
// ## Closed.
func TestANewIDNeverRepeatsOneWrittenInALineThatDoesNotParse(t *testing.T) {
	const front = "doc_id: queue-example-tracker\ntitle: Q\ndoc_type: queue\nsystem: x\n" +
		"environment: other\nstatus: active\nsensitivity: internal\ntags:\n  - queue"
	const unreadable = "- [ ] Written in a format this binary does not know {kind=next}  [id:: ex-1]\n"
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
