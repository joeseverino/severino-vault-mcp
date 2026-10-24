package fsx_test

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

// mutators are the os calls that change a file or open a second root. Outside
// fsx they would write a vault file without the note's lock and the re-check
// Vault.Update gives every rewrite.
var mutators = map[string]bool{
	"WriteFile": true, "Rename": true, "Create": true, "CreateTemp": true, "OpenFile": true,
	"Truncate": true, "Remove": true, "RemoveAll": true, "Link": true, "Symlink": true,
	"Chmod": true, "Chtimes": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true, "OpenRoot": true,
}

// outsideTheVault are the files allowed a direct os write, each with a path
// that is never a vault file.
var outsideTheVault = map[string]string{
	"internal/gate/gate.go": "the restricted-unlock audit log, an operator-configured path",
}

// Every file mutation in the server goes through fsx, so no writer can skip
// the note lock. Test files and testkit (which plays the outside appender)
// are exempt.
func TestOnlyFsxWritesFiles(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || rel == "internal/fsx" || rel == "internal/testkit" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		for _, call := range osMutations(t, p) {
			if _, ok := outsideTheVault[rel]; ok {
				seen[rel] = true
				continue
			}
			t.Errorf("%s: os.%s writes a file outside fsx; change a vault note through fsx.Vault.Update (or AtomicCreate for a new one)", rel, call)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range outsideTheVault {
		if !seen[rel] {
			t.Errorf("%s is allowed a direct write but makes none; drop it from outsideTheVault", rel)
		}
	}
}

func osMutations(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	osName := ""
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "os" {
			osName = "os"
			if imp.Name != nil {
				osName = imp.Name.Name
			}
		}
	}
	if osName == "" {
		return nil
	}
	var calls []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == osName && mutators[sel.Sel.Name] {
			calls = append(calls, sel.Sel.Name)
		}
		return true
	})
	return calls
}
