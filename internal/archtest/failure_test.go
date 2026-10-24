package archtest

import (
	"go/ast"
	"go/token"
	"testing"
)

// jsonx.Failure (and Fail, its one-message form) is the only place an
// {"ok": false} object is built. A literal "ok" key paired with false anywhere
// else, through New, Set or a map literal, is a second failure helper.
func TestOneFailureEnvelope(t *testing.T) {
	for _, s := range sources(t) {
		if in(s.rel, "internal/jsonx") {
			continue
		}
		ast.Inspect(s.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				for i := 0; i+1 < len(n.Args); i++ {
					if isOK(n.Args[i]) && isFalse(n.Args[i+1]) {
						t.Errorf("%s:%d: builds an ok:false object; return jsonx.Fail or jsonx.Failure", s.rel, s.line(n))
					}
				}
			case *ast.KeyValueExpr:
				if isOK(n.Key) && isFalse(n.Value) {
					t.Errorf("%s:%d: builds an ok:false object; return jsonx.Fail or jsonx.Failure", s.rel, s.line(n))
				}
			}
			return true
		})
	}
}

func isOK(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && (lit.Value == `"ok"` || lit.Value == "`ok`")
}

func isFalse(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "false"
}
