// Package imports_test verifies the import-graph invariants from spec.md §14.
//
// Forbidden edges:
//
//	internal/mcp        → internal/cli
//	internal/cli        → internal/mcp
//	internal/dispatch   → internal/cli
//	internal/dispatch   → internal/mcp
//	cmd/gen-catalog     → internal/pluginenv
package imports_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// edge represents a forbidden import dependency.
type edge struct {
	from string
	to   string
}

// forbidden is the normative list from spec.md §14.
var forbidden = []edge{
	{from: "github.com/ehmo/gum/internal/mcp", to: "github.com/ehmo/gum/internal/cli"},
	{from: "github.com/ehmo/gum/internal/cli", to: "github.com/ehmo/gum/internal/mcp"},
	{from: "github.com/ehmo/gum/internal/dispatch", to: "github.com/ehmo/gum/internal/cli"},
	{from: "github.com/ehmo/gum/internal/dispatch", to: "github.com/ehmo/gum/internal/mcp"},
	{from: "github.com/ehmo/gum/cmd/gen-catalog", to: "github.com/ehmo/gum/internal/pluginenv"},
}

func TestNoCyclicImports(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}

	// Build import closure: pkg path → set of all transitive imports.
	closure := buildTransitiveClosure(pkgs)

	var violations []string
	for _, e := range forbidden {
		if transImports, ok := closure[e.from]; ok {
			if transImports[e.to] {
				violations = append(violations, e.from+" → "+e.to)
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("forbidden import edges detected:\n  %s", strings.Join(violations, "\n  "))
	}
}

// buildTransitiveClosure returns a map from each package path to the set of
// all packages it transitively imports.
func buildTransitiveClosure(pkgs []*packages.Package) map[string]map[string]bool {
	index := map[string]*packages.Package{}
	var index_ func(p *packages.Package)
	index_ = func(p *packages.Package) {
		if _, seen := index[p.PkgPath]; seen {
			return
		}
		index[p.PkgPath] = p
		for _, imp := range p.Imports {
			index_(imp)
		}
	}
	for _, p := range pkgs {
		index_(p)
	}

	result := map[string]map[string]bool{}
	var transitive func(path string) map[string]bool
	transitive = func(path string) map[string]bool {
		if s, ok := result[path]; ok {
			return s
		}
		s := map[string]bool{}
		result[path] = s // break cycles
		p, ok := index[path]
		if !ok {
			return s
		}
		for impPath, imp := range p.Imports {
			s[impPath] = true
			for dep := range transitive(imp.PkgPath) {
				s[dep] = true
			}
		}
		return s
	}
	for path := range index {
		transitive(path)
	}
	return result
}
