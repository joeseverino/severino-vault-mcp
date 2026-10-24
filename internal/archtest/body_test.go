package archtest

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"
)

// sections is the one home of a body's lines and headings: Lines/NewBody
// split it, its ATX pattern finds headings outside fenced code, and Body's
// Find, NextHeading, SectionEnd, Under, AppendTo and Remove edit it. A
// newline split or a heading pattern anywhere else is a second copy.
func TestOneBodySplitter(t *testing.T) {
	for _, s := range sources(t) {
		if in(s.rel, "internal/sections") {
			continue
		}
		stringsName := s.importName("strings")
		ast.Inspect(s.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || len(n.Args) != 2 || stringsName == "" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == stringsName && sel.Sel.Name == "Split" && isNewline(n.Args[1]) {
					t.Errorf("%s:%d: splits a body on newlines; use sections.Lines or sections.NewBody", s.rel, s.line(n))
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING && (strings.Contains(n.Value, "#{1,6}") || strings.HasPrefix(n.Value, "`^#") || strings.HasPrefix(n.Value, `"^#`)) {
					t.Errorf("%s:%d: a heading pattern; find headings with sections.Body", s.rel, s.line(n))
				}
			}
			return true
		})
	}
}

func isNewline(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `"\n"`
}
