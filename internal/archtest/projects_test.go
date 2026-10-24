package archtest

import (
	"go/ast"
	"testing"
)

// vault.ProjectFolders is the one lister of the projects folder. An
// os.ReadDir whose path names ProjectsDir anywhere else is a second copy.
func TestOneProjectLister(t *testing.T) {
	for _, s := range sources(t) {
		osName := s.importName("os")
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || (s.rel == "internal/vault/vault.go" && fn.Name.Name == "ProjectFolders") {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if ok && isPkgCall(call, osName, "ReadDir") && len(call.Args) == 1 && namesIdent(call.Args[0], "ProjectsDir") {
					t.Errorf("%s:%d: %s lists the projects folder; call vault.ProjectFolders", s.rel, s.line(call), fn.Name.Name)
				}
				return true
			})
		}
	}
}

// namesIdent reports whether expr mentions the identifier name, bare or as a
// package selector.
func namesIdent(expr ast.Expr, name string) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
