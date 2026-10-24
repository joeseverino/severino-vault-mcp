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
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func has(errs []string, sub string) bool {
	return slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, sub) })
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

// The note locks live outside the vault, so validate and the index never see them.
func TestNoteLocksAreNotVaultFiles(t *testing.T) {
	root := tk.FakeVault(t)
	cfg := config.Load("", tk.Env(root))
	before := doctor.Validate(cfg, schema.Labs, false)
	docs := len(tk.Loader(root).Index(true).Docs)
	for _, rel := range []string{"03 Runbooks/Duplicate.md", "01 Projects/untagged.md", "Root note.md"} {
		tk.HoldLock(t, root, rel)()
	}
	if _, err := os.Stat(filepath.Join(root, ".locks")); !os.IsNotExist(err) {
		t.Fatalf("the vault holds a lock folder: %v", err)
	}
	after := doctor.Validate(cfg, schema.Labs, false)
	if after.CheckedFiles != before.CheckedFiles || len(after.Findings) != len(before.Findings) {
		t.Fatalf("validate saw the locks: %d files, %d findings before; %d, %d after",
			before.CheckedFiles, len(before.Findings), after.CheckedFiles, len(after.Findings))
	}
	if got := len(tk.Loader(root).Index(true).Docs); got != docs {
		t.Fatalf("the index saw the locks: %d docs, then %d", docs, got)
	}
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
	if !has(byFile["03 Runbooks/Bad.md"], "doc_id \"nope\" must start") || !has(byFile["03 Runbooks/Bad.md"], "doc_type \"made_up\" not in") ||
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
