package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vault"
)

const statusRel = "03 Runbooks/Backup Ops.md"

// statusDoc is the multisection doc with a bullet list under each heading,
// the same bullet in both, as a status note carries.
func statusDoc(t *testing.T) (*core.Vault, string) {
	t.Helper()
	root := tk.FakeVault(t)
	tk.MultisectionDoc(t, root)
	path := filepath.Join(root, statusRel)
	text := strings.Replace(tk.Read(t, path), "current.\n", "current.\n\n- Waiting on conditions: `audit` once upstream is closed;\n- Shared bullet\n", 1)
	tk.Write(t, path, text+"\n- Shared bullet\n")
	return tk.Vault(root), path
}

func textArgs(heading *string, old, repl string) core.WriteDocArgs {
	return core.WriteDocArgs{Mode: "replace_text", DocID: "rb-backup-ops", Heading: heading, OldText: old, NewText: &repl}
}

func TestReplaceTextChangesOnePassageAndStampsLastReviewed(t *testing.T) {
	v, path := statusDoc(t)
	before := tk.Read(t, path)
	r := mustOK(t, core.WriteDoc(v, textArgs(nil, "`audit` once upstream is closed;", "`audit` now passes.")))
	after := tk.Read(t, path)
	if r.Str("mode") != "replace_text" || r.Str("doc_id") != "rb-backup-ops" || tk.Get(r, "wrote_bytes") != len(after) {
		t.Fatal(jsonx.Compact(r))
	}
	beforeHead, beforeBody := splitHead(t, before)
	afterHead, afterBody := splitHead(t, after)
	if afterHead != strings.Replace(beforeHead, "last_reviewed: 2026-05-01", "last_reviewed: "+clock.TodayISO(), 1) {
		t.Fatalf("frontmatter changed beyond last_reviewed:\n%s", afterHead)
	}
	if want := strings.Replace(beforeBody, "`audit` once upstream is closed;", "`audit` now passes.", 1); afterBody != want {
		t.Fatalf("%q", afterBody)
	}
}

func TestReplaceTextDeletesALine(t *testing.T) {
	v, path := statusDoc(t)
	_, before := splitHead(t, tk.Read(t, path))
	mustOK(t, core.WriteDoc(v, textArgs(nil, "- Waiting on conditions: `audit` once upstream is closed;\n", "")))
	if _, after := splitHead(t, tk.Read(t, path)); after != strings.Replace(before, "- Waiting on conditions: `audit` once upstream is closed;\n", "", 1) {
		t.Fatalf("%q", after)
	}
}

func TestReplaceTextRefusesAPassageThatIsNotUnique(t *testing.T) {
	v, path := statusDoc(t)
	before := tk.Read(t, path)
	r := core.WriteDoc(v, textArgs(nil, "- Shared bullet", "- Changed"))
	if r.Bool("ok") || tk.Get(r, "count") != 2 || !strings.Contains(r.Str("error"), "occurs 2 times") {
		t.Fatal(jsonx.Compact(r))
	}
	got, _ := r.Get("sections")
	if !slices.Equal(vault.CoerceList(got), []string{"Routine operations", "Troubleshooting"}) {
		t.Fatal(jsonx.Compact(r))
	}
	steps := nextSteps(r)
	if len(steps) != 3 || !strings.Contains(steps[1], `heading="Routine operations"`) || !strings.Contains(steps[2], `heading="Troubleshooting"`) {
		t.Fatal(steps)
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused edit wrote")
	}
	// Overlapping matches count as two, so the edit never picks one of them.
	mustOK(t, core.WriteDoc(v, textArgs(nil, "Overview line", "Overview x-x-x line")))
	if r := core.WriteDoc(v, textArgs(nil, "x-x", "y")); r.Bool("ok") || r.Int("count") != 2 {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestReplaceTextHeadingScopesTheMatch(t *testing.T) {
	v, path := statusDoc(t)
	_, before := splitHead(t, tk.Read(t, path))
	mustOK(t, core.WriteDoc(v, textArgs(new("Troubleshooting"), "- Shared bullet", "- Only here")))
	_, after := splitHead(t, tk.Read(t, path))
	if want := strings.Replace(before, "spikes.\n\n- Shared bullet", "spikes.\n\n- Only here", 1); after != want {
		t.Fatalf("%q", after)
	}
	// Text outside the named section is not found there, and the error says
	// where it is.
	r := core.WriteDoc(v, textArgs(new("Troubleshooting"), "Run the daily job", "x"))
	if r.Bool("ok") || tk.Get(r, "count") != 0 || !strings.Contains(r.Str("error"), "occurs 1 times elsewhere") {
		t.Fatal(jsonx.Compact(r))
	}
	// "" scopes to the text before the first heading.
	mustOK(t, core.WriteDoc(v, textArgs(new(""), "Overview line", "Summary line")))
	if _, body := splitHead(t, tk.Read(t, path)); !strings.HasPrefix(body, "Summary line before any heading.") {
		t.Fatalf("%q", body)
	}
}

func TestReplaceTextRefusesAMatchOnlyTheFrontmatterHas(t *testing.T) {
	v, path := statusDoc(t)
	before := tk.Read(t, path)
	r := core.WriteDoc(v, textArgs(nil, "status: active", "status: draft"))
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "only the frontmatter has it") {
		t.Fatal(jsonx.Compact(r))
	}
	if steps := nextSteps(r); len(steps) != 1 || !strings.HasPrefix(steps[0], "set_frontmatter(") {
		t.Fatal(steps)
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused edit wrote")
	}
}

func TestReplaceTextRefusals(t *testing.T) {
	v, path := statusDoc(t)
	before := tk.Read(t, path)
	body := textArgs(nil, "Overview", "x")
	body.Body = "x"
	noNew := textArgs(nil, "Overview", "")
	noNew.NewText = nil
	for want, a := range map[string]core.WriteDocArgs{
		`requires old_text`:                                            textArgs(nil, "", "x"),
		`requires new_text`:                                            noNew,
		`takes old_text and new_text, not body`:                        body,
		"new_text is the same as old_text":                             textArgs(nil, "Overview", "Overview"),
		"does not occur in 03 Runbooks/Backup Ops.md; copy it exactly": textArgs(nil, "Nowhere", "x"),
		`no heading "Goal"`:                                            textArgs(new("Goal"), "Overview", "x"),
		`only apply to mode "replace_text"`:                            {Mode: "replace_section", DocID: "rb-backup-ops", Heading: new("Troubleshooting"), Body: "x", OldText: "Check"},
		"infra-local-pki is restricted":                                {Mode: "replace_text", DocID: "infra-local-pki", OldText: "a", NewText: new("b")},
	} {
		r := core.WriteDoc(v, a)
		if r.Bool("ok") || !strings.Contains(r.Str("error"), want) || !r.Has("next_steps") {
			t.Errorf("%s: %s", want, jsonx.Compact(r))
		}
	}
	if tk.Read(t, path) != before {
		t.Fatal("a refused edit wrote")
	}
}
