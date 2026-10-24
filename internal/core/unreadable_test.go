package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/core"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/jsonx"
	tk "github.com/joeseverino/severino-vault-mcp/v3/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/v3/internal/write"
)

// deny makes dir unreadable until the test ends.
func deny(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// everyTool calls each tool that loads the index, once.
func everyTool(v *core.Vault) map[string]*jsonx.Obj {
	return map[string]*jsonx.Obj{
		"find":            find(v, "runbook"),
		"find text":       findBy(v, "runbook", "text"),
		"read_doc":        core.ReadDoc(v, "rb-add-nginx-proxy-host", core.ReadArgs{}),
		"set_frontmatter": core.SetFrontmatter(v, "03 Runbooks/Add Nginx Proxy Host.md", write.With("status", "draft")),
		"write_doc":       core.WriteDoc(v, core.WriteDocArgs{Mode: "create", RelativePath: "03 Runbooks/New.md", DocID: "rb-new"}),
		"move_doc":        core.MoveDoc(v, core.MoveArgs{DocID: "rb-add-nginx-proxy-host", To: "04 Reference"}),
		"move_folder":     core.MoveFolder(v, core.FolderArgs{From: "01 Projects/example-wiki", To: "01 Projects/other"}),
		"log_entries":     core.LogEntries(v, "infra-example-log", core.LogArgs{Action: "list"}),
		"queue":           core.Queue(v, core.QueueArgs{Action: "list"}),
	}
}

func TestAnUnreadableVaultFailsEveryTool(t *testing.T) {
	cases := map[string]func(t *testing.T) *core.Vault{
		"root denied": func(t *testing.T) *core.Vault {
			root := tk.FakeVault(t)
			deny(t, root)
			return tk.Vault(root)
		},
		"indexed folder denied": func(t *testing.T) *core.Vault {
			root := tk.FakeVault(t)
			deny(t, filepath.Join(root, "03 Runbooks"))
			return tk.Vault(root)
		},
		"nested folder denied": func(t *testing.T) *core.Vault {
			root := tk.FakeVault(t)
			sub := filepath.Join(root, "03 Runbooks", "sub")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			deny(t, sub)
			return tk.Vault(root)
		},
		"root missing": func(t *testing.T) *core.Vault {
			return tk.Vault(filepath.Join(tk.Dir(t), "gone"))
		},
		"root is a file": func(t *testing.T) *core.Vault {
			f := filepath.Join(tk.Dir(t), "file")
			tk.Write(t, f, "x")
			return tk.Vault(f)
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			v := mk(t)
			for tool, res := range everyTool(v) {
				if !res.Failed() {
					t.Errorf("%s answered %s", tool, jsonx.Compact(res))
					continue
				}
				if msg := res.Str("error"); !strings.Contains(msg, v.Config.VaultPath) || !strings.Contains(msg, "unreadable") {
					t.Errorf("%s: error does not name the path and the cause: %q", tool, msg)
				}
				if !res.Has("next_steps") {
					t.Errorf("%s: no next_steps", tool)
				}
			}
			if out := core.RenderDoc(v, "rb-add-nginx-proxy-host"); strings.Contains(out, "Not Found") {
				t.Errorf("resource says not found: %q", out)
			}
		})
	}
}

func TestAnEmptyVaultIsNotAFailure(t *testing.T) {
	v := tk.Vault(tk.Dir(t))
	res := find(v, "runbook")
	if res.Failed() {
		t.Fatal(jsonx.Compact(res))
	}
}

func TestAMissingIndexedFolderIsNotAFailure(t *testing.T) {
	root := tk.FakeVault(t)
	if err := os.RemoveAll(filepath.Join(root, "04 Reference")); err != nil {
		t.Fatal(err)
	}
	if res := find(tk.Vault(root), "runbook"); res.Failed() {
		t.Fatal(jsonx.Compact(res))
	}
}
