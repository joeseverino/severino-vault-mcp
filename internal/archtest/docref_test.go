package archtest

import (
	"go/ast"
	"go/token"
	"testing"
)

// docIndexMaps are the index maps a doc reference could be looked up in.
var docIndexMaps = map[string]bool{"ByDocID": true, "DuplicateDocIDs": true, "Aliases": true}

// core.resolveDoc is the one way a doc reference a tool or subcommand is
// given becomes a doc. In the tool surface, reading the index's doc_id or
// alias maps, or comparing a RelativePath to a path, anywhere else is a
// second resolver.
func TestOneDocResolver(t *testing.T) {
	for _, s := range sources(t) {
		if !in(s.rel, "internal/core", "internal/cli", "internal/mcpserver") || s.rel == "internal/core/docref.go" {
			continue
		}
		ast.Inspect(s.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if docIndexMaps[n.Sel.Name] {
					t.Errorf("%s:%d: reads the index's %s; resolve the reference with resolveDoc", s.rel, s.line(n), n.Sel.Name)
				}
			case *ast.BinaryExpr:
				if (n.Op == token.EQL || n.Op == token.NEQ) && (pathCompare(n.X, n.Y) || pathCompare(n.Y, n.X)) {
					t.Errorf("%s:%d: compares a RelativePath to a path; resolve the reference with resolveDoc", s.rel, s.line(n))
				}
			}
			return true
		})
	}
}

// pathCompare reports whether a is a .RelativePath selector and b anything
// but the empty string, which only tests that a path was given.
func pathCompare(a, b ast.Expr) bool {
	sel, ok := a.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "RelativePath" {
		return false
	}
	lit, ok := b.(*ast.BasicLit)
	return !ok || lit.Kind != token.STRING || lit.Value != `""`
}
