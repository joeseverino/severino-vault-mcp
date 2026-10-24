package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestLockPathIsComputableFromTheNotePathAlone(t *testing.T) {
	for rel, want := range map[string]string{
		"Log.md":                  ".locks/Log.md.lock",
		"02 Infra/Example Log.md": ".locks/02 Infra%Example Log.md.lock",
		"./a/b/../c/d.md":         ".locks/a%c%d.md.lock",
		// A "%" in a name can make two notes share a lock; sharing only waits.
		"a/b%c.md": ".locks/a%b%c.md.lock",
		"a%b/c.md": ".locks/a%b%c.md.lock",
	} {
		if got := fsx.LockPath(rel); got != want {
			t.Errorf("LockPath(%q) = %q, want %q", rel, got, want)
		}
	}
}

func noteVault(t *testing.T) (string, *fsx.Vault) {
	t.Helper()
	dir, _, v := sandbox(t)
	tk.Write(t, filepath.Join(dir, "note.md"), "# Note\n- one\n")
	return dir, v
}

func upper(text string) string { return strings.ToUpper(text) }

func TestUpdateHoldsTheLockSoALockingAppendLandsAfter(t *testing.T) {
	dir, v := noteVault(t)
	wait := tk.AppendDuringWrite(t, dir, "note.md", "- two\n", true)
	if wrote, err := v.Update("note.md", upper); err != nil || !wrote {
		t.Fatal(wrote, err)
	}
	wait()
	if got := tk.Read(t, filepath.Join(dir, "note.md")); got != "# NOTE\n- ONE\n- two\n" {
		t.Fatalf("got %q", got)
	}
}

func TestUpdateReappliesTheEditOverAnAppendThatSkippedTheLock(t *testing.T) {
	dir, v := noteVault(t)
	wait := tk.AppendDuringWrite(t, dir, "note.md", "- two\n", false)
	runs := 0
	wrote, err := v.Update("note.md", func(text string) string { runs++; return upper(text) })
	wait()
	if err != nil || !wrote {
		t.Fatal(wrote, err)
	}
	if got := tk.Read(t, filepath.Join(dir, "note.md")); got != "# NOTE\n- ONE\n- TWO\n" {
		t.Fatalf("got %q", got)
	}
	if runs != 2 {
		t.Fatalf("edit ran %d times, want once per version of the note", runs)
	}
}

func TestUpdateGivesUpOnANoteThatNeverHoldsStill(t *testing.T) {
	dir, v := noteVault(t)
	prev := fsx.BeforeReplace
	t.Cleanup(func() { fsx.BeforeReplace = prev })
	fsx.BeforeReplace = func(string) { tk.Append(t, dir, "note.md", "- more\n", false) }
	_, err := v.Update("note.md", upper)
	if !errors.Is(err, fsx.ErrChanged) {
		t.Fatalf("want ErrChanged, got %v", err)
	}
	got := tk.Read(t, filepath.Join(dir, "note.md"))
	if !strings.HasPrefix(got, "# Note\n- one\n- more\n") || strings.Contains(got, "NOTE") {
		t.Fatalf("a refused update changed the note: %q", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".svmc-") {
			t.Fatalf("staged file left behind: %s", e.Name())
		}
	}
}

func TestUpdateWaitsBoundedlyForAHeldLock(t *testing.T) {
	dir, v := noteVault(t)
	prev := fsx.LockWait
	t.Cleanup(func() { fsx.LockWait = prev })
	fsx.LockWait = 50 * time.Millisecond
	defer tk.HoldLock(t, dir, "note.md")()
	if _, err := v.Update("note.md", upper); !errors.Is(err, fsx.ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if got := tk.Read(t, filepath.Join(dir, "note.md")); got != "# Note\n- one\n" {
		t.Fatalf("got %q", got)
	}
}

func TestUpdateWritesNothingForAnUnchangedText(t *testing.T) {
	dir, v := noteVault(t)
	before, _ := os.Stat(filepath.Join(dir, "note.md"))
	wrote, err := v.Update("note.md", func(text string) string { return text })
	if err != nil || wrote {
		t.Fatal(wrote, err)
	}
	after, _ := os.Stat(filepath.Join(dir, "note.md"))
	if !os.SameFile(before, after) {
		t.Fatal("an unchanged update replaced the file")
	}
}
