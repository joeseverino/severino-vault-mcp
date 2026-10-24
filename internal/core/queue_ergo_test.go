package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestAnUnknownIDPrefixListsThePrefixesAndNamesTheProjectsOwn(t *testing.T) {
	_, v := queueVault(t)
	add(t, v, "Example Tracker", "next", "Work")
	r := core.Queue(v, core.QueueArgs{Action: "close", ID: "example-tracker-1", Resolution: "x"})
	refusedWith(t, r, "no queue uses the id prefix", "ex, example-site", `its prefix is \"ex\"`)
}

func TestAnIDThatIsNotPrefixNumberNamesTheForm(t *testing.T) {
	_, v := queueVault(t)
	r := core.Queue(v, core.QueueArgs{Action: "set", ID: "ex-1 ex-2 pass/x", Status: "open"})
	refusedWith(t, r, "<prefix>-<number>", "example-3", "one id per call")
}

func TestCloseWithoutAResolutionShowsTheCLIForm(t *testing.T) {
	_, v := queueVault(t)
	id := add(t, v, "Example Tracker", "next", "Work")
	r := core.Queue(v, core.QueueArgs{Action: "close", ID: id})
	refusedWith(t, r, "queue close "+id+" --resolution")
}

func TestAQueueThatFailsValidationNamesTheSetFrontmatterThatRepairsIt(t *testing.T) {
	root, v := queueVault(t)
	tk.Doc(t, filepath.Join(root, trackerQueue), "doc_id: queue-example-tracker\ntitle: Q\nsystem: Example Tracker\nstatus: active",
		"# Q\n\n## Open\n\n## Closed\n")
	r := core.Queue(v, core.QueueArgs{Action: "add", Project: "Example Tracker", Kind: "next", Text: new("Work")})
	refusedWith(t, r, `set_frontmatter(relative_path=\"`+trackerQueue+`\", doc_type=\"queue\"`)
	if strings.Contains(jsonx.Compact(r), "the fields the error names") {
		t.Fatal(jsonx.Compact(r))
	}
}
