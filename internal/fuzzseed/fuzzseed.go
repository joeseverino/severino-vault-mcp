// Package fuzzseed supplies real markdown from the sample vault as fuzz seeds.
package fuzzseed

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleVault = "../../examples/sample-vault"

// Markdown returns the text of every markdown file in the sample vault.
func Markdown(tb testing.TB) []string {
	tb.Helper()
	var out []string
	err := filepath.WalkDir(sampleVault, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		raw, err := os.ReadFile(p) //nolint:gosec // fixed fixture tree
		if err != nil {
			return err
		}
		out = append(out, string(raw))
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return out
}
