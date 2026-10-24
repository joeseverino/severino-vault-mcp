package archtest

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"testing"
)

// pystr.Quoted is the one way a list reaches text: a fmt verb given a slice
// prints Go's own list form ([a b], ["a" "b"]), a second format beside it.
// This type-checks every package and fails on any fmt format call whose
// arguments include a slice other than []byte.
func TestListsReachTextThroughQuoted(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string][]string{}
	for _, s := range sources(t) {
		dirs[path.Dir(s.rel)] = append(dirs[path.Dir(s.rel)], s.rel)
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	formats := map[string]bool{"Sprintf": true, "Errorf": true, "Fprintf": true, "Printf": true, "Appendf": true}
	for dir, rels := range dirs {
		var files []*ast.File
		for _, rel := range rels {
			f, err := parser.ParseFile(fset, filepath.Join(repo, rel), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check(dir, fset, files, info); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !formats[sel.Sel.Name] {
					return true
				}
				if fn, ok := info.Uses[sel.Sel].(*types.Func); !ok || fn.Pkg() == nil || fn.Pkg().Path() != "fmt" {
					return true
				}
				for _, a := range call.Args {
					tv, ok := info.Types[a]
					if !ok || tv.Type == nil {
						continue
					}
					if s, ok := tv.Type.Underlying().(*types.Slice); ok && !isByte(s.Elem()) {
						p := fset.Position(a.Pos())
						rel, _ := filepath.Rel(repo, p.Filename)
						t.Errorf("%s:%d: fmt.%s formats a slice (%s); pass pystr.Quoted(...) under %%s",
							filepath.ToSlash(rel), p.Line, sel.Sel.Name, types.ExprString(a))
					}
				}
				return true
			})
		}
	}
}

func isByte(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Byte
}
