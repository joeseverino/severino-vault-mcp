package fsx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// LockDir is the vault-relative directory that holds one lock file per note.
// It is hidden, and its files are not .md, so no index or validate run lists
// them.
const LockDir = ".locks"

// LockPath is the vault-relative lock file for the note at the vault-relative
// path rel: LockDir/<rel with every "/" replaced by "%">.lock. Any process can
// compute it from the note's path alone. Two notes whose paths map to the same
// name (a "%" in a file name) only share a lock, which costs some waiting and
// never a lost write.
func LockPath(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	return filepath.Join(LockDir, strings.ReplaceAll(rel, "/", "%")+".lock")
}

// ErrLocked reports a note whose lock stayed held past LockWait.
var ErrLocked = errors.New("note is locked by another writer")

// ErrChanged reports a note that kept changing, under a writer that skipped
// the lock, through every attempt to rewrite it.
var ErrChanged = errors.New("note kept changing while it was rewritten")

// LockWait bounds how long Lock waits for another holder, so a stuck
// appender fails a write instead of hanging the server.
var LockWait = 10 * time.Second

// BeforeReplace runs after Update has staged the new text and before it
// checks the note and renames over it. A test seam: tests append here to
// land a write inside the window.
var BeforeReplace = func(rel string) {}

const updateAttempts = 3

// Lock takes the exclusive flock(2) on p's lock file, the convention every
// writer of a vault note follows (docs/ai-tool-contract.md). The lock is on a
// sidecar file, never the note: a rewrite renames a new inode over the note,
// so a lock on the note would let a waiting appender write into the replaced
// file. The returned func releases it.
func (v *Vault) Lock(p string) (func(), error) {
	rel, err := v.Rel(p)
	if err != nil {
		return nil, err
	}
	if err := v.root.MkdirAll(LockDir, 0o755); err != nil {
		return nil, err
	}
	f, err := v.root.OpenFile(LockPath(rel), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd()) //nolint:gosec // a file descriptor fits an int
	deadline := time.Now().Add(LockWait)
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w: %s", ErrLocked, rel)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Update rewrites the note at p, the one way this server changes an existing
// file. Under p's lock it reads the note, passes its text to edit, and
// replaces the note with what edit returns; an edit that returns the text
// unchanged writes nothing. Just before the replace it reads the note again:
// if the bytes differ from those edit saw (a writer that skipped the lock),
// edit runs again on the new bytes, so nothing that writer added is dropped.
// After updateAttempts such rounds it gives up with ErrChanged. edit may run
// more than once and must derive everything from the text it is given.
func (v *Vault) Update(p string, edit func(text string) string) (wrote bool, err error) {
	rel, err := v.Rel(p)
	if err != nil {
		return false, err
	}
	unlock, err := v.Lock(rel)
	if err != nil {
		return false, err
	}
	defer unlock()
	raw, err := v.root.ReadFile(rel)
	if err != nil {
		return false, err
	}
	for range updateAttempts {
		text := pystr.Decode(raw)
		next := edit(text)
		if next == text {
			return false, nil
		}
		staged, err := v.stageSibling(rel, []byte(next), ".svmc-")
		if err != nil {
			return false, err
		}
		BeforeReplace(rel)
		now, err := v.root.ReadFile(rel)
		if err != nil {
			_ = v.root.Remove(staged)
			return false, err
		}
		if !bytes.Equal(now, raw) {
			_ = v.root.Remove(staged)
			raw = now
			continue
		}
		if err := v.root.Rename(staged, rel); err != nil {
			_ = v.root.Remove(staged)
			return false, err
		}
		return true, nil
	}
	return false, fmt.Errorf("%w: %s", ErrChanged, rel)
}
