package archtest

import (
	"go/ast"
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
// field, jsonx.Strings for a JSON string list, fsx.IsDir for a folder check,
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

// fieldGetter matches func(x *jsonx.Obj, key string, ...) string: reading
// one field as text.
func fieldGetter(fn *ast.FuncDecl, jsonxName string) bool {
	if fn.Recv != nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "string" {
		return false
	}
	var types []ast.Expr
	for _, f := range fn.Type.Params.List {
		n := max(len(f.Names), 1)
		for range n {
			types = append(types, f.Type)
		}
	}
	if len(types) < 2 {
		return false
	}
	star, ok := types[0].(*ast.StarExpr)
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
	key, ok := types[1].(*ast.Ident)
	return ok && key.Name == "string"
}
