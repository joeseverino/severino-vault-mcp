package testkit

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/fsx"
)

// Append adds line to the note at root/rel the way an append-only hook does:
// O_APPEND, and, when locked, under the note's lock per the documented
// convention (flock on fsx.LockFile(root, rel), held for the append).
func Append(t testing.TB, root, rel, line string, locked bool) {
	t.Helper()
	if locked {
		defer HoldLock(t, root, rel)()
	}
	f, err := os.OpenFile(filepath.Join(root, rel), os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // a test vault path
	if err != nil {
		t.Error(err)
		return
	}
	if _, err := f.WriteString(line); err != nil {
		t.Error(err)
	}
	if err := f.Close(); err != nil {
		t.Error(err)
	}
}

// HoldLock takes the lock on the note at root/rel as an outside process
// would, blocking until it is free, and returns its release.
func HoldLock(t testing.TB, root, rel string) (release func()) {
	t.Helper()
	lockPath, err := fsx.LockFile(root, rel)
	if err != nil {
		t.Error(err)
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Error(err)
		return func() {}
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644) //nolint:gosec // a test vault path
	if err != nil {
		t.Error(err)
		return func() {}
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // a file descriptor fits an int
		t.Error(err)
	}
	return func() { _ = lock.Close() }
}

// AppendDuringWrite lands one Append of line inside the next rewrite's
// window, after the server has read the note and before it replaces it, from
// another goroutine. A locked appender blocks on the lock the rewrite holds;
// the window stays open long enough that it would land there if the rewrite
// held none. Call the returned func before reading the note: it waits for the
// append to finish.
func AppendDuringWrite(t testing.TB, root, rel, line string, locked bool) (wait func()) {
	t.Helper()
	var once sync.Once
	done := make(chan struct{})
	prev := fsx.BeforeReplace
	t.Cleanup(func() { fsx.BeforeReplace = prev })
	fsx.BeforeReplace = func(string) {
		once.Do(func() {
			go func() {
				defer close(done)
				Append(t, root, rel, line, locked)
			}()
			select {
			case <-done:
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
	return func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the append never ran")
		}
	}
}
