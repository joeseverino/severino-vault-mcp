package fsx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/pystr"
)

// StateDir is the per-user state directory: $XDG_STATE_HOME/severino-vault-mcp,
// else ~/.local/state/severino-vault-mcp (where the restricted-unlock audit
// log defaults to, so there is one).
func StateDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "severino-vault-mcp"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "severino-vault-mcp"), nil
}

// LockDir is the absolute directory that holds one lock file per note of the
// vault at root: <StateDir>/locks/<id>, where id is the first 16 hex
// characters of the SHA-256 of the vault root's absolute path with symlinks
// resolved. The locks live outside the vault so a synced vault never uploads
// them.
func LockDir(root string) (string, error) {
	state, err := StateDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(Resolve(root)))
	return filepath.Join(state, "locks", hex.EncodeToString(sum[:])[:16]), nil
}

// LockName is the lock file name for the note at the vault-relative path rel:
// rel cleaned, every "/" replaced by "%", plus ".lock". Any process can
// compute it from the note's path alone. Two notes whose paths map to the same
// name (a "%" in a file name) only share a lock, which costs some waiting and
// never a lost write.
func LockName(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	return strings.ReplaceAll(rel, "/", "%") + ".lock"
}

// LockFile is the absolute lock file for the note at rel in the vault at root.
func LockFile(root, rel string) (string, error) {
	dir, err := LockDir(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LockName(rel)), nil
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
	path, err := LockFile(v.bases[0], rel)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // a lock file in the state dir
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
