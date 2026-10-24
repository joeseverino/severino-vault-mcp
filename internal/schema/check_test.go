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

// at is a doc path no scoped field covers.
const at = "03 Runbooks/Example.md"

func TestCheckHoldsEachDeclaredFieldToItsRuleAndScope(t *testing.T) {
	doc := func(extra ...any) *jsonx.Obj {
		return jsonx.New("doc_id", "project-x", "title", "X", "doc_type", "architecture_note", "system", "x",
			"environment", "other", "status", "active", "sensitivity", "internal").Merge(jsonx.New(extra...))
	}
	index := "01 Projects/Example/index.md"
	if e := Labs.Check(doc("project_path", "~/Code/Archive/x", "repos", []any{"example-a"}, "queue_prefix", "ex",
		"aliases", []any{"X"}), index); len(e) != 0 {
		t.Fatal(e)
	}
	for _, c := range []struct {
		fm   *jsonx.Obj
		rel  string
		want string
	}{
		{doc("project_path", "~/x"), at, "project_path only applies to docs at '01 Projects/*/index.md'"},
		{doc("project_path", "Code/x"), index, "project_path must be an absolute path"},
		{doc("repos", []any{"owner/example"}), index, "bare repository directory name"},
		{doc("repos", "example"), index, "repos must be a list"},
		{doc("queue_prefix", "backlog"), index, "is reserved"},
		{doc("queue_prefix", "EX"), index, "lowercase letters and digits"},
		{doc("aliases", "X"), at, "aliases must be a list"},
		{doc("last_reviewed", "someday"), at, "ISO date"},
	} {
		if e := Labs.Check(c.fm, c.rel); !has(e, c.want) {
			t.Errorf("want %q, got %v", c.want, e)
		}
	}
}

// A report carries a kind, no other doc_type does, and reports live in
// Reports folders, which hold nothing else; the archive keeps a doc where
// it was archived from.
func TestAReportCarriesAKindAndLivesInAReportsFolder(t *testing.T) {
	doc := func(docType string, extra ...any) *jsonx.Obj {
		return jsonx.New("doc_id", "report-x", "title", "X", "doc_type", docType, "system", "x",
			"environment", "other", "status", "active", "sensitivity", "internal").Merge(jsonx.New(extra...))
	}
	report := doc(ReportDocType, ReportKindField, "audit")
	for _, rel := range []string{"01 Projects/Example/Reports/X.md", "Reports/X.md", "01 Projects/Example/Reports/X — 2026-10-01/X.md"} {
		if e := Labs.Check(report, rel); len(e) != 0 {
			t.Errorf("%s: %v", rel, e)
		}
	}
	for _, c := range []struct {
		fm   *jsonx.Obj
		rel  string
		want string
	}{
		{doc(ReportDocType), "Reports/X.md", "missing required field: kind"},
		{doc(ReportDocType, ReportKindField, "musing"), "Reports/X.md", "kind 'musing' not in"},
		{doc("decision_record", ReportKindField, "audit"), at, "kind only applies to doc_type ['report'], not 'decision_record'"},
		{report, "01 Projects/Example/X.md", "a doc_type 'report' doc lives in a 'Reports' folder"},
		{report, "01 Projects/Example/Reports.md", "a doc_type 'report' doc lives in a 'Reports' folder"},
		{doc("decision_record"), "01 Projects/Example/Reports/X.md", "a 'Reports' folder holds only doc_type 'report' docs, not 'decision_record'"},
		{doc("decision_record"), "Reports/Old/X.md", "holds only doc_type 'report' docs"},
	} {
		if e := Labs.Check(c.fm, c.rel); !has(e, c.want) {
			t.Errorf("%s: want %q, got %v", c.rel, c.want, e)
		}
	}
	archived := doc("decision_record", "status", ArchivedStatus)
	if e := Labs.CheckArchived(archived, "99 Archive"); len(e) != 0 {
		t.Errorf("an archived doc from a Reports folder: %v", e)
	}
	if e := Labs.CheckArchived(doc(ReportDocType, ReportKindField, "audit", "status", ArchivedStatus), "99 Archive"); len(e) != 0 {
		t.Errorf("an archived report: %v", e)
	}
}

func TestFromContractKeepsAProfilesOwnFields(t *testing.T) {
	back, err := FromContract(Labs.ContractDict())
	if err != nil || back.Fingerprint() != Labs.Fingerprint() {
		t.Fatalf("%v", err)
	}
	if back.DocTypeFolders[ReportDocType] != Labs.DocTypeFolders[ReportDocType] {
		t.Fatalf("doc_type folders: %v", back.DocTypeFolders)
	}
	if k, ok := back.Field(ReportKindField); !ok || !slices.Equal(k.DocTypes, []string{ReportDocType}) || !slices.Equal(Sorted(k.Choices), Sorted(ReportKinds)) {
		t.Fatalf("%+v", k)
	}
	c := Labs.ContractDict()
	c.Set("doc_type_folders", jsonx.New("musing", "Musings"))
	if _, err := FromContract(c); err == nil || !strings.Contains(err.Error(), "unknown doc_types") {
		t.Fatalf("a folder for an unknown doc_type: %v", err)
	}
	f, ok := back.Field("queue_prefix")
	if !ok || !f.Unique || f.Scope != projectIndex || !slices.Equal(f.Reserved, []string{"backlog"}) {
		t.Fatalf("%+v", f)
	}
}

func TestCheckPassesAValidTask(t *testing.T) {
	if e := Labs.Check(jsonx.New("doc_id", "task-ship-the-thing", "title", "Ship the thing", "doc_type", "task", "status", "open"), at); len(e) != 0 {
		t.Fatal(e)
	}
}

func TestCheckEnforcesTheTaskLifecycle(t *testing.T) {
	if !has(Labs.Check(jsonx.New("doc_id", "task-x", "title", "x", "doc_type", "task", "status", "deprecated"), at), "status 'deprecated' not in") {
		t.Fatal("deprecated task accepted")
	}
}

func TestCheckRequiresTheFullFieldSetAndRejectsTaskStatusOnADoc(t *testing.T) {
	if !has(Labs.Check(jsonx.New("doc_id", "rb-x", "title", "x", "doc_type", "runbook", "status", "active"), at), "missing required field") {
		t.Fatal("missing fields accepted")
	}
	if !has(Labs.Check(jsonx.New("doc_id", "rb-x", "title", "x", "doc_type", "runbook", "system", "s", "environment", "homelab",
		"sensitivity", "internal", "status", "parked"), at), "status 'parked' not in") {
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
	if e := research.Check(fm, at); len(e) != 0 {
		t.Fatal(e)
	}
	if e := Labs.Check(fm, at); !has(e, "doc_type 'paper' not in") || !has(e, "doc_id 'paper-x' must start with one of") {
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
