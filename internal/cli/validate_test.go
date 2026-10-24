package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

const proposedRel = "01 Projects/example-wiki/Onboarding Review.md"

const proposedFront = `doc_id: report-onboarding-review
title: Onboarding Review
doc_type: architecture_note
system: Example Wiki
environment: other
status: proposed
sensitivity: internal
last_reviewed: 2026-10-19
tags: []`

func TestValidateReportsAnEnumProblemInSetFrontmattersWords(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, proposedRel), proposedFront, "# Review\n")
	want := proposedRel + ": status 'proposed' not in ['active', 'archived', 'deprecated', 'draft']\n"
	for _, path := range []string{proposedRel, filepath.Join(root, proposedRel)} {
		code, out, _ := run(t, root, "", "validate", path)
		if code != 1 || out != want {
			t.Fatalf("%s: %d %q", path, code, out)
		}
	}
	// set_frontmatter refuses the same value with the same words.
	status := "proposed"
	res := write.SetFrontmatter(tk.Loader(root), "03 Runbooks/Add Nginx Proxy Host.md", write.With("status", status), tk.Vault(root).Profile)
	if !strings.HasSuffix(want, ": "+res.Str("error")+"\n") {
		t.Fatal(jsonx.Compact(res))
	}
}

func TestWriteDocCreatesADocValidatePasses(t *testing.T) {
	root := tk.FakeVault(t)
	rel := "03 Runbooks/Rotate Example Token.md"
	args := []string{"write-doc", "--relative-path", rel, "--doc-id", "rb-rotate-example-token",
		"--title", "Rotate Example Token", "--doc-type", "runbook", "--system", "homelab"}
	code, out, _ := run(t, root, "## Steps\n\n1. Run `x`.\n", append(args, "--status", "proposed")...)
	if code != 1 || !strings.Contains(out, `status 'proposed' not in ['active', 'archived', 'deprecated', 'draft']`) {
		t.Fatal(code, out)
	}
	if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
		t.Fatal("refused create wrote the file")
	}
	if code, out, _ := run(t, root, "## Steps\n\n1. Run `x`.\n", args...); code != 0 || !strings.Contains(out, `"ok":true`) {
		t.Fatal(code, out)
	}
	if code, out, _ := run(t, root, "", "validate", rel); code != 0 || out != "" {
		t.Fatal(code, out)
	}
}

func TestValidatePassesValidDocsSilently(t *testing.T) {
	root := tk.FakeVault(t)
	code, out, _ := run(t, root, "", "validate", "03 Runbooks/Add Nginx Proxy Host.md", "02 Infrastructure/Local PKI.md")
	if code != 0 || out != "" {
		t.Fatal(code, out)
	}
}

func TestValidateIgnoresPathsNoVaultHolds(t *testing.T) {
	root := tk.FakeVault(t)
	elsewhere := filepath.Join(tk.Dir(t), "03 Runbooks", "Stray.md")
	tk.Write(t, elsewhere, "no frontmatter\n")
	tk.Write(t, filepath.Join(root, "03 Runbooks", "notes.txt"), "no frontmatter\n")
	tk.Write(t, filepath.Join(root, "03 Runbooks", "_draft.md"), "no frontmatter\n")
	tk.Write(t, filepath.Join(root, "03 Runbooks", "00 Templates", "T.md"), "no frontmatter\n")
	code, out, _ := run(t, root, "", "validate", elsewhere, "03 Runbooks/notes.txt", "03 Runbooks/_draft.md",
		"03 Runbooks/00 Templates/T.md", "00 Inbox/Daily Note/2026-06-19.md", "03 Runbooks/Absent.md", "../escape.md")
	if code != 0 || out != "" {
		t.Fatal(code, out)
	}
}

func TestValidateChecksRequiredFieldsDatesAndTheBlock(t *testing.T) {
	root := tk.FakeVault(t)
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Thin.md"), "doc_id: rb-thin\ntitle: Thin\ndoc_type: runbook\nsystem: x\nenvironment: other\nstatus: active\nlast_reviewed: 2026-13-01", "")
	tk.Doc(t, filepath.Join(root, "03 Runbooks", "Task.md"), "doc_id: task-x\ntitle: X\ndoc_type: task\nstatus: active\nenvironment: nowhere", "")
	code, out, _ := run(t, root, "", "validate", "03 Runbooks/Thin.md", "01 Projects/untagged.md", "03 Runbooks/Task.md")
	want := "03 Runbooks/Thin.md: missing required field: sensitivity\n" +
		"03 Runbooks/Thin.md: last_reviewed must be an ISO date (YYYY-MM-DD)\n" +
		"01 Projects/untagged.md: missing YAML frontmatter\n" +
		// Labs tracks work in the queue, so a task block is a doc with an
		// unknown doc_type, held to every doc rule.
		"03 Runbooks/Task.md: missing required field: system\n" +
		"03 Runbooks/Task.md: missing required field: sensitivity\n" +
		"03 Runbooks/Task.md: doc_type 'task' not in ['architecture_note', 'decision_record', 'deployment_guide', 'public_article_draft', 'queue', 'recovery_procedure', 'report', 'runbook', 'troubleshooting_guide']\n" +
		"03 Runbooks/Task.md: environment 'nowhere' not in ['adguard', 'cloudflare', 'homelab', 'local_mac', 'other', 'tailscale', 'unifi', 'vps', 'wordpress']\n"
	if code != 1 || out != want {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestValidateJSON(t *testing.T) {
	root := tk.FakeVault(t)
	code, out, _ := run(t, root, "", "validate", "--json", "01 Projects/untagged.md")
	if code != 1 || strings.TrimSpace(out) != `{"ok":false,"problems":[{"relative_path":"01 Projects/untagged.md","problem":"missing YAML frontmatter"}]}` {
		t.Fatal(code, out)
	}
	if code, out, _ := run(t, root, "", "validate", "--json", "/nowhere/x.md"); code != 0 || strings.TrimSpace(out) != `{"ok":true,"problems":[]}` {
		t.Fatal(code, out)
	}
	if code, _, errOut := run(t, root, "", "validate"); code != 2 || !strings.Contains(errOut, "required: path") {
		t.Fatal(code, errOut)
	}
}

func TestValidateUsesTheEduProfileForEduDocs(t *testing.T) {
	root := tk.Dir(t)
	edu := filepath.Join(root, "edu")
	tk.Doc(t, filepath.Join(edu, "GT", "index.md"), "doc_id: res-gt\ntitle: GT\ndoc_type: resource\nstatus: deprecated", "")
	cfg := filepath.Join(root, "edu.toml")
	tk.Write(t, cfg, "[vault]\npath = \""+edu+"\"\nindexed_dirs = [\"GT\"]\n")
	os.MkdirAll(filepath.Join(root, "labs"), 0o755)
	var out, errb bytes.Buffer
	env := tk.Env(root, "SVMC_EDU_CONFIG", cfg, "SVMC_VAULT_PATH", filepath.Join(root, "labs"))
	code := Main([]string{"validate", "GT/index.md"}, &Ctx{Env: env, Stdout: &out, Stderr: &errb})
	if code != 1 || !strings.HasPrefix(out.String(), "GT/index.md: status 'deprecated' not in ['active', 'archived', 'completed'") {
		t.Fatal(code, out.String())
	}
}
