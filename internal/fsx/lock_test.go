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

func TestLockNameIsComputableFromTheNotePathAlone(t *testing.T) {
	for rel, want := range map[string]string{
		"Log.md":                  "Log.md.lock",
		"02 Infra/Example Log.md": "02 Infra%Example Log.md.lock",
		"./a/b/../c/d.md":         "a%c%d.md.lock",
		// A "%" in a name can make two notes share a lock; sharing only waits.
		"a/b%c.md": "a%b%c.md.lock",
		"a%b/c.md": "a%b%c.md.lock",
	} {
		if got := fsx.LockName(rel); got != want {
			t.Errorf("LockName(%q) = %q, want %q", rel, got, want)
		}
	}
}

// The worked example in docs/ai-tool-contract.md, with the id pinned.
func TestLockFileLivesInTheStateDirKeyedByTheVaultRoot(t *testing.T) {
	state := tk.Dir(t)
	t.Setenv("XDG_STATE_HOME", state)
	const id = "52f595614f5a30d2" // first 16 hex of sha256("/srv/example-vault")
	got, err := fsx.LockFile("/srv/example-vault", "Projects/Example/queue.md")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "severino-vault-mcp", "locks", id, "Projects%Example%queue.md.lock")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLockDirIsPerVaultAndFollowsSymlinks(t *testing.T) {
	base := tk.Dir(t)
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	a, _ := fsx.LockDir(real)
	b, _ := fsx.LockDir(link)
	c, _ := fsx.LockDir(base)
	if a != b || a == c {
		t.Fatalf("real %q, link %q, other %q", a, b, c)
	}
}

func TestStateDirFallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/x")
	if got, _ := fsx.StateDir(); got != "/home/x/.local/state/severino-vault-mcp" {
		t.Fatalf("got %q", got)
	}
}

func TestLockTakesItsFileOutsideTheVaultPrivately(t *testing.T) {
	dir, v := noteVault(t)
	unlock, err := v.Lock("note.md")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(filepath.Join(dir, ".locks")); !os.IsNotExist(err) {
		t.Fatalf("the vault holds a lock folder: %v", err)
	}
	file, _ := fsx.LockFile(dir, "note.md")
	info, err := os.Stat(filepath.Dir(file))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("lock dir: %v %v", info, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
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
