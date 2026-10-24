package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/clock"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
)

const logRel = "02 Infrastructure/Example Log.md"

const logFront = `---
doc_id: infra-example-log
title: Example Log
doc_type: architecture_note
system: Example
environment: other
status: active
sensitivity: internal
last_reviewed: 2025-01-01
tags: [log]
---
`

func logVault(t *testing.T, body string) (*core.Vault, string) {
	t.Helper()
	root := tk.FakeVault(t)
	tk.Write(t, filepath.Join(root, logRel), logFront+"\n"+body)
	return tk.Vault(root), root
}

const exampleLog = "# Example Log\n\n## Entries\n\n" +
	"- 2026-10-19 · worker · repo-a · lesson one\n" +
	"- 2026-10-19 · worker · repo-b · lesson two → fixed: abc123\n"

func entryTexts(t *testing.T, r *jsonx.Obj) []string {
	t.Helper()
	if !r.Bool("ok") {
		t.Fatal(jsonx.Compact(r))
	}
	raw, _ := r.Get("entries")
	var out []string
	for _, e := range raw.([]*jsonx.Obj) {
		out = append(out, e.Str("text"))
	}
	return out
}

func TestLogEntriesListThenCloseOne(t *testing.T) {
	v, root := logVault(t, exampleLog)
	list := core.LogArgs{Action: "list", OpenOnly: true, Contains: "repo-"}
	r := core.LogEntries(v, "infra-example-log", list)
	if got := entryTexts(t, r); !slices.Equal(got, []string{"2026-10-19 · worker · repo-a · lesson one"}) {
		t.Fatal(got)
	}
	// Lean by default: the line in the file and the text.
	entries, _ := r.Get("entries")
	if e := entries.([]*jsonx.Obj)[0]; !slices.Equal(e.Keys(), []string{"line", "text"}) || tk.Get(e, "line") != 17 {
		t.Fatal(jsonx.Compact(e))
	}

	c := core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "close", Match: "lesson one", Resolution: "fixed: def456 (repo-a)"})
	if !c.Bool("ok") || c.Str("text") != "2026-10-19 · worker · repo-a · lesson one → fixed: def456 (repo-a)" {
		t.Fatal(jsonx.Compact(c))
	}
	text := tk.Read(t, filepath.Join(root, logRel))
	if !strings.Contains(text, "\n- 2026-10-19 · worker · repo-a · lesson one → fixed: def456 (repo-a)\n") ||
		!strings.Contains(text, "last_reviewed: "+clock.TodayISO()+"\n") ||
		!strings.Contains(text, "- 2026-10-19 · worker · repo-b · lesson two → fixed: abc123\n") {
		t.Fatal(text)
	}
	if got := entryTexts(t, core.LogEntries(v, "infra-example-log", list)); len(got) != 0 {
		t.Fatal(got)
	}
	// Closed entries are still there to read.
	all := core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "list", Fields: []string{"text", "resolution"}})
	entries, _ = all.Get("entries")
	if tk.Get(all, "count") != 2 || entries.([]*jsonx.Obj)[0].Str("resolution") != "fixed: def456 (repo-a)" {
		t.Fatal(jsonx.Compact(all))
	}
}

func TestLogEntriesCloseNeedsExactlyOneOpenMatch(t *testing.T) {
	v, root := logVault(t, exampleLog+"- 2026-10-20 · worker · repo-a · lesson three\n")
	before := tk.Read(t, filepath.Join(root, logRel))
	r := core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "close", Match: "repo-a", Resolution: "fixed"})
	candidates, _ := r.Get("candidates")
	if r.Bool("ok") || len(candidates.([]*jsonx.Obj)) != 2 || !strings.Contains(r.Str("error"), "2 open entries") {
		t.Fatal(jsonx.Compact(r))
	}
	r = core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "close", Match: "lesson two", Resolution: "fixed"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "1 closed entries do") {
		t.Fatal(jsonx.Compact(r))
	}
	if tk.Read(t, filepath.Join(root, logRel)) != before {
		t.Fatal("a refused close wrote")
	}
}

func TestLogEntriesRefusesRestrictedAndReadOnlyCloses(t *testing.T) {
	v, root := logVault(t, exampleLog)
	r := core.LogEntries(v, "infra-local-pki", core.LogArgs{Action: "list", OpenOnly: true})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "restricted") {
		t.Fatal(jsonx.Compact(r))
	}
	tk.Write(t, filepath.Join(root, "05 Writeups", "Log.md"), strings.Replace(logFront, "infra-example-log", "infra-writeup-log", 1)+"\n"+exampleLog)
	v = tk.Vault(root)
	if got := entryTexts(t, core.LogEntries(v, "infra-writeup-log", core.LogArgs{Action: "list", OpenOnly: true})); len(got) != 1 {
		t.Fatal(got)
	}
	r = core.LogEntries(v, "infra-writeup-log", core.LogArgs{Action: "close", Match: "lesson one", Resolution: "fixed"})
	if r.Bool("ok") || !strings.Contains(r.Str("error"), "read-only") {
		t.Fatal(jsonx.Compact(r))
	}
}

func TestLogEntriesAnnotateRewritesThenReopens(t *testing.T) {
	v, root := logVault(t, exampleLog)
	path := filepath.Join(root, logRel)
	r := core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "annotate", Match: "lesson two", Resolution: "fixed: def456, with a guard"})
	if !r.Bool("ok") || r.Str("text") != "2026-10-19 · worker · repo-b · lesson two → fixed: def456, with a guard" {
		t.Fatal(jsonx.Compact(r))
	}
	if text := tk.Read(t, path); !strings.HasSuffix(text, "\n- 2026-10-19 · worker · repo-b · lesson two → fixed: def456, with a guard\n") ||
		!strings.Contains(text, "\n- 2026-10-19 · worker · repo-a · lesson one\n") || !strings.Contains(text, "last_reviewed: "+clock.TodayISO()+"\n") {
		t.Fatal(text)
	}
	r = core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "annotate", Match: "lesson two", Reopen: true})
	if !r.Bool("ok") || r.Str("text") != "2026-10-19 · worker · repo-b · lesson two" {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.HasSuffix(tk.Read(t, path), "\n- 2026-10-19 · worker · repo-b · lesson two\n") {
		t.Fatal(tk.Read(t, path))
	}
	open := entryTexts(t, core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "list", OpenOnly: true}))
	if len(open) != 2 {
		t.Fatal(open)
	}
}

func TestLogEntriesAnnotateRefusals(t *testing.T) {
	v, root := logVault(t, exampleLog+"- 2026-10-20 · worker · repo-b · lesson three → fixed: 999\n")
	before := tk.Read(t, filepath.Join(root, logRel))
	for want, a := range map[string]core.LogArgs{
		"2 closed entries contain":                                   {Match: "repo-b", Resolution: "x"},
		"no closed entry contains \"lesson one\"; 1 open entries do": {Match: "lesson one", Resolution: "x"},
		"annotate needs match":                                       {Resolution: "x"},
		"annotate needs resolution":                                  {Match: "lesson two"},
		"not both":                                                   {Match: "lesson two", Resolution: "x", Reopen: true},
		"reopen only applies to annotate":                            {Action: "close", Match: "lesson one", Resolution: "x", Reopen: true},
	} {
		if a.Action == "" {
			a.Action = "annotate"
		}
		r := core.LogEntries(v, "infra-example-log", a)
		if r.Bool("ok") || !strings.Contains(r.Str("error"), want) || !r.Has("next_steps") {
			t.Errorf("%s: %s", want, jsonx.Compact(r))
		}
	}
	if tk.Read(t, filepath.Join(root, logRel)) != before {
		t.Fatal("a refused annotate wrote")
	}
}
