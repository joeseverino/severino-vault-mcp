package mcpserver

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/core"
	"github.com/joeseverino/severino-vault-mcp/internal/frontmatter"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/schema"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
	"github.com/joeseverino/severino-vault-mcp/internal/vaults"
)

// labsAndEdu is a registry of both built-in vaults, the edu one empty.
func labsAndEdu(t *testing.T) (*vaults.Registry, string) {
	root := tk.FakeVault(t)
	env := tk.Env(root, "SVMC_EDU_CONFIG", eduConfig(t, root))
	return vaults.Build(context.Background(), env, io.Discard), root
}

func propKeys(tool map[string]any) []string {
	var out []string
	for k := range tool["properties"].(map[string]any) {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// The frontmatter tools' arguments are the vaults' declared fields and
// nothing else: a field cannot be declared without becoming settable, or
// settable without being declared.
func TestFrontmatterToolArgumentsAreTheProfilesFields(t *testing.T) {
	reg, _ := labsAndEdu(t)
	got := tools(t, connect(t, reg))
	set := []string{"relative_path", "vault"}
	create := []string{"body", "doc_id", "heading", "include_restricted", "mode", "relative_path", "vault"}
	for _, f := range schema.Union(schema.Labs, schema.Education) {
		switch {
		case f.IsList():
			set = append(set, f.Name, "add_"+f.Name, "remove_"+f.Name)
			create = append(create, f.Name)
		case f.Touch:
			set = append(set, f.Name, "touch_"+f.Name)
			create = append(create, f.Name)
		default:
			set = append(set, f.Name)
			if f.Name != "doc_id" {
				create = append(create, f.Name)
			}
		}
	}
	slices.Sort(set)
	slices.Sort(create)
	if keys := propKeys(got["set_frontmatter"].InputSchema.(map[string]any)); !slices.Equal(keys, set) {
		t.Fatalf("set_frontmatter arguments\n got %v\nwant %v", keys, set)
	}
	if keys := propKeys(got["write_doc"].InputSchema.(map[string]any)); !slices.Equal(keys, create) {
		t.Fatalf("write_doc arguments\n got %v\nwant %v", keys, create)
	}
}

// Every field each vault's profile declares is set through set_frontmatter,
// and the doc then validates.
func TestEveryDeclaredFieldIsSettableThroughTheServer(t *testing.T) {
	reg, _ := labsAndEdu(t)
	s := connect(t, reg)
	for _, c := range []struct {
		vault, rel string
		profile    *schema.Profile
	}{{"labs", "01 Projects/Example/index.md", schema.Labs}, {"edu", "03 Runbooks/Example.md", schema.Education}} {
		v := reg.Lookup(c.vault)
		full := filepath.Join(v.Config.VaultPath, c.rel)
		tk.Write(t, full, "---\ntitle: Example\n---\n\n# Example\n")
		tk.RefTargets(t, v.Config.VaultPath, "03 Runbooks", c.profile)
		for _, f := range c.profile.Settable() {
			want := tk.Sample(f)
			r, isErr := call(t, s, "set_frontmatter", map[string]any{"vault": c.vault, "relative_path": c.rel, f.Name: want})
			if isErr || !r.Bool("ok") {
				t.Fatalf("%s %s: %s", c.vault, f.Name, jsonx.Compact(r))
			}
			fm, err := frontmatter.Read(full)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := fm.Get(f.Name); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("%s %s: wrote %v, want %v", c.vault, f.Name, got, want)
			}
		}
		if _, problems, ok := core.CheckDoc(v, full); !ok || len(problems) > 0 {
			t.Fatalf("%s: %v", c.vault, problems)
		}
	}
}

// A field one vault declares is refused on a vault that does not.
func TestAnotherVaultsFieldIsRefused(t *testing.T) {
	reg, _ := labsAndEdu(t)
	r, _ := call(t, connect(t, reg), "set_frontmatter", map[string]any{"relative_path": "03 Runbooks/Add Nginx Proxy Host.md", "course": "CS6035"})
	if r.Bool("ok") || !strings.HasPrefix(r.Str("error"), "'course' is not a field of the labs profile") {
		t.Fatal(jsonx.Compact(r))
	}
}
