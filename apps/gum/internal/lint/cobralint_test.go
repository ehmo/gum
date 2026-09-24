package lint_test

// Cobra init-hook prohibition (bead gum-ches, docs/test-matrix.md).
//
// Spec §12.2 requires the CLI's initialization order to be constructor-driven.
// cobra.OnInitialize and cobra.OnFinalize register package-global hooks that
// run on every command execution in registration order, so ordering becomes a
// property of import order rather than of the constructor that builds the
// command tree. The matrix row claimed an AST lint enforced this; no lint
// existed, and the row was backed by a shim that grepped for an unrelated
// test name.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cobraScanPaths are the packages that build the cobra command tree. The spec
// row names internal/cli; cmd/gum is where the commands are actually
// constructed, so a lint that skipped it would pass on an empty set.
var cobraScanPaths = []string{
	"internal/cli",
	"cmd/gum",
}

// forbiddenCobraHooks maps a prohibited cobra function to the reason.
var forbiddenCobraHooks = map[string]string{
	"OnInitialize": "spec §12.2: initialization is constructor-driven; a global init hook makes ordering depend on import order",
	"OnFinalize":   "spec §12.2: teardown is constructor-driven; a global finalize hook makes ordering depend on import order",
}

// cobraHookViolation is one flagged call site.
type cobraHookViolation struct {
	Line int
	Name string
}

// findCobraHookCalls reports every call to a forbidden cobra hook in file.
// It resolves the local name of the spf13/cobra import, so an aliased import
// cannot hide the call.
func findCobraHookCalls(fset *token.FileSet, file *ast.File) []cobraHookViolation {
	cobraNames := map[string]bool{}
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != "github.com/spf13/cobra" {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "." {
				// A dot import puts OnInitialize in file scope under its bare
				// name; record the empty selector base to catch that below.
				cobraNames[""] = true
				continue
			}
			cobraNames[imp.Name.Name] = true
			continue
		}
		cobraNames["cobra"] = true
	}
	if len(cobraNames) == 0 {
		return nil
	}

	var found []cobraHookViolation
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			pkg, ok := fun.X.(*ast.Ident)
			if !ok || !cobraNames[pkg.Name] {
				return true
			}
			if _, bad := forbiddenCobraHooks[fun.Sel.Name]; bad {
				found = append(found, cobraHookViolation{Line: fset.Position(call.Pos()).Line, Name: fun.Sel.Name})
			}
		case *ast.Ident:
			if !cobraNames[""] {
				return true
			}
			if _, bad := forbiddenCobraHooks[fun.Name]; bad {
				found = append(found, cobraHookViolation{Line: fset.Position(call.Pos()).Line, Name: fun.Name})
			}
		}
		return true
	})
	return found
}

// TestForbiddenPatterns is the matrix row's proof artifact: no file in the CLI
// layer calls a cobra init hook.
func TestForbiddenPatterns(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	scanned := 0

	for _, rel := range cobraScanPaths {
		abs := filepath.Join(root, rel)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			t.Errorf("scan path %s does not exist; the lint would silently cover nothing", rel)
			continue
		}

		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
			if err != nil {
				t.Errorf("parse %s: %v", relPath(root, path), err)
				return nil
			}
			scanned++

			for _, v := range findCobraHookCalls(fset, file) {
				t.Errorf("%s:%d: cobra.%s is prohibited — %s", relPath(root, path), v.Line, v.Name, forbiddenCobraHooks[v.Name])
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", abs, err)
		}
	}

	// cmd/gum alone is dozens of files. A refactor that moves the command tree
	// and leaves this list stale must fail loudly, not report a clean scan.
	const minScannedFiles = 20
	if scanned < minScannedFiles {
		t.Fatalf("scanned %d CLI files, want at least %d; cobraScanPaths no longer points at the command tree", scanned, minScannedFiles)
	}
}

// TestForbiddenPatternsDetectorCatchesEachHookForm proves the detector before
// the clean-tree assertion above is allowed to mean anything. Without it,
// TestForbiddenPatterns passes for a detector that flags nothing.
func TestForbiddenPatternsDetectorCatchesEachHookForm(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "plain import",
			src: `package cli
import "github.com/spf13/cobra"
func build() { cobra.OnInitialize(func() {}) }`,
			want: []string{"OnInitialize"},
		},
		{
			name: "aliased import",
			src: `package cli
import c "github.com/spf13/cobra"
func build() { c.OnFinalize(func() {}) }`,
			want: []string{"OnFinalize"},
		},
		{
			name: "dot import",
			src: `package cli
import . "github.com/spf13/cobra"
func build() { OnInitialize(func() {}) }`,
			want: []string{"OnInitialize"},
		},
		{
			name: "both hooks in one file",
			src: `package cli
import "github.com/spf13/cobra"
func build() {
	cobra.OnInitialize(func() {})
	cobra.OnFinalize(func() {})
}`,
			want: []string{"OnInitialize", "OnFinalize"},
		},
		{
			name: "unrelated package with the same function name",
			src: `package cli
import "example.com/other"
func build() { other.OnInitialize(func() {}) }`,
			want: nil,
		},
		{
			name: "cobra used without a hook",
			src: `package cli
import "github.com/spf13/cobra"
func build() *cobra.Command { return &cobra.Command{Use: "gum"} }`,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "fixture.go", tc.src, parser.AllErrors)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}

			var got []string
			for _, v := range findCobraHookCalls(fset, file) {
				got = append(got, v.Name)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("findCobraHookCalls = %v, want %v", got, tc.want)
			}
		})
	}
}
