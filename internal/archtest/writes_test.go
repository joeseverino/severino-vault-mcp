package archtest

import (
	"go/ast"
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
// the note lock. testkit, which plays the outside appender, is exempt.
func TestOnlyFsxWritesFiles(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range sources(t) {
		if in(s.rel, "internal/fsx", "internal/testkit") {
			continue
		}
		osName := s.importName("os")
		if osName == "" {
			continue
		}
		ast.Inspect(s.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != osName || !mutators[sel.Sel.Name] {
				return true
			}
			if _, ok := outsideTheVault[s.rel]; ok {
				seen[s.rel] = true
				return true
			}
			t.Errorf("%s: os.%s writes a file outside fsx; change a vault note through fsx.Vault.Update (or AtomicCreate for a new one)", s.rel, sel.Sel.Name)
			return true
		})
	}
	for rel := range outsideTheVault {
		if !seen[rel] {
			t.Errorf("%s is allowed a direct write but makes none; drop it from outsideTheVault", rel)
		}
	}
}
