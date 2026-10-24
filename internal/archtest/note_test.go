package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// frontmatter.NewNote is the one read-then-split: fsx.Vault.ReadNote inside a
// vault root, frontmatter.ReadFile outside one. A function that reads text
// and splits it at the frontmatter itself is a second copy. Splitting text a
// caller already holds (an Update edit's input) is fine.
func TestOneNoteReader(t *testing.T) {
	for _, s := range sources(t) {
		if in(s.rel, "internal/frontmatter", "internal/fsx") {
			continue
		}
		fm := s.importName("github.com/joeseverino/severino-vault-mcp/internal/frontmatter")
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			reads, splits := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "ReadText" {
					reads = true
				}
				if id, ok := sel.X.(*ast.Ident); ok && fm != "" && id.Name == fm {
					switch sel.Sel.Name {
					case "Split", "BodyOffset", "NewNote":
						splits = true
					}
				}
				return true
			})
			if reads && splits {
				t.Errorf("%s:%d: %s reads a note and splits its frontmatter; use fsx.Vault.ReadNote or frontmatter.ReadFile", s.rel, s.line(fn), fn.Name.Name)
			}
		}
	}
}

// The value coercions have one home each: jsonx.Obj's Text, Int and Str for a
// field, jsonx.Strings for a JSON string list, vault.CoerceList for a value read as a list, fsx.IsDir for a folder check,
// core's containsFold for a case-blind match.
func TestOneCoercion(t *testing.T) {
	for _, s := range sources(t) {
		osName := s.importName("os")
		stringsName := s.importName("strings")
		jsonxName := s.importName("github.com/joeseverino/severino-vault-mcp/internal/jsonx")
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if !in(s.rel, "internal/jsonx") && jsonxName != "" && fieldGetter(fn, jsonxName) {
				t.Errorf("%s:%d: %s reads a field as a string; use (*jsonx.Obj).Text or Str", s.rel, s.line(fn), fn.Name.Name)
			}
			if fn.Name.Name != "CoerceList" && listCoercion(fn) {
				t.Errorf("%s:%d: %s turns a value into a []string by hand; use vault.CoerceList", s.rel, s.line(fn), fn.Name.Name)
			}
			stats, isDir := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch {
				case isPkgCall(call, osName, "Stat"):
					stats = true
				case isMethodCall(call, "IsDir"):
					isDir = true
				case isBuiltinMake(call, "any") && !in(s.rel, "internal/jsonx"):
					t.Errorf("%s:%d: builds a []any by hand; use jsonx.Strings", s.rel, s.line(call))
				case isPkgCall(call, stringsName, "Contains") && fn.Name.Name != "containsFold":
					if inner, ok := call.Args[0].(*ast.CallExpr); ok && isPkgCall(inner, stringsName, "ToLower") {
						t.Errorf("%s:%d: a case-blind match by hand; use containsFold", s.rel, s.line(call))
					}
				}
				return true
			})
			if stats && isDir && !in(s.rel, "internal/fsx") {
				t.Errorf("%s:%d: %s checks for a folder with os.Stat; use fsx.IsDir", s.rel, s.line(fn), fn.Name.Name)
			}
		}
	}
}

// listCoercion matches a function returning []string whose body type-switches
// on []any: a value-to-list coercion.
func listCoercion(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	arr, ok := fn.Type.Results.List[0].Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	if id, ok := arr.Elt.(*ast.Ident); !ok || id.Name != "string" {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, e := range cc.List {
			if a, ok := e.(*ast.ArrayType); ok && a.Len == nil {
				if id, ok := a.Elt.(*ast.Ident); ok && id.Name == "any" {
					found = true
				}
			}
		}
		return true
	})
	return found
}

func isPkgCall(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || pkg == "" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == name
}

func isMethodCall(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}

// isBuiltinMake matches make([]elem, len(...)).
func isBuiltinMake(call *ast.CallExpr, elem string) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "make" || len(call.Args) < 2 {
		return false
	}
	arr, ok := call.Args[0].(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	el, ok := arr.Elt.(*ast.Ident)
	if !ok || el.Name != elem {
		return false
	}
	ln, ok := call.Args[1].(*ast.CallExpr)
	if !ok {
		return false
	}
	lid, ok := ln.Fun.(*ast.Ident)
	return ok && lid.Name == "len"
}

// fieldGetter matches func(x *jsonx.Obj, key string, ...) string whose body
// reads x with key (x[key], x.Get(key), x.Text(key)) and returns something
// other than a literal, a concatenation or a formatted message: reading one
// field as text. A check that takes the same parameters and returns a problem
// message returns only literals and formatted text, so it does not match.
func fieldGetter(fn *ast.FuncDecl, jsonxName string) bool {
	if fn.Recv != nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "string" {
		return false
	}
	type param struct {
		name string
		typ  ast.Expr
	}
	var params []param
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			params = append(params, param{"", f.Type})
		}
		for _, n := range f.Names {
			params = append(params, param{n.Name, f.Type})
		}
	}
	if len(params) < 2 {
		return false
	}
	star, ok := params[0].typ.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != jsonxName || sel.Sel.Name != "Obj" {
		return false
	}
	key, ok := params[1].typ.(*ast.Ident)
	if !ok || key.Name != "string" {
		return false
	}
	obj, keyName := params[0].name, params[1].name
	if obj == "" || keyName == "" || obj == "_" || keyName == "_" {
		return false
	}
	isObj := func(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == obj }
	isKey := func(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == keyName }
	reads, returnsValue := false, false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && isObj(sel.X) {
				for _, a := range n.Args {
					if isKey(a) {
						reads = true
					}
				}
			}
		case *ast.IndexExpr:
			x := n.X
			if sel, ok := x.(*ast.SelectorExpr); ok {
				x = sel.X
			}
			if isObj(x) && isKey(n.Index) {
				reads = true
			}
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				if !messageExpr(r) {
					returnsValue = true
				}
			}
		}
		return true
	})
	return reads && returnsValue
}

// messageExpr is a return expression that builds text rather than passing a
// read value through: a string literal, a concatenation, or a Sprintf.
func messageExpr(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.BinaryExpr:
		return true
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "Sprintf"
	}
	return false
}

func TestFieldGetterMatchesReadsOnly(t *testing.T) {
	const src = `package p
import "x/jsonx"
func getter(o *jsonx.Obj, k string) string { v, _ := o.Get(k).(string); return v }
func indexed(o *jsonx.Obj, k string) string { return o.M[k] }
func check(o *jsonx.Obj, k string) string {
	if o.Has(k) { return "" }
	return fmt.Sprintf("%s missing", k)
}
func literal(o *jsonx.Obj, k string) string {
	if o.Text(k) == "" { return "empty" }
	return ""
}
func other(o *jsonx.Obj, k string) string { return "x" }
`
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"getter": true, "indexed": true, "check": false, "literal": false, "other": false}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if got := fieldGetter(fn, "jsonx"); got != want[fn.Name.Name] {
			t.Errorf("fieldGetter(%s) = %v, want %v", fn.Name.Name, got, want[fn.Name.Name])
		}
	}
}
