package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
)

func TestADocReferenceTakesTheDocIDPrefixesAndNamesADocThatExists(t *testing.T) {
	f, ok := Labs.Field("superseded_by")
	if !ok || !f.DocRef || !slices.Equal(f.Prefixes, Labs.DocIDPrefixes) {
		t.Fatalf("%+v", f)
	}
	fm := func(next any) *jsonx.Obj {
		return jsonx.New("doc_id", "report-old", "title", "Old", "doc_type", "decision_record", "system", "x",
			"environment", "other", "status", "deprecated", "sensitivity", "internal", "superseded_by", next)
	}
	if e := Labs.Check(fm("example-new"), at); !has(e, "superseded_by 'example-new' must start with one of") {
		t.Fatal(e)
	}
	known := func(id string) bool { return id == "report-new" }
	for next, want := range map[string]string{
		"report-new":  "",
		"report-gone": "superseded_by 'report-gone' names no doc in the vault",
		"report-old":  "superseded_by 'report-old' names the doc itself",
	} {
		got := Labs.RefProblems(fm(next), known)
		if (want == "" && len(got) != 0) || (want != "" && !slices.Equal(got, []string{want})) {
			t.Errorf("%s: %v", next, got)
		}
	}
	if got := Labs.RefProblems(fm(nil), known); len(got) != 0 {
		t.Fatalf("an absent reference names nothing: %v", got)
	}
}

func TestFromContractKeepsADocReference(t *testing.T) {
	back, err := FromContract(Labs.ContractDict())
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := back.Field("superseded_by"); !ok || !f.DocRef {
		t.Fatalf("%+v", f)
	}
}

func TestProjectProblemsNameTheClosestFolders(t *testing.T) {
	projects := []string{"backlog", "example-site", "example-wiki", "tools"}
	fm := func(v any) *jsonx.Obj { return jsonx.New("related_projects", v) }
	if got := Labs.ProjectProblems(fm([]any{"example-wiki", "backlog"}), projects); len(got) != 0 {
		t.Fatalf("valid entries: %v", got)
	}
	got := Labs.ProjectProblems(fm([]any{"Example-Wiki", "tool"}), projects)
	if len(got) != 2 || !strings.Contains(got[0], "'Example-Wiki' names no project folder (closest: example-wiki,") ||
		!strings.Contains(got[1], "(closest: tools,") {
		t.Fatalf("invalid entries: %v", got)
	}
	if got := Labs.ProjectProblems(fm([]any{"anything"}), nil); len(got) != 0 {
		t.Fatalf("a vault without projects: %v", got)
	}
}
