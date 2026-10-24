package schema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

const runbookPath = "03 Runbooks/Example.md"

func mentions(errs []string, sub string) bool {
	return slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, sub) })
}

func TestAProfileThatDeclaresTasksHoldsThemToTheirLifecycle(t *testing.T) {
	p := tk.TaskProfile()
	if e := p.Check(jsonx.New("doc_id", "task-ship-the-thing", "title", "Ship the thing", "doc_type", "task", "status", "open"), runbookPath); len(e) != 0 {
		t.Fatal(e)
	}
	if !mentions(p.Check(jsonx.New("doc_id", "task-x", "title", "x", "doc_type", "task", "status", "deprecated"), runbookPath), "status 'deprecated' not in") {
		t.Fatal("deprecated task accepted")
	}
}

// Labs and edu track work in the queue: a task block there is a doc with an
// unknown doc_type, held to the doc's rules.
func TestLabsAndEducationDeclareNoTasks(t *testing.T) {
	block := jsonx.New("doc_id", "task-x", "title", "x", "doc_type", "task", "status", "open")
	for _, p := range []*schema.Profile{schema.Labs, schema.Education} {
		if p.IsTask(block) || !mentions(p.Check(block, runbookPath), "doc_type 'task'") {
			t.Errorf("%s: %v", p.Name, p.Check(block, runbookPath))
		}
		if _, ok := p.Field("effort"); ok {
			t.Errorf("%s declares the task field effort", p.Name)
		}
	}
}
