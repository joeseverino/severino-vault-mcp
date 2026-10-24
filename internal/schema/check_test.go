package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

func has(errs []string, sub string) bool {
	return slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, sub) })
}

func TestCheckPassesAValidTask(t *testing.T) {
	if e := Labs.Check(jsonx.New("doc_id", "task-ship-the-thing", "title", "Ship the thing", "doc_type", "task", "status", "open")); len(e) != 0 {
		t.Fatal(e)
	}
}

func TestCheckEnforcesTheTaskLifecycle(t *testing.T) {
	if !has(Labs.Check(jsonx.New("doc_id", "task-x", "title", "x", "doc_type", "task", "status", "deprecated")), "status 'deprecated' not in") {
		t.Fatal("deprecated task accepted")
	}
}

func TestCheckRequiresTheFullFieldSetAndRejectsTaskStatusOnADoc(t *testing.T) {
	if !has(Labs.Check(jsonx.New("doc_id", "rb-x", "title", "x", "doc_type", "runbook", "status", "active")), "missing required field") {
		t.Fatal("missing fields accepted")
	}
	if !has(Labs.Check(jsonx.New("doc_id", "rb-x", "title", "x", "doc_type", "runbook", "system", "s", "environment", "homelab",
		"sensitivity", "internal", "status", "parked")), "status 'parked' not in") {
		t.Fatal("task status accepted on a runbook")
	}
}

func TestCheckHonorsACustomProfile(t *testing.T) {
	research := &Profile{
		Name: "research", DocTypes: []string{"paper", "task"}, Environments: []string{"lab"}, Statuses: []string{"draft", "active"},
		Sensitivities: []string{"public", "internal", "restricted"}, DocIDPrefixes: []string{"paper-", "task-"},
		RequiredFields: []string{"doc_id", "title", "doc_type", "status"}, TaskStatuses: []string{"open", "done"},
		TaskRequiredFields: []string{"doc_id", "title", "doc_type", "status"}, TaskFields: []string{"status", "created"},
	}
	fm := jsonx.New("doc_id", "paper-x", "title", "X", "doc_type", "paper", "status", "active")
	if e := research.Check(fm); len(e) != 0 {
		t.Fatal(e)
	}
	if e := Labs.Check(fm); !has(e, "doc_type 'paper' not in") || !has(e, "doc_id 'paper-x' must start with one of") {
		t.Fatal(e)
	}
}

// The enum wording lives only in this package: any other non-test source that
// spells it out is a second copy of the rule, and copies drift. Call NotIn or
// Profile.FieldError instead.
func TestEnumWordingHasOneHome(t *testing.T) {
	wording := regexp.MustCompile(`not in (\[|%s|%v)|must be one of`)
	repo, _ := filepath.Abs(filepath.Join("..", ".."))
	self, _ := filepath.Abs(".")
	var copies []string
	err := filepath.WalkDir(repo, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == self || (p != repo && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if wording.MatchString(line) {
				rel, _ := filepath.Rel(repo, p)
				copies = append(copies, fmt.Sprintf("%s:%d", rel, i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) > 0 {
		t.Fatalf("enum wording outside internal/schema: %v", copies)
	}
}
