package doctor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/doctor"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func errorsFor(fm *jsonx.Obj, p *schema.Profile) []string {
	r := &doctor.Report{}
	doctor.ValidateFrontmatter(r, "x.md", fm, p)
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Message)
	}
	return out
}

func has(errs []string, sub string) bool {
	return slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, sub) })
}

func TestValidTaskHasNoFindings(t *testing.T) {
	if e := errorsFor(jsonx.New("doc_id", "task-ship-the-thing", "title", "Ship the thing", "doc_type", "task", "status", "open"), schema.Labs); len(e) != 0 {
		t.Fatal(e)
	}
}

func TestTaskStatusLifecycleIsEnforced(t *testing.T) {
	if !has(errorsFor(jsonx.New("doc_id", "task-x", "title", "x", "doc_type", "task", "status", "deprecated"), schema.Labs), "status=") {
		t.Fatal("deprecated task accepted")
	}
}

func TestStandardDocRequiresTheFullFieldSetAndRejectsTaskStatus(t *testing.T) {
	if !has(errorsFor(jsonx.New("doc_id", "rb-x", "title", "x", "doc_type", "runbook", "status", "active"), schema.Labs), "missing required field") {
		t.Fatal("missing fields accepted")
	}
	if !has(errorsFor(jsonx.New("doc_id", "rb-x", "title", "x", "doc_type", "runbook", "system", "s", "environment", "homelab",
		"sensitivity", "internal", "status", "parked"), schema.Labs), "status=") {
		t.Fatal("task status accepted on a runbook")
	}
}

func TestValidateHonorsACustomProfile(t *testing.T) {
	research := &schema.Profile{
		Name: "research", DocTypes: []string{"paper", "task"}, Environments: []string{"lab"}, Statuses: []string{"draft", "active"},
		Sensitivities: []string{"public", "internal", "restricted"}, DocIDPrefixes: []string{"paper-", "task-"},
		RequiredFields: []string{"doc_id", "title", "doc_type", "status"}, TaskStatuses: []string{"open", "done"},
		TaskRequiredFields: []string{"doc_id", "title", "doc_type", "status"}, TaskFields: []string{"status", "created"},
	}
	fm := jsonx.New("doc_id", "paper-x", "title", "X", "doc_type", "paper", "status", "active")
	if e := errorsFor(fm, research); len(e) != 0 {
		t.Fatal(e)
	}
	if !has(errorsFor(fm, schema.Labs), "doc_type=") {
		t.Fatal("labs accepted a foreign doc type")
	}
}

func TestReportsMissingFrontmatterAndProposesAFix(t *testing.T) {
	root := tk.FakeVault(t)
	r := doctor.Validate(config.Load("", tk.Env(root)), schema.Labs, true)
	if r.OK() {
		t.Fatal("expected errors")
	}
	for _, f := range r.Findings {
		if f.RelativePath == "01 Projects/untagged.md" {
			if f.Message != "missing YAML frontmatter" || !strings.Contains(f.Proposal, "doc_id: project-untagged") || !strings.Contains(f.Proposal, "sensitivity: internal") {
				t.Fatalf("%+v", f)
			}
			return
		}
	}
	t.Fatal("no finding for the untagged file")
}

func TestReportsInvalidFrontmatterAndDuplicates(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Write(t, filepath.Join(root, "03 Runbooks", "Bad.md"), "---\ndoc_id: nope\ntitle: Bad\ndoc_type: made_up\nsystem: Bad\n"+
		"environment: other\nstatus: active\nsensitivity: internal\n---\n\n# Bad\n")
	tk.Write(t, filepath.Join(root, "03 Runbooks", "Duplicate.md"), "---\ndoc_id: rb-add-nginx-proxy-host\ntitle: Duplicate\n"+
		"doc_type: runbook\nsystem: Duplicate\nenvironment: other\nstatus: active\nsensitivity: internal\n---\n\n# Duplicate\n")
	r := doctor.Validate(config.Load("", tk.Env(root)), schema.Labs, false)
	byFile := map[string][]string{}
	for _, f := range r.Findings {
		byFile[f.RelativePath] = append(byFile[f.RelativePath], f.Message)
	}
	if !has(byFile["03 Runbooks/Bad.md"], "doc_id must start") || !has(byFile["03 Runbooks/Bad.md"], "doc_type='made_up'") ||
		!has(byFile["03 Runbooks/Duplicate.md"], "duplicate doc_id") {
		t.Fatal(byFile)
	}
}

func TestDoctorAppliesProfileDocumentSchema(t *testing.T) {
	root := tk.Dir(t)
	tk.Write(t, filepath.Join(root, "03 Runbooks", "bad.md"), "---\ndoc_id: rb-bad\ntitle: Bad\ndoc_type: runbook\nsystem: test\n"+
		"environment: other\nstatus: active\nsensitivity: internal\ncategory: x\nrenews: someday\n---\n\n# Bad\n")
	p := *schema.Labs
	p.DocumentSchemas = map[string]schema.DocumentSchema{"runbook": {Fields: map[string]schema.Field{
		"category": {Required: true, Kind: "string", Choices: []string{"a", "b"}},
		"renews":   {Required: true, Kind: "date"},
	}}}
	var msgs []string
	for _, f := range doctor.Validate(config.Load("", tk.Env(root)), &p, false).Findings {
		msgs = append(msgs, f.Message)
	}
	if !has(msgs, "category") || !has(msgs, "ISO date") {
		t.Fatal(msgs)
	}
}

func TestDoctorPassesOnTheSampleVault(t *testing.T) {
	wd, _ := os.Getwd()
	var out bytes.Buffer
	if code := doctor.Run(&out, config.Load("", tk.Env(filepath.Join(wd, "..", "..", "examples", "sample-vault"))), schema.Labs, false); code != 0 {
		t.Fatal(out.String())
	}
}
