// Spec §14.1 rule 2: every package listed in §14's constructor-convention
// table that emits log output MUST route every emission through one
// injectable *slog.Logger, MUST fall back to slog.Default() when the caller
// injects none, and MUST fall silent under slog.New(slog.DiscardHandler).
// This file implements TestLoggerInjectionContract, the proof
// docs/test-matrix.md names.
package lint_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auditlog"
	"github.com/ehmo/gum/internal/output/gain"
)

// rule2Packages is the set of packages that emit log output and therefore
// owe the §14.1 rule 2 contract. It is not the whole constructor-convention
// table: a package that emits nothing needs no logger, and rule 1's lint
// keeps it that way. cmd/gum is excluded because rule 3 makes it the owner
// of the process-wide default handler, so it logs through slog.Default() by
// design.
var rule2Packages = []string{
	"internal/dispatch",
	"internal/mcp",
	"internal/auditlog",
	"internal/output/gain",
	"internal/plugins/registry",
}

// emissionMethods are the slog call names that write a record. Constructors
// and lookups (slog.New, slog.Default, slog.String) are not emissions and
// stay legal everywhere.
var emissionMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
	"Log": true, "LogAttrs": true,
}

// TestLoggerInjectionContract proves §14.1 rule 2 in three parts: no package
// in the rule-2 set still emits through the package-level slog functions,
// each one declares the injection field and accessor plus an exported way to
// set them, and the contract holds at runtime for the two packages an
// external test can drive end to end.
func TestLoggerInjectionContract(t *testing.T) {
	root := moduleRoot(t)

	scanned := 0
	for _, rel := range rule2Packages {
		abs := filepath.Join(root, rel)
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("rule-2 package %s does not exist; the scan set is stale", rel)
			continue
		}
		scanned++
		checkPackageRoutesEveryEmission(t, root, rel, abs)
	}
	// A refactor that renames or empties the set must fail here rather than
	// turn the gate into a no-op, the same guard TestNoCyclicImports puts on
	// its dispatcher-call scan.
	if scanned != len(rule2Packages) {
		t.Fatalf("scanned %d of %d rule-2 packages; the gate must cover all of them", scanned, len(rule2Packages))
	}

	t.Run("gain ledger", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can chmod /dev/null, which would change a system device node")
		}
		// /dev/null is openable and appendable by any account but owned by
		// root, so the 0o600 tightening in NewLedgerWithLogger fails with
		// EPERM and warns. It is the one gain emission an external test can
		// trigger without a 100 MB ledger.
		assertLoggerContract(t, func(l *slog.Logger) {
			led, err := gain.NewLedgerWithLogger(os.DevNull, l)
			if err != nil {
				t.Fatalf("NewLedgerWithLogger(%s): %v", os.DevNull, err)
			}
			_ = led.Close()
		})
	})

	t.Run("auditlog writer", func(t *testing.T) {
		assertLoggerContract(t, func(l *slog.Logger) {
			w, err := auditlog.New(t.TempDir(), auditlog.WithBufferedChannel(4), auditlog.WithLogger(l))
			if err != nil {
				t.Fatalf("auditlog.New: %v", err)
			}
			// Close drains the buffered channel and emits exactly one
			// audit_drain_complete record.
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	})
}

// assertLoggerContract runs emit three times against the same package: with
// a capturing logger injected, with a discard logger injected, and with no
// logger at all while slog.Default() captures. It proves the three
// behaviours rule 2 names — injection reaches the package, a DiscardHandler
// silences it without panicking, and the zero value falls back to
// slog.Default().
func assertLoggerContract(t *testing.T, emit func(*slog.Logger)) {
	t.Helper()

	var injected bytes.Buffer
	emit(slog.New(slog.NewTextHandler(&injected, nil)))
	if injected.Len() == 0 {
		t.Fatal("injected logger captured nothing; the package does not route its emission through the injected logger")
	}

	// A DiscardHandler must swallow the record without reaching the process
	// default and without panicking on the nil-handler edges inside slog.
	var leaked bytes.Buffer
	restore := swapDefault(t, &leaked)
	emit(slog.New(slog.DiscardHandler))
	if leaked.Len() != 0 {
		t.Errorf("slog.New(slog.DiscardHandler) still emitted %q", leaked.String())
	}

	// No injection at all falls back to slog.Default(), which is still the
	// capturing handler installed above.
	leaked.Reset()
	emit(nil)
	if leaked.Len() == 0 {
		t.Error("with no logger injected the package emitted nothing; the default must be slog.Default()")
	}
	restore()
}

// swapDefault installs a capturing handler as the process default and
// returns the restore func. slog.SetDefault is process-wide, so callers must
// not run in parallel.
func swapDefault(t *testing.T, buf *bytes.Buffer) func() {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	restore := func() { slog.SetDefault(prev) }
	t.Cleanup(restore)
	return restore
}

// checkPackageRoutesEveryEmission asserts one rule-2 package emits nothing
// through the package-level slog functions, declares the `logger
// *slog.Logger` field and the `log()` accessor that every routed call goes
// through, and exports at least one identifier a caller can inject with.
func checkPackageRoutesEveryEmission(t *testing.T, root, rel, abs string) {
	t.Helper()
	fset := token.NewFileSet()

	var hasField, hasAccessor, hasInjectionPoint, hasRoutedCall bool

	err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Errorf("parse %s: %v", relPath(root, path), err)
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				sel, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || !emissionMethods[sel.Sel.Name] {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "slog" {
					pos := fset.Position(node.Pos())
					t.Errorf("%s:%d: slog.%s emits through the package-level logger — route it through the injected one (spec §14.1 rule 2)",
						relPath(root, pos.Filename), pos.Line, sel.Sel.Name)
					return true
				}
				hasRoutedCall = true
			case *ast.Field:
				if isLoggerField(node) {
					hasField = true
				}
				for _, name := range node.Names {
					if name.IsExported() && strings.Contains(name.Name, "Logger") {
						hasInjectionPoint = true
					}
				}
			case *ast.FuncDecl:
				if node.Name.Name == "log" && node.Recv != nil {
					hasAccessor = true
				}
				if node.Name.IsExported() && strings.Contains(node.Name.Name, "Logger") {
					hasInjectionPoint = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", abs, err)
	}

	if !hasRoutedCall {
		t.Errorf("%s: no log emission found; drop it from rule2Packages or restore the call site", rel)
	}
	if !hasField {
		t.Errorf("%s: no `logger *slog.Logger` field; rule 2 requires one injectable logger per package", rel)
	}
	if !hasAccessor {
		t.Errorf("%s: no `log()` accessor; the slog.Default() fallback has nowhere to live", rel)
	}
	if !hasInjectionPoint {
		t.Errorf("%s: no exported identifier naming a Logger; a caller has no way to inject one", rel)
	}
}

// isLoggerField reports whether f declares `logger *slog.Logger`.
func isLoggerField(f *ast.Field) bool {
	star, ok := f.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Logger" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "slog" {
		return false
	}
	for _, name := range f.Names {
		if name.Name == "logger" {
			return true
		}
	}
	return false
}
