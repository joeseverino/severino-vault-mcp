// Package fsx holds durable file writes and the vault-root path checks every
// writer shares, so a mutation can never land outside the configured vault.
package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

func stageSibling(path string, data []byte, prefix string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), prefix+"*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// WriteFile is a test seam over AtomicWrite.
var WriteFile = AtomicWrite

// AtomicWrite durably replaces path through a fsynced sibling temp file.
func AtomicWrite(path, text string) error {
	staged, err := stageSibling(path, []byte(text), "."+filepath.Base(path)+".svmc-")
	if err != nil {
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}

// ErrExists reports a create that found the path taken.
var ErrExists = errors.New("file exists")

// AtomicCreate durably creates path and never replaces an existing file:
// the staged file is hard-linked into place, an atomic create-if-absent.
func AtomicCreate(path, text string) error {
	staged, err := stageSibling(path, []byte(text), "."+filepath.Base(path)+".svmc-create-")
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	if err := os.Link(staged, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, path)
		}
		return err
	}
	return nil
}

// Resolve is Python's Path.resolve(strict=False): absolute, symlinks
// followed for the part that exists.
func Resolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	dir, base := filepath.Split(abs)
	dir = filepath.Clean(dir)
	if dir == abs || base == "" {
		return abs
	}
	return filepath.Join(Resolve(dir), base)
}

// Within reports whether p is root or inside it.
func Within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../"))
}

// Fail is the shared failure envelope.
func Fail(msg string) *jsonx.Obj { return jsonx.New("ok", false, "error", msg) }

// PathWithinRoot returns nil when path is inside root (and exists per kind:
// "dir", "file" or "any"), else a failure envelope.
func PathWithinRoot(root, path, label, kind string) *jsonx.Obj {
	resolvedRoot := Resolve(root)
	if !Within(resolvedRoot, Resolve(path)) {
		return Fail(fmt.Sprintf("%s must stay inside configured vault root %s: %s", label, resolvedRoot, path))
	}
	info, err := os.Stat(path)
	switch kind {
	case "dir":
		if err != nil || !info.IsDir() {
			return Fail(fmt.Sprintf("%s not found: %s", label, path))
		}
	case "file":
		if err != nil || !info.Mode().IsRegular() {
			return Fail(fmt.Sprintf("%s not found: %s", label, path))
		}
	}
	return nil
}

// IndexedPath resolves a vault-relative path that must be a file under an
// indexed dir.
func IndexedPath(cfg config.Config, relativePath string) (string, *jsonx.Obj) {
	root := Resolve(cfg.VaultPath)
	full := Resolve(filepath.Join(root, relativePath))
	if !Within(root, full) {
		return "", Fail("path escapes vault root: " + relativePath)
	}
	if info, err := os.Stat(full); err != nil || !info.Mode().IsRegular() {
		return "", Fail("file not found: " + relativePath)
	}
	for _, sub := range cfg.IndexedDirs {
		if Within(filepath.Join(root, sub), full) {
			return full, nil
		}
	}
	return "", Fail("path is outside the indexed dirs: " + pystr.ReprList(cfg.IndexedDirs))
}
