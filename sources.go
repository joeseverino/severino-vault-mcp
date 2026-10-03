// Package vaultmcp embeds this module's Go sources so the binary can
// fingerprint itself: tools compares an installed binary's fingerprint with
// the source tree's (`go run ./cmd/severino-vault-mcp --fingerprint`) to catch
// a stale install.
package vaultmcp

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"slices"
)

//go:embed go.mod go.sum sources.go cmd/*/*.go internal/*/*.go
var sources embed.FS

// Fingerprint hashes every embedded source file by path and content.
func Fingerprint() string {
	var paths []string
	_ = fs.WalkDir(sources, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	slices.Sort(paths)
	h := sha256.New()
	for _, p := range paths {
		data, _ := sources.ReadFile(p)
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
