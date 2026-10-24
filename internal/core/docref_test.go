package core_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// refVault is the fake vault with a log note and two docs that share the
// title "Shared Title".
func refVault(t *testing.T) (*core.Vault, string) {
	t.Helper()
	v, root := logVault(t, exampleLog)
	for _, id := range []string{"rb-shared-one", "rb-shared-two"} {
		tk.Write(t, filepath.Join(root, "03 Runbooks", id+".md"), "---\ndoc_id: "+id+"\ntitle: Shared Title\ndoc_type: runbook\nsystem: Example\n"+
			"environment: other\nstatus: active\nsensitivity: internal\nlast_reviewed: 2025-01-01\n---\n\n# Shared Title\n\n## Steps\n\nOne.\n")
	}
	return v, root
}

// docRefs are the three ways to name the nginx runbook, and the log note.
var docRefs = map[string][2]string{
	"doc_id": {"rb-add-nginx-proxy-host", "infra-example-log"},
	"title":  {"Add Nginx Proxy Host", "Example Log"},
	"path":   {"03 Runbooks/Add Nginx Proxy Host.md", logRel},
}

// docTools call each tool that names an existing doc with ref, and report
// the doc_id it acted on, or the failure.
var docTools = map[string]func(v *core.Vault, ref string) *jsonx.Obj{
	"read_doc": func(v *core.Vault, ref string) *jsonx.Obj {
		return core.ReadDoc(v, ref, core.ReadArgs{})
	},
	"write_doc": func(v *core.Vault, ref string) *jsonx.Obj {
		return core.WriteDoc(v, core.WriteDocArgs{Mode: "append_section", DocID: ref, Heading: new(""), Body: "Appended."})
	},
	"write_doc relative_path": func(v *core.Vault, ref string) *jsonx.Obj {
		return core.WriteDoc(v, core.WriteDocArgs{Mode: "append_section", RelativePath: ref, Heading: new(""), Body: "Appended."})
	},
	"set_frontmatter": func(v *core.Vault, ref string) *jsonx.Obj {
		return core.SetFrontmatter(v, ref, write.With("status", "draft"))
	},
	"move_doc": func(v *core.Vault, ref string) *jsonx.Obj {
		return core.MoveDoc(v, core.MoveArgs{DocID: ref, To: "02 Infrastructure/"})
	},
	"log_entries": func(v *core.Vault, ref string) *jsonx.Obj {
		return core.LogEntries(v, ref, core.LogArgs{Action: "list", OpenOnly: true})
	},
}

func TestEveryDocToolTakesADocIDTitleOrPath(t *testing.T) {
	for tool, call := range docTools {
		for kind, refs := range docRefs {
			t.Run(tool+"/"+kind, func(t *testing.T) {
				v, root := refVault(t)
				ref, want := refs[0], "rb-add-nginx-proxy-host"
				if tool == "log_entries" {
					ref, want = refs[1], "infra-example-log"
				}
				r := call(v, ref)
				if r.Failed() {
					t.Fatal(jsonx.Compact(r))
				}
				switch tool {
				case "read_doc", "log_entries":
					if r.Str("doc_id") != want {
						t.Fatalf("acted on %q: %s", r.Str("doc_id"), jsonx.Compact(r))
					}
				case "move_doc":
					if r.Str("doc_id") != want || r.Str("to") != "02 Infrastructure/Add Nginx Proxy Host.md" {
						t.Fatal(jsonx.Compact(r))
					}
				default:
					mark := "Appended."
					if tool == "set_frontmatter" {
						mark = "status: draft"
					}
					if text := tk.Read(t, filepath.Join(root, "03 Runbooks", "Add Nginx Proxy Host.md")); !strings.Contains(text, mark) {
						t.Fatalf("the nginx runbook lacks %q: %s", mark, jsonx.Compact(r))
					}
				}
			})
		}
	}
}

func TestEveryDocToolRefusesAnAmbiguousTitleWithItsCandidates(t *testing.T) {
	for tool, call := range docTools {
		t.Run(tool, func(t *testing.T) {
			v, root := refVault(t)
			r := call(v, "shared title")
			if r.Bool("ok") || r.Bool("found") {
				t.Fatalf("an ambiguous title resolved: %s", jsonx.Compact(r))
			}
			if !strings.Contains(r.Str("error"), "title of 2 docs") {
				t.Fatal(jsonx.Compact(r))
			}
			steps, _ := r.Get("next_steps")
			for _, id := range []string{"rb-shared-one", "rb-shared-two"} {
				if !slices.ContainsFunc(steps.([]string), func(s string) bool { return strings.Contains(s, `"`+id+`"`) }) {
					t.Errorf("no step names candidate %s: %s", id, jsonx.Compact(r))
				}
				if text := tk.Read(t, filepath.Join(root, "03 Runbooks", id+".md")); strings.Contains(text, "Appended.") || strings.Contains(text, "status: draft") {
					t.Errorf("a refused call changed %s", id)
				}
			}
			if tool == "read_doc" && len(tk.Hits(r, "candidates")) != 2 {
				t.Fatalf("read_doc lists no candidates: %s", jsonx.Compact(r))
			}
		})
	}
}

func TestALiveDocWinsANameAnArchivedOneShares(t *testing.T) {
	v, root := refVault(t)
	tk.Write(t, filepath.Join(root, "99 Archive", "Old Nginx.md"), "---\ndoc_id: rb-old-nginx\ntitle: Add Nginx Proxy Host\ndoc_type: runbook\nsystem: Example\n"+
		"environment: other\nstatus: archived\nsensitivity: internal\nlast_reviewed: 2025-01-01\n---\n\n# Old\n")
	if r := core.ReadDoc(v, "99 Archive/Old Nginx.md", core.ReadArgs{}); r.Str("doc_id") != "rb-old-nginx" {
		t.Fatalf("the archived doc is not indexed: %s", jsonx.Compact(r))
	}
	if r := core.ReadDoc(v, "Add Nginx Proxy Host", core.ReadArgs{}); r.Str("doc_id") != "rb-add-nginx-proxy-host" {
		t.Fatal(jsonx.Compact(r))
	}
}
