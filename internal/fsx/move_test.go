package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
	tk "github.com/joeseverino/severino-vault-mcp/internal/testkit"
)

func TestMoveRelocatesANoteAndCreatesItsFolders(t *testing.T) {
	dir, v := noteVault(t)
	if err := v.Move("note.md", "a/b/renamed.md", false); err != nil {
		t.Fatal(err)
	}
	if got := tk.Read(t, filepath.Join(dir, "a", "b", "renamed.md")); got != "# Note\n- one\n" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.md")); !os.IsNotExist(err) {
		t.Fatalf("the source is still there: %v", err)
	}
}

func TestMoveNeverReplaces(t *testing.T) {
	dir, v := noteVault(t)
	tk.Write(t, filepath.Join(dir, "taken.md"), "taken\n")
	if err := v.Move("note.md", "taken.md", false); !errors.Is(err, fsx.ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	tk.Write(t, filepath.Join(dir, "src", "doc.md"), "doc\n")
	tk.Write(t, filepath.Join(dir, "dst", "src", "other.txt"), "other\n")
	if err := v.Move("src/doc.md", "dst/src/doc.md", true); !errors.Is(err, fsx.ErrExists) {
		t.Fatalf("want ErrExists for a taken folder, got %v", err)
	}
	for rel, want := range map[string]string{"taken.md": "taken\n", "note.md": "# Note\n- one\n", "src/doc.md": "doc\n"} {
		if got := tk.Read(t, filepath.Join(dir, rel)); got != want {
			t.Fatalf("%s changed: %q", rel, got)
		}
	}
}

func TestMoveWithFolderTakesTheFilesBesideTheNote(t *testing.T) {
	dir, _, v := sandbox(t)
	tk.Write(t, filepath.Join(dir, "Reports", "Audit 2026", "Audit.md"), "audit\n")
	tk.Write(t, filepath.Join(dir, "Reports", "Audit 2026", "check.py"), "print()\n")
	if err := v.Move("Reports/Audit 2026/Audit.md", "Archive/Audit 2026/Audit.md", true); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"Archive/Audit 2026/Audit.md", "Archive/Audit 2026/check.py"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("%s did not move: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Reports", "Audit 2026")); !os.IsNotExist(err) {
		t.Fatalf("the source folder is still there: %v", err)
	}
}

func TestMoveRefusesADestinationOutsideTheVault(t *testing.T) {
	dir, outside, v := sandbox(t)
	tk.Write(t, filepath.Join(dir, "note.md"), "note\n")
	for _, to := range []string{"../outside/note.md", "link-dir/note.md"} {
		if err := v.Move("note.md", to, false); err == nil {
			t.Errorf("Move to %q allowed", to)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "note.md")); err == nil {
		t.Fatal("a note landed outside the vault")
	}
	if got := tk.Read(t, filepath.Join(dir, "note.md")); got != "note\n" {
		t.Fatalf("the refused move changed the note: %q", got)
	}
}

// A writer holding either note's lock keeps the move waiting, so a move never
// lands under a rewrite or an append in progress.
func TestMoveWaitsForEitherNotesLock(t *testing.T) {
	dir, v := noteVault(t)
	prev := fsx.LockWait
	t.Cleanup(func() { fsx.LockWait = prev })
	fsx.LockWait = 50 * time.Millisecond
	for _, held := range []string{"note.md", "moved.md"} {
		release := tk.HoldLock(t, dir, held)
		if err := v.Move("note.md", "moved.md", false); !errors.Is(err, fsx.ErrLocked) {
			t.Fatalf("lock on %s held: want ErrLocked, got %v", held, err)
		}
		release()
	}
	if err := v.Move("note.md", "moved.md", false); err != nil {
		t.Fatal(err)
	}
}
