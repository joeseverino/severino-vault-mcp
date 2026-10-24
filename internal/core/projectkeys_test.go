package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/write"
)

const (
	trackerIndex = "01 Projects/Example Tracker/index.md"
	siteIndex    = "01 Projects/example-site/index.md"
)

// bareProjects is two project indexes with no queue keys yet.
func bareProjects(t *testing.T) (string, *core.Vault) {
	t.Helper()
	root := tk.Dir(t)
	for _, p := range []string{"Example Tracker", "example-site"} {
		tk.Doc(t, filepath.Join(root, "01 Projects", p, "index.md"),
			"doc_id: project-"+strings.ToLower(strings.ReplaceAll(p, " ", "-"))+"\ntitle: "+p+"\ndoc_type: architecture_note\nsystem: Example\n"+
				"environment: other\nstatus: active\nsensitivity: internal",
			"# "+p+"\n")
	}
	return root, tk.Vault(root)
}

func refused(t *testing.T, r *jsonx.Obj, want string) {
	t.Helper()
	if r.Bool("ok") || !strings.Contains(r.Str("error"), want) {
		t.Fatalf("want a refusal naming %q, got %s", want, jsonx.Compact(r))
	}
}

func TestSettingProjectKeysMapsACwdToTheProjectQueue(t *testing.T) {
	root, v := bareProjects(t)
	mustOK(t, core.SetFrontmatter(v, trackerIndex, write.With("queue_prefix", "ex").List("repos", write.ListOp{Set: []string{"example-tracker", "example-tracker-ext"}, HasSet: true})))
	cwd := filepath.Join(t.TempDir(), "example-tracker-ext")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	r := mustOK(t, core.Queue(v, core.QueueArgs{Action: "add", Cwd: cwd, Kind: "next", Text: new("Write the store")}))
	if r.Str("id") != "ex-1" || r.Str("project") != "Example Tracker" || r.Str("fallback") != "" {
		t.Fatal(jsonx.Compact(r))
	}
	if !strings.Contains(tk.Read(t, filepath.Join(root, trackerIndex)), "queue_prefix: ex") {
		t.Fatal("queue_prefix not written")
	}
}

func TestProjectRepoListOps(t *testing.T) {
	root, v := bareProjects(t)
	set := func(s write.Changes) { mustOK(t, core.SetFrontmatter(v, trackerIndex, s)) }
	set(write.With().List("repos", write.ListOp{Add: []string{"example-a", "example-b"}}))
	set(write.With().List("repos", write.ListOp{Remove: []string{"example-a"}, Add: []string{"example-c"}}))
	got := tk.Read(t, filepath.Join(root, trackerIndex))
	if !strings.Contains(got, "example-b") || !strings.Contains(got, "example-c") || strings.Contains(got, "example-a") {
		t.Fatalf("repos not edited:\n%s", got)
	}
}

func TestProjectKeysRefuseBadValues(t *testing.T) {
	_, v := bareProjects(t)
	cases := map[string]struct {
		set  write.Changes
		want string
	}{
		"slash":     {write.With().List("repos", write.ListOp{Add: []string{"owner/example-tracker"}}), "bare repository directory name"},
		"empty":     {write.With().List("repos", write.ListOp{Add: []string{""}}), "bare repository directory name"},
		"uppercase": {write.With("queue_prefix", "EX"), "queue_prefix"},
		"digit":     {write.With("queue_prefix", "1ex"), "queue_prefix"},
		"too long":  {write.With("queue_prefix", "abcdefghijklm"), "queue_prefix"},
		"dash":      {write.With("queue_prefix", "ex-a"), "queue_prefix"},
		"backlog":   {write.With("queue_prefix", "backlog"), "backlog"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			refused(t, core.SetFrontmatter(v, trackerIndex, c.set), c.want)
		})
	}
}

func TestProjectKeysAreUniqueAcrossProjects(t *testing.T) {
	_, v := bareProjects(t)
	mustOK(t, core.SetFrontmatter(v, trackerIndex, write.With("queue_prefix", "ex").List("repos", write.ListOp{Add: []string{"example-tracker"}})))
	refused(t, core.SetFrontmatter(v, siteIndex, write.With("queue_prefix", "ex")), "Example Tracker")
	refused(t, core.SetFrontmatter(v, siteIndex, write.With().List("repos", write.ListOp{Add: []string{"Example-Tracker"}})), "Example Tracker")
	mustOK(t, core.SetFrontmatter(v, siteIndex, write.With("queue_prefix", "site").List("repos", write.ListOp{Add: []string{"example-site"}})))
	if r := core.SetFrontmatter(v, trackerIndex, write.With("queue_prefix", "ex")); !r.Bool("ok") {
		t.Fatalf("re-setting a project's own prefix is not a clash: %s", jsonx.Compact(r))
	}
}

func TestProjectKeysRefuseOtherDocsWithANextStep(t *testing.T) {
	v := tk.Vault(tk.FakeVault(t))
	r := core.SetFrontmatter(v, "03 Runbooks/Add Nginx Proxy Host.md", write.With("queue_prefix", "ex"))
	refused(t, r, "only applies to docs at '01 Projects/*/index.md'")
	if _, ok := r.Get("next_steps"); !ok {
		t.Fatalf("no next step: %s", jsonx.Compact(r))
	}
}
