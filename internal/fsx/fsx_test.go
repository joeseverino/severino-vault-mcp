package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func sandbox(t *testing.T) (vaultDir, outside string, v *fsx.Vault) {
	t.Helper()
	base := tk.Dir(t)
	vaultDir = filepath.Join(base, "vault")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{vaultDir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tk.Write(t, filepath.Join(outside, "secret.md"), "outside\n")
	if err := os.Symlink(outside, filepath.Join(vaultDir, "link-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(vaultDir, "link-file.md")); err != nil {
		t.Fatal(err)
	}
	v, err := fsx.OpenVault(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return vaultDir, outside, v
}

func TestEveryOperationRefusesDotDotTraversal(t *testing.T) {
	_, _, v := sandbox(t)
	for _, p := range []string{"../outside/secret.md", "a/../../outside/secret.md", "/../outside/x"} {
		checkRefused(t, v, p)
	}
}

func TestEveryOperationRefusesASymlinkOutOfTheVault(t *testing.T) {
	_, outside, v := sandbox(t)
	for _, p := range []string{"link-dir/secret.md", "link-dir/new.md"} {
		checkRefused(t, v, p)
	}
	if got := tk.Read(t, filepath.Join(outside, "secret.md")); got != "outside\n" {
		t.Fatalf("outside file changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.md")); err == nil {
		t.Fatal("a file landed outside the vault")
	}
}

func TestALeafSymlinkOutIsReplacedNeverFollowed(t *testing.T) {
	dir, outside, v := sandbox(t)
	if _, err := v.ReadFile("link-file.md"); err == nil {
		t.Fatal("read followed a symlink out of the vault")
	}
	if err := v.AtomicWrite("link-file.md", "replaced"); err != nil {
		t.Fatal(err)
	}
	if got := tk.Read(t, filepath.Join(dir, "link-file.md")); got != "replaced" {
		t.Fatalf("got %q", got)
	}
	if got := tk.Read(t, filepath.Join(outside, "secret.md")); got != "outside\n" {
		t.Fatalf("outside file changed: %q", got)
	}
}

func checkRefused(t *testing.T, v *fsx.Vault, p string) {
	t.Helper()
	if _, err := v.ReadFile(p); err == nil {
		t.Errorf("ReadFile(%q) allowed", p)
	}
	if _, err := v.ReadText(p); err == nil {
		t.Errorf("ReadText(%q) allowed", p)
	}
	if _, err := v.Stat(p); err == nil {
		t.Errorf("Stat(%q) allowed", p)
	}
	if err := v.AtomicWrite(p, "x"); err == nil {
		t.Errorf("AtomicWrite(%q) allowed", p)
	}
	if err := v.AtomicCreate(p, "x"); err == nil {
		t.Errorf("AtomicCreate(%q) allowed", p)
	}
	if err := v.MkdirAll(p + "-dir"); err == nil {
		t.Errorf("MkdirAll(%q) allowed", p)
	}
	if err := v.Remove(p); err == nil {
		t.Errorf("Remove(%q) allowed", p)
	}
	if err := v.Rename(p, "moved.md"); err == nil {
		t.Errorf("Rename from %q allowed", p)
	}
	if err := v.Rename("moved.md", p); err == nil {
		t.Errorf("Rename to %q allowed", p)
	}
}

func TestAtomicWriteReplacesAndLeavesNoStagedFiles(t *testing.T) {
	dir, _, v := sandbox(t)
	p := filepath.Join(dir, "note.md")
	for _, text := range []string{"one", "two"} {
		if err := v.AtomicWrite(p, text); err != nil {
			t.Fatal(err)
		}
		if got := tk.Read(t, p); got != text {
			t.Fatalf("got %q", got)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".note.md.svmc-") {
			t.Fatalf("staged file left behind: %s", e.Name())
		}
	}
}

func TestAtomicCreateNeverReplaces(t *testing.T) {
	dir, _, v := sandbox(t)
	p := filepath.Join(dir, "sub", "new.md")
	if err := v.MkdirAll(filepath.Dir(p)); err != nil {
		t.Fatal(err)
	}
	if err := v.AtomicCreate(p, "first"); err != nil {
		t.Fatal(err)
	}
	err := v.AtomicCreate(p, "second")
	if !errors.Is(err, fsx.ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if got := tk.Read(t, p); got != "first" {
		t.Fatalf("got %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("staged file left behind: %v", entries)
	}
}

func TestSymlinkInsideTheVaultStillWorks(t *testing.T) {
	dir, _, v := sandbox(t)
	tk.Write(t, filepath.Join(dir, "real.md"), "real")
	if err := os.Symlink("real.md", filepath.Join(dir, "alias.md")); err != nil {
		t.Fatal(err)
	}
	if got, err := v.ReadText(filepath.Join(dir, "alias.md")); err != nil || got != "real" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestIndexedPathRefusesTraversalAndEscapingSymlinks(t *testing.T) {
	dir, _, v := sandbox(t)
	_ = v.Close()
	tk.Write(t, filepath.Join(dir, "Notes", "ok.md"), "ok")
	cfg := config.Config{VaultPath: dir, IndexedDirs: []string{"Notes"}}
	for _, rel := range []string{"../outside/secret.md", "link-dir/secret.md", "link-file.md", "Notes/../../outside/secret.md"} {
		got, full, errObj := fsx.IndexedPath(cfg, rel)
		if errObj == nil {
			got.Close()
			t.Fatalf("IndexedPath(%q) allowed %s", rel, full)
		}
		if !strings.Contains(errObj.Str("error"), "path escapes vault root: "+rel) {
			t.Errorf("IndexedPath(%q): %s", rel, errObj.Str("error"))
		}
	}
	got, full, errObj := fsx.IndexedPath(cfg, "Notes/ok.md")
	if errObj != nil {
		t.Fatal(errObj.Str("error"))
	}
	defer got.Close()
	if full != filepath.Join(dir, "Notes", "ok.md") {
		t.Fatal(full)
	}
	if _, _, e := fsx.IndexedPath(cfg, "Notes/missing.md"); e == nil || !strings.Contains(e.Str("error"), "file not found") {
		t.Fatal("missing file not reported")
	}
}

func TestPathWithinRootRefusesTraversalAndEscapingSymlinks(t *testing.T) {
	dir, _, v := sandbox(t)
	_ = v.Close()
	tk.Write(t, filepath.Join(dir, "in.md"), "in")
	for _, p := range []string{
		filepath.Join(dir, "..", "outside", "secret.md"),
		filepath.Join(dir, "link-dir", "secret.md"),
		filepath.Join(dir, "link-file.md"),
	} {
		e := fsx.PathWithinRoot(dir, p, "daily note", "any")
		if e == nil || !strings.Contains(e.Str("error"), "daily note must stay inside configured vault root") {
			t.Errorf("PathWithinRoot(%q) = %v", p, e)
		}
	}
	if e := fsx.PathWithinRoot(dir, filepath.Join(dir, "in.md"), "x", "file"); e != nil {
		t.Fatal(e.Str("error"))
	}
	if e := fsx.PathWithinRoot(dir, filepath.Join(dir, "absent.md"), "x", "file"); e == nil {
		t.Fatal("absent file accepted")
	}
}
