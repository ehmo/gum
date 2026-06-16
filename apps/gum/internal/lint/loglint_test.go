// Package lint owns build-time AST guards that the spec mandates run in CI.
//
// Spec §14.1 rule 1: all log output MUST go through `log/slog`. Direct use of
// the stdlib `log` package, third-party loggers (zerolog, zap, logrus), and
// `fmt.Fprint*` calls that write to `os.Stderr` or `os.Stdout` are PROHIBITED
// in the packages listed in §14's constructor-convention table plus
// `internal/output`. This file implements `TestStdLogProhibition`, the AST
// lint that enforces the rule.
package lint_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// scanPaths enumerates the package prefixes that §14.1 rule 1 forbids
// from importing `log` or writing directly to os.Stderr/os.Stdout. Paths are
// relative to the apps/gum module root and resolved at runtime; missing paths
// are skipped (some packages such as ratelimit/tee/profiles don't exist yet,
// and the spec acknowledges that partial coverage is acceptable until they
// land).
var scanPaths = []string{
	"internal/dispatch",
	"internal/adapters",
	"internal/mcp",
	"internal/cli",
	"internal/cache",
	"internal/auth",
	"internal/profiles",
	"internal/sandbox",
	"internal/ratelimit",
	"internal/tee",
	"internal/output",
}

// prohibitedImports lists Go import paths whose presence in any scanned file
// fails the lint. The stdlib `log` package is forbidden because it routes
// around slog; the third-party loggers are forbidden by §14.1 rule 1.
var prohibitedImports = map[string]string{
	"log":                       "spec §14.1 rule 1: use log/slog, not the stdlib log package",
	"github.com/rs/zerolog":     "spec §14.1 rule 1: zerolog is prohibited; use log/slog",
	"go.uber.org/zap":           "spec §14.1 rule 1: zap is prohibited; use log/slog",
	"github.com/sirupsen/logrus": "spec §14.1 rule 1: logrus is prohibited; use log/slog",
}

// TestStdLogProhibition asserts every Go source file under the scan set
// avoids the prohibited log surfaces. Diagnostic helpers (Printf into a
// strings.Builder, Errorf wrapping) remain allowed because they don't emit
// logs — only os.Stderr/os.Stdout writes do.
func TestStdLogProhibition(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	for _, rel := range scanPaths {
		abs := filepath.Join(root, rel)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatalf("stat %s: %v", abs, err)
		}

		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.ParseComments)
			if err != nil {
				return nil
			}
			for _, imp := range file.Imports {
				p := strings.Trim(imp.Path.Value, "\"")
				if reason, bad := prohibitedImports[p]; bad {
					t.Errorf("%s: prohibited import %q — %s", relPath(root, path), p, reason)
				}
			}

			astFile, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Errorf("parse %s: %v", relPath(root, path), err)
				return nil
			}
			ast.Inspect(astFile, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if !isFmtFprintWriter(call) {
					return true
				}
				if writesToStdStream(call) {
					pos := fset.Position(call.Pos())
					t.Errorf("%s:%d: fmt.Fprint* writes to os.Stderr/Stdout — use log/slog (spec §14.1 rule 1)",
						relPath(root, pos.Filename), pos.Line)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", abs, err)
		}
	}
}

// isFmtFprintWriter reports whether call is fmt.Fprint, fmt.Fprintln, or
// fmt.Fprintf. Other fmt selectors (Errorf, Sprintf, Sscan, …) don't emit
// to a writer and are ignored.
func isFmtFprintWriter(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return false
	}
	switch sel.Sel.Name {
	case "Fprint", "Fprintln", "Fprintf":
		return true
	}
	return false
}

// writesToStdStream returns true when the first argument of an fmt.Fprint*
// call is os.Stderr or os.Stdout.
func writesToStdStream(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "os" {
		return false
	}
	return sel.Sel.Name == "Stderr" || sel.Sel.Name == "Stdout"
}

// moduleRoot walks up from this test file to locate the apps/gum module root
// (the directory containing go.mod). Computed at runtime so the test works
// from any cwd the runner picks.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate go.mod ancestor")
	return ""
}

// relPath returns path relative to root for cleaner error messages; on
// failure it returns the original absolute path.
func relPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
