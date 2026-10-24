package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// jsonx.Obj.Has takes one key, so a dotted path handed to it is never
// present and a negative check on it always passes. A dotted path goes to
// testkit.Has, which walks it.
func TestNoDottedPathReachesObjHas(t *testing.T) {
	for _, s := range append(sources(t), testSources(t)...) {
		tk := s.importName("github.com/joeseverino/severino-vault-mcp/internal/testkit")
		ast.Inspect(s.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Has" {
				return true
			}
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && tk != "" && pkg.Name == tk {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if key, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(key, ".") {
				t.Errorf("%s:%d: Has(%s) takes one key; walk a dotted path with testkit.Has", s.rel, s.line(call), lit.Value)
			}
			return true
		})
	}
}
