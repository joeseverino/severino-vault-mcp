package archtest

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"
)

// sections.Slugify (and SlugifyWith) is the server's one slug function. A
// function named for slugs anywhere else, or a [^a-z0-9] class that rebuilds
// its rule, is a second copy.
func TestOneSlugFunction(t *testing.T) {
	for _, s := range sources(t) {
		if in(s.rel, "internal/sections") {
			continue
		}
		ast.Inspect(s.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if strings.Contains(strings.ToLower(n.Name.Name), "slug") {
					t.Errorf("%s:%d: %s builds a slug; call sections.Slugify or SlugifyWith", s.rel, s.line(n), n.Name.Name)
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING && strings.Contains(n.Value, "[^a-z0-9") {
					t.Errorf("%s:%d: a [^a-z0-9] pattern rebuilds the slug rule; call sections.Slugify", s.rel, s.line(n))
				}
			}
			return true
		})
	}
}
