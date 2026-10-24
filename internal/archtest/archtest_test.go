// Package archtest holds the repository's source guards: each fact has one
// home, and these tests fail when a second copy appears outside it.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

// sources parses every non-test Go file in the repository, skipping hidden
// directories and testdata.
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
	var out []source
	err = filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
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
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}
