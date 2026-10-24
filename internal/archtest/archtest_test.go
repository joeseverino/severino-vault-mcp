// Package archtest holds the repository's source guards: each fact has one
// home, and these tests fail when a second copy appears outside it.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/joeseverino/severino-vault-mcp/v3/internal/config"
)

// source is one non-test Go file of the server, parsed.
type source struct {
	rel  string // slash path from the repository root
	fset *token.FileSet
	file *ast.File
}

// importName is the name f uses for the import path, or "" when f does not
// import it.
func (s source) importName(path string) string {
	for _, imp := range s.file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == path {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return path[strings.LastIndex(path, "/")+1:]
		}
	}
	return ""
}

func (s source) line(n ast.Node) int { return s.fset.Position(n.Pos()).Line }

// goModIgnored are the directories go.mod's ignore directives name, as
// slash paths from the repository root: the go tool skips them, so the
// source guards do too. A line is `ignore ./dir` or one entry of an
// `ignore ( ... )` block; a trailing comment is dropped.
func goModIgnored(goMod string) []string {
	var out []string
	inBlock := false
	for line := range strings.SplitSeq(goMod, "\n") {
		line, _, _ = strings.Cut(line, "//")
		f := strings.Fields(line)
		switch {
		case inBlock && len(f) == 1 && f[0] == ")":
			inBlock = false
		case inBlock && len(f) == 1:
			out = append(out, path.Clean(f[0]))
		case len(f) == 2 && f[0] == "ignore" && f[1] == "(":
			inBlock = true
		case len(f) == 2 && f[0] == "ignore":
			out = append(out, path.Clean(f[1]))
		}
	}
	return out
}

// sources parses every non-test Go file in the repository, skipping hidden
// directories, testdata and what go.mod ignores.
func sources(t *testing.T) []source {
	t.Helper()
	return parseTree(t, false)
}

// testSources parses every Go test file the same way.
func testSources(t *testing.T) []source {
	t.Helper()
	return parseTree(t, true)
}

func parseTree(t *testing.T, tests bool) []source {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goMod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	ignored := goModIgnored(string(goMod))
	var out []source
	err = filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || slices.Contains(ignored, rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") != tests {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		out = append(out, source{rel: rel, fset: fset, file: f})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("found no sources")
	}
	return out
}

// in reports whether rel lies in one of the package directories.
func in(rel string, dirs ...string) bool {
	for _, d := range dirs {
		if config.Under(rel, d) {
			return true
		}
	}
	return false
}

func TestGoModIgnoreDirectivesParse(t *testing.T) {
	got := goModIgnored("module m\n\nignore ./var // scratch\nignore (\n\t./a/b\n\t./c\n)\nignorex ./no\n")
	if want := []string{"var", "a/b", "c"}; !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

// A scratch file under a directory go.mod ignores, even one that does not
// parse, never reaches the guards.
func TestSourceWalkSkipsWhatGoModIgnores(t *testing.T) {
	scratch := filepath.Join("..", "..", "var", "archtest-scratch")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratch) })
	for name, src := range map[string]string{"broken.go": "package x\nfunc (", "broken_test.go": "package x\nfunc ("} {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range append(sources(t), testSources(t)...) {
		if strings.HasPrefix(s.rel, "var/") {
			t.Fatalf("walked ignored %s", s.rel)
		}
	}
}
