package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"
)

// config.Under is the one test of a path lying inside a folder. A
// strings.HasPrefix(rel, dir+"/") anywhere else is a second copy.
func TestOneUnderCheck(t *testing.T) {
	for _, s := range sources(t) {
		stringsName := s.importName("strings")
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || (s.rel == "internal/config/config.go" && fn.Name.Name == "Under") {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if ok && isPkgCall(call, stringsName, "HasPrefix") && len(call.Args) == 2 && endsWithSlash(call.Args[1]) {
					t.Errorf("%s:%d: %s tests a path against dir+\"/\"; call config.Under", s.rel, s.line(call), fn.Name.Name)
				}
				return true
			})
		}
	}
}

// endsWithSlash matches an expression built as x + "/".
func endsWithSlash(e ast.Expr) bool {
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	lit, ok := bin.Y.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	v, _ := strconv.Unquote(lit.Value)
	return v == "/"
}
