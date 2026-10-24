// Package fsx holds durable file writes and the vault-root path checks every
// writer shares. Every read and mutation of a vault file goes through an
// os.Root, so a path (or a symlink swapped in after a check) can never reach
// outside the configured vault. An existing note changes only through
// Vault.Update, which holds the note's lock across read and replace.
package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joeseverino/severino-vault-mcp/internal/config"
	"github.com/joeseverino/severino-vault-mcp/internal/jsonx"
	"github.com/joeseverino/severino-vault-mcp/internal/pystr"
)

// ErrEscapes reports a path that resolves outside the vault root.
var ErrEscapes = errors.New("path escapes vault root")

// ErrExists reports a create that found the path taken.
var ErrExists = errors.New("file exists")

// Vault is an open handle on a vault root. Close it when done.
type Vault struct {
	root  *os.Root
	bases []string
}

// OpenVault opens the vault root at path.
func OpenVault(path string) (*Vault, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	bases := []string{abs}
	if real := Resolve(abs); real != abs {
		bases = append(bases, real)
	}
	return &Vault{root: root, bases: bases}, nil
}

// Close releases the root handle.
func (v *Vault) Close() error { return v.root.Close() }

// Rel returns p as a clean vault-relative path. p is either absolute (under
// the vault, by either its given or its resolved name) or already relative.
// A lexical escape yields ErrEscapes.
func (v *Vault) Rel(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return cleanRel(filepath.Clean(p), p)
	}
	for _, base := range v.bases {
		if r, err := filepath.Rel(base, p); err == nil {
			if rel, err := cleanRel(r, p); err == nil {
				return rel, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %s", ErrEscapes, p)
}

func cleanRel(rel, orig string) (string, error) {
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%w: %s", ErrEscapes, orig)
	}
	return rel, nil
}

// escaped reports whether err is an os.Root refusal to leave the root; the
// standard library exposes no sentinel for it.
func escaped(err error) bool {
	return errors.Is(err, ErrEscapes) || (err != nil && strings.Contains(err.Error(), "path escapes from parent"))
}

// Stat follows symlinks that stay inside the vault.
func (v *Vault) Stat(p string) (fs.FileInfo, error) {
	rel, err := v.Rel(p)
	if err != nil {
		return nil, err
	}
	return v.root.Stat(rel)
}

// IsDir reports whether p is a directory inside the vault.
func (v *Vault) IsDir(p string) bool {
	info, err := v.Stat(p)
	return err == nil && info.IsDir()
}

// ReadFile reads p.
func (v *Vault) ReadFile(p string) ([]byte, error) {
	rel, err := v.Rel(p)
	if err != nil {
		return nil, err
	}
	return v.root.ReadFile(rel)
}

// ReadText reads p with Python's text-mode decoding.
func (v *Vault) ReadText(p string) (string, error) {
	raw, err := v.ReadFile(p)
	if err != nil {
		return "", err
	}
	return pystr.Decode(raw), nil
}

// MkdirAll creates dir and its parents inside the vault.
func (v *Vault) MkdirAll(dir string) error {
	rel, err := v.Rel(dir)
	if err != nil {
		return err
	}
	return v.root.MkdirAll(rel, 0o755)
}

// Remove deletes p.
func (v *Vault) Remove(p string) error {
	rel, err := v.Rel(p)
	if err != nil {
		return err
	}
	return v.root.Remove(rel)
}

// Move moves the note at from to to, the one way this server relocates a
// file. With folder, the note's whole folder moves instead, to the folder
// that holds to, so files beside the note go with it. It holds both notes'
// locks (taken in path order, so two moves never wait on each other), creates
// the destination's missing parents, and never replaces anything: a
// destination that exists is ErrExists. A note moves by hard link then unlink,
// so a crash between the two leaves two copies, never none.
func (v *Vault) Move(from, to string, folder bool) error {
	src, err := v.Rel(from)
	if err != nil {
		return err
	}
	dst, err := v.Rel(to)
	if err != nil {
		return err
	}
	notes := []string{src, dst}
	switch {
	case LockPath(dst) == LockPath(src):
		notes = notes[:1]
	case LockPath(dst) < LockPath(src):
		notes = []string{dst, src}
	}
	for _, n := range notes {
		unlock, err := v.Lock(n)
		if err != nil {
			return err
		}
		defer unlock()
	}
	if folder {
		src, dst = filepath.Dir(src), filepath.Dir(dst)
	}
	if _, err := v.root.Lstat(dst); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := v.root.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if folder {
		return v.root.Rename(src, dst)
	}
	if err := v.root.Link(src, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
		return err
	}
	return v.root.Remove(src)
}

var tempSeq = func() func() string {
	n := uint64(os.Getpid()) << 32
	return func() string {
		n++
		return strconv.FormatUint(n, 36)
	}
}()

func (v *Vault) stageSibling(rel string, data []byte, infix string) (string, error) {
	dir, base := filepath.Split(rel)
	for range 100 {
		name := filepath.Join(dir, "."+base+infix+tempSeq())
		f, err := v.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = v.root.Remove(name)
			return "", werr
		}
		return name, nil
	}
	return "", fmt.Errorf("stage %s: no free temp name", rel)
}

// AtomicCreate durably creates p and never replaces an existing file:
// the staged file is hard-linked into place, an atomic create-if-absent.
func (v *Vault) AtomicCreate(p, text string) error {
	rel, err := v.Rel(p)
	if err != nil {
		return err
	}
	staged, err := v.stageSibling(rel, []byte(text), ".svmc-create-")
	if err != nil {
		return err
	}
	defer func() { _ = v.root.Remove(staged) }()
	if err := v.root.Link(staged, rel); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, p)
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
	outside := Fail(fmt.Sprintf("%s must stay inside configured vault root %s: %s", label, resolvedRoot, path))
	v, err := OpenVault(root)
	if err != nil {
		return outside
	}
	defer v.Close()
	info, err := v.Stat(path)
	if escaped(err) {
		return outside
	}
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
// indexed dir. On success it returns an open Vault (the caller closes it) and
// the file's resolved absolute path; every read and write of that file goes
// through the Vault.
func IndexedPath(cfg config.Config, relativePath string) (*Vault, string, *jsonx.Obj) {
	v, err := OpenVault(cfg.VaultPath)
	if err != nil {
		return nil, "", Fail("file not found: " + relativePath)
	}
	escapes := Fail("path escapes vault root: " + relativePath)
	rel, err := v.Rel(strings.TrimLeft(relativePath, "/"))
	if err != nil {
		_ = v.Close()
		return nil, "", escapes
	}
	info, err := v.root.Stat(rel)
	if escaped(err) {
		_ = v.Close()
		return nil, "", escapes
	}
	if err != nil || !info.Mode().IsRegular() {
		_ = v.Close()
		return nil, "", Fail("file not found: " + relativePath)
	}
	root := v.bases[len(v.bases)-1]
	full := Resolve(filepath.Join(root, rel))
	for _, sub := range cfg.IndexedDirs {
		if Within(filepath.Join(root, sub), full) {
			return v, full, nil
		}
	}
	_ = v.Close()
	return nil, "", Fail("path is outside the indexed dirs: " + pystr.ReprList(cfg.IndexedDirs))
}
