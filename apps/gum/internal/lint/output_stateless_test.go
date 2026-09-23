package lint_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// statelessOutputPaths are the internal/output encoder packages spec §14
// line 3506 requires to be stateless: "encoders take a value and a profile,
// return bytes; they hold no mutable state and have no exported
// constructors."
//
// internal/output/tee and internal/output/gain are out of scope by the same
// line, which splits artifact and ledger state off from the encoders: tee
// "owns artifact filesystem state, retention, and the gum://results/{hash}
// resolution", and the gain ledger is an append-only file with rotation.
// Both therefore have exported constructors and file handles by design.
var statelessOutputPaths = []string{
	"internal/output",
	"internal/output/fieldmask",
	"internal/output/jcs",
	"internal/output/profile",
	"internal/output/render",
	"internal/output/toon",
}

// TestOutputStatelessness is the docs/test-matrix.md row 172 proof. It scans
// the encoder packages for the three shapes mutable state takes in Go: an
// exported constructor handing out a stateful receiver, a package-level var
// something writes to, and an exported struct with unexported fields.
//
// The second check is the one that stops the failure the row names, a
// stateful encoder cache: a cache must be written to, and a write to a
// package-level var is what this rejects.
func TestOutputStatelessness(t *testing.T) {
	root := moduleRoot(t)

	for _, rel := range statelessOutputPaths {
		dir := filepath.Join(root, rel)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("scan path %s is missing: %v", rel, err)
		}
		files := packageFiles(t, dir)
		if len(files) == 0 {
			t.Fatalf("scan path %s has no non-test Go files", rel)
		}
		fset := token.NewFileSet()
		parsed := make(map[string]*ast.File, len(files))
		for _, path := range files {
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			parsed[path] = f
		}

		checkNoExportedConstructors(t, root, fset, parsed)
		checkNoWrittenPackageVars(t, root, fset, parsed)
		checkNoExportedStructWithHiddenFields(t, root, fset, parsed)
	}
}

// packageFiles lists the non-test Go files directly inside dir. It does not
// recurse: each scan path is one package.
func packageFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func checkNoExportedConstructors(t *testing.T, root string, fset *token.FileSet, files map[string]*ast.File) {
	t.Helper()
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "New") || !fn.Name.IsExported() {
				continue
			}
			pos := fset.Position(fn.Pos())
			kind := "function"
			if fn.Recv != nil {
				kind = "method"
			}
			t.Errorf("%s:%d: exported constructor %s %s — internal/output encoders are stateless (spec §14)",
				relPath(root, pos.Filename), pos.Line, kind, fn.Name.Name)
		}
	}
}

// checkNoWrittenPackageVars fails on any package-level var that the package
// itself writes to. Declaring a var is allowed (sentinel errors, compiled
// regexps, lookup tables); writing one is what turns it into state.
//
// One-time lazy initialization under sync.Once is exempt. The value it
// produces is written once and read forever after, which is what a const or
// an init() would give; the Once only defers the cost. The exemption is
// narrow: the write must sit inside the closure passed to Once.Do, or inside
// a named function that nothing calls except a Once.Do argument list.
func checkNoWrittenPackageVars(t *testing.T, root string, fset *token.FileSet, files map[string]*ast.File) {
	t.Helper()
	onceInit := onceInitFuncs(files)
	pkgVars := map[string]token.Position{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					pkgVars[name.Name] = fset.Position(name.Pos())
				}
			}
		}
	}

	report := func(name string, pos token.Position, how string) {
		decl := pkgVars[name]
		t.Errorf("%s:%d: %s package-level var %s (declared at %s:%d) — internal/output holds no mutable state (spec §14)",
			relPath(root, pos.Filename), pos.Line, how, name, relPath(root, decl.Filename), decl.Line)
	}

	// rootIdent unwraps x, x[k], x.f, *x and x[i:j] down to the base
	// identifier, so an indexed or field write counts as a write to the var.
	var rootIdent func(ast.Expr) *ast.Ident
	rootIdent = func(e ast.Expr) *ast.Ident {
		switch v := e.(type) {
		case *ast.Ident:
			return v
		case *ast.IndexExpr:
			return rootIdent(v.X)
		case *ast.SelectorExpr:
			return rootIdent(v.X)
		case *ast.StarExpr:
			return rootIdent(v.X)
		case *ast.SliceExpr:
			return rootIdent(v.X)
		}
		return nil
	}

	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Recv == nil && onceInit[fn.Name.Name] {
				continue
			}
			// Locals shadow package vars. Collect every name bound inside
			// the function so a local reuse is not reported as a write.
			shadowed := map[string]bool{}
			for _, field := range paramNames(fn) {
				shadowed[field] = true
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && isOnceDo(call) {
					return false
				}
				switch v := n.(type) {
				case *ast.AssignStmt:
					if v.Tok == token.DEFINE {
						for _, lhs := range v.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								shadowed[id.Name] = true
							}
						}
						return true
					}
					for _, lhs := range v.Lhs {
						id := rootIdent(lhs)
						if id == nil || shadowed[id.Name] {
							continue
						}
						if _, isPkg := pkgVars[id.Name]; isPkg {
							report(id.Name, fset.Position(lhs.Pos()), "assignment to")
						}
					}
				case *ast.IncDecStmt:
					id := rootIdent(v.X)
					if id == nil || shadowed[id.Name] {
						return true
					}
					if _, isPkg := pkgVars[id.Name]; isPkg {
						report(id.Name, fset.Position(v.Pos()), "increment/decrement of")
					}
				case *ast.RangeStmt:
					if v.Tok == token.DEFINE {
						for _, e := range []ast.Expr{v.Key, v.Value} {
							if id, ok := e.(*ast.Ident); ok {
								shadowed[id.Name] = true
							}
						}
					}
				case *ast.CallExpr:
					fnIdent, ok := v.Fun.(*ast.Ident)
					if !ok || fnIdent.Name != "delete" || len(v.Args) == 0 {
						return true
					}
					id := rootIdent(v.Args[0])
					if id == nil || shadowed[id.Name] {
						return true
					}
					if _, isPkg := pkgVars[id.Name]; isPkg {
						report(id.Name, fset.Position(v.Pos()), "delete from")
					}
				}
				return true
			})
		}
	}
}

// rootSelector unwraps x[k], *x and x[i:j] down to the selector they index,
// so `m.cache[k] = v` counts as a write to the cache field.
func rootSelector(e ast.Expr) *ast.SelectorExpr {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v
	case *ast.IndexExpr:
		return rootSelector(v.X)
	case *ast.StarExpr:
		return rootSelector(v.X)
	case *ast.SliceExpr:
		return rootSelector(v.X)
	}
	return nil
}

// isOnceDo reports whether call is `<something>.Do(...)`, the sync.Once
// entry point. Name-based: the lint parses without type information, and no
// other Do method in these packages writes package state.
func isOnceDo(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "Do"
}

// onceInitFuncs returns the names of functions passed by reference to a
// Once.Do call and never called directly. Those run at most once, so what
// they write is initialization, not mutation.
func onceInitFuncs(files map[string]*ast.File) map[string]bool {
	passedToDo := map[string]bool{}
	calledDirectly := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isOnceDo(call) {
				for _, arg := range call.Args {
					if id, ok := arg.(*ast.Ident); ok {
						passedToDo[id.Name] = true
					}
				}
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				calledDirectly[id.Name] = true
			}
			return true
		})
	}
	out := map[string]bool{}
	for name := range passedToDo {
		if !calledDirectly[name] {
			out[name] = true
		}
	}
	return out
}

// paramNames lists every identifier a function declaration binds in its
// receiver, parameters and named results.
func paramNames(fn *ast.FuncDecl) []string {
	var out []string
	collect := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				out = append(out, n.Name)
			}
		}
	}
	collect(fn.Recv)
	if fn.Type != nil {
		collect(fn.Type.Params)
		collect(fn.Type.Results)
	}
	return out
}

// checkNoExportedStructWithHiddenFields fails when the package writes to an
// unexported field of an exported type outside a composite literal. A value
// built once from a literal and only read afterwards (fieldmask.Mask is the
// live example) hides its representation without holding mutable state; a
// field the package assigns to after construction is state.
//
// The match is by field name, so a same-named field on an unexported type
// also trips it. That direction is safe: the lint over-reports rather than
// letting a mutable field through.
func checkNoExportedStructWithHiddenFields(t *testing.T, root string, fset *token.FileSet, files map[string]*ast.File) {
	t.Helper()
	owner := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if !name.IsExported() {
							owner[name.Name] = ts.Name.Name
						}
					}
				}
			}
		}
	}
	if len(owner) == 0 {
		return
	}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel := rootSelector(lhs)
				if sel == nil || sel.Sel == nil {
					continue
				}
				typeName, hidden := owner[sel.Sel.Name]
				if !hidden {
					continue
				}
				pos := fset.Position(sel.Pos())
				t.Errorf("%s:%d: assignment to unexported field %s of exported type %s — internal/output holds no mutable state (spec §14)",
					relPath(root, pos.Filename), pos.Line, sel.Sel.Name, typeName)
			}
			return true
		})
	}
}
