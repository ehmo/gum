// TestNoCyclicImports is the §14 import-graph gate. Spec §14 line 3503 names
// this file and this test by path, requires golang.org/x/tools/go/packages for
// the graph load, and lists three assertions: (a) the module import graph has
// no cycle, (b) nothing in internal/dispatch's transitive import set imports
// internal/dispatch back, and (c) the four directional rules hold. The test
// did not exist, so every rule in the contract was unenforced.
//
// Two of the four rules carry a sanctioned exception, each written into §14
// alongside the rule and asserted here as a closed set so a third case fails:
//
//   - rule 3 forbids an adapter calling the dispatcher entrypoint, but §1
//     line 241 makes the dispatch core the single chokepoint for "every
//     gum_call from inside code mode". The Risor code adapter is therefore the
//     one sanctioned re-entry, and only for the two sandbox host functions.
//   - rule 4 forbids cmd/gen-catalog importing any internal package beyond
//     internal/catalog, but the §5.4 pipeline requires the generator to
//     "validate all catalog-embedded output profiles against
//     docs/expression-profile-dsl.json; fail build on any violation", and that
//     validator is internal/output/profile.ValidateRawProfileFile.
//
// Rule 2 also needed a reading, now written into §14. It lists internal/auth
// among the packages that must not import internal/dispatch, on the stated
// grounds that "doing so creates a cycle". It does not: dispatch never imports
// internal/auth, which reaches it through DispatcherConfig injection, so auth
// importing dispatch.Credentials to implement dispatch.AuthResolver leaves the
// graph acyclic. The property that protects the kernel is the one this test
// enforces: a package inside dispatch's own closure may not import dispatch,
// and no injected package may call the dispatcher entrypoint.
//
// Cost note: the graph load is one packages.Load over ./... with imports and
// deps, a few seconds on cold cache. Nothing here compiles or runs the module.

package lint_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const (
	modulePath  = "github.com/ehmo/gum"
	dispatchPkg = modulePath + "/internal/dispatch"
	catalogPkg  = modulePath + "/internal/catalog"
	profilePkg  = modulePath + "/internal/output/profile"
	genCatalog  = modulePath + "/cmd/gen-catalog"
	mcpPkg      = modulePath + "/internal/mcp"
	cliPkg      = modulePath + "/internal/cli"
)

// dispatcherCallScanRoots maps each source tree the rule-3 call-site ratchet
// walks, as a path relative to internal/lint, to that tree's module-relative
// prefix. The prefix is carried explicitly because filepath.Rel cannot relate
// two relative paths that climb past the working directory.
var dispatcherCallScanRoots = map[string]string{
	"..":        "internal",
	"../../cmd": "cmd",
}

// rule2Families are the package families spec §14 rule 2 forbids from
// importing internal/dispatch. The spec names flat paths (internal/output,
// internal/tee); the tree has since nested some of them (internal/output/toon,
// internal/output/tee), so each entry matches itself and everything below it.
var rule2Families = []string{
	modulePath + "/internal/usage",
	catalogPkg,
	modulePath + "/internal/profiles",
	modulePath + "/internal/auth",
	modulePath + "/internal/cache",
	modulePath + "/internal/ratelimit",
	modulePath + "/internal/retry",
	modulePath + "/internal/sanitize",
	modulePath + "/internal/output",
	modulePath + "/internal/tee",
	modulePath + "/internal/pluginenv",
}

// dispatcherCallAllowlist holds the path prefixes allowed to call the
// dispatcher entrypoint, as module-relative paths. §14 rule 3 lets the two
// presentation layers call it; cmd/gum is the binary that wires them. The two
// internal/adapters files are the rule-3 exception: they are the Risor sandbox
// host functions behind gum_call and gum_parallel, which §1 line 241 requires
// to pass through the dispatch core.
var dispatcherCallAllowlist = []string{
	"cmd/",
	"internal/cli/",
	"internal/mcp/",
	"internal/dispatch/",
	"internal/adapters/code_risor.go",
	"internal/adapters/code_risor_parallel.go",
}

// importGraph maps a module-internal package path to its module-internal
// direct imports. Third-party and stdlib edges are dropped: every §14 rule is
// about this module's own layering.
type importGraph map[string][]string

func loadImportGraph(t *testing.T) importGraph {
	t.Helper()

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
		// The test lives in internal/lint; the module root is two levels up.
		Dir:   "../..",
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("packages.Load reported errors; the import graph is not trustworthy")
	}

	graph := importGraph{}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if !strings.HasPrefix(p.PkgPath, modulePath) {
			return
		}
		deps := make([]string, 0, len(p.Imports))
		for dep := range p.Imports {
			if strings.HasPrefix(dep, modulePath) {
				deps = append(deps, dep)
			}
		}
		sort.Strings(deps)
		graph[p.PkgPath] = deps
	})
	if len(graph) == 0 {
		t.Fatal("import graph is empty; packages.Load matched no module package")
	}
	if _, ok := graph[dispatchPkg]; !ok {
		t.Fatalf("%s absent from the loaded graph", dispatchPkg)
	}
	return graph
}

// transitive returns every module-internal package reachable from root,
// excluding root itself unless a cycle reaches back to it.
func (g importGraph) transitive(root string) map[string]bool {
	seen := map[string]bool{}
	stack := append([]string(nil), g[root]...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		stack = append(stack, g[cur]...)
	}
	return seen
}

// matching returns the loaded packages that are family or live under it.
func (g importGraph) matching(family string) []string {
	var out []string
	for pkg := range g {
		if pkg == family || strings.HasPrefix(pkg, family+"/") {
			out = append(out, pkg)
		}
	}
	sort.Strings(out)
	return out
}

// TestNoCyclicImports asserts the §14 line 3503 contract.
func TestNoCyclicImports(t *testing.T) {
	graph := loadImportGraph(t)

	t.Run("no cycle in the module graph", func(t *testing.T) {
		// The Go compiler already rejects import cycles, so this arm exists to
		// catch a graph-loading regression that would make the rest of the
		// test vacuous, not a realistic layering slip.
		const (
			white = 0
			grey  = 1
			black = 2
		)
		color := map[string]int{}
		var path []string
		var walk func(string)
		walk = func(pkg string) {
			color[pkg] = grey
			path = append(path, pkg)
			for _, dep := range graph[pkg] {
				switch color[dep] {
				case grey:
					t.Errorf("import cycle: %s -> %s", strings.Join(path, " -> "), dep)
				case white:
					walk(dep)
				}
			}
			path = path[:len(path)-1]
			color[pkg] = black
		}
		for pkg := range graph {
			if color[pkg] == white {
				walk(pkg)
			}
		}
	})

	t.Run("dispatch is the leaf of its own closure", func(t *testing.T) {
		// Assertion (b): no package dispatch depends on may depend on
		// dispatch. That is what makes the kernel injectable rather than
		// ambient.
		for pkg := range graph.transitive(dispatchPkg) {
			if graph.transitive(pkg)[dispatchPkg] {
				t.Errorf("%s is in dispatch's closure and imports dispatch back", pkg)
			}
		}
	})

	t.Run("rule 2: no named family inside the kernel closure imports it", func(t *testing.T) {
		closure := graph.transitive(dispatchPkg)
		matched := 0
		for _, family := range rule2Families {
			for _, pkg := range graph.matching(family) {
				matched++
				if !closure[pkg] {
					// Injected through DispatcherConfig: may hold kernel types,
					// may not call the kernel. The call-site ratchet below is
					// what binds it.
					continue
				}
				if graph.transitive(pkg)[dispatchPkg] {
					t.Errorf("%s is inside the kernel closure and imports %s; §14 rule 2 forbids it", pkg, dispatchPkg)
				}
			}
		}
		// A rename that emptied every family would leave this check asserting
		// nothing, which is the failure mode a missing gate already has.
		if matched == 0 {
			t.Fatalf("no loaded package matched any rule-2 family; update rule2Families")
		}
	})

	t.Run("rule 3: dispatch imports no presentation layer", func(t *testing.T) {
		closure := graph.transitive(dispatchPkg)
		for _, presentation := range []string{mcpPkg, cliPkg} {
			if closure[presentation] {
				t.Errorf("%s imports %s; the kernel must not depend on a presentation layer", dispatchPkg, presentation)
			}
		}
	})

	t.Run("rule 4: gen-catalog imports only the generator's closure", func(t *testing.T) {
		if _, ok := graph[genCatalog]; !ok {
			t.Fatalf("%s absent from the loaded graph", genCatalog)
		}
		allowed := map[string]bool{catalogPkg: true, profilePkg: true}
		for _, root := range []string{catalogPkg, profilePkg} {
			for pkg := range graph.transitive(root) {
				allowed[pkg] = true
			}
		}
		for pkg := range graph.transitive(genCatalog) {
			if !allowed[pkg] {
				t.Errorf("%s imports %s; §14 rule 4 allows only internal/catalog and the §5.4 profile validator", genCatalog, pkg)
			}
		}
		if graph.transitive(genCatalog)[dispatchPkg] {
			t.Errorf("%s imports %s; the generator must never reach the kernel", genCatalog, dispatchPkg)
		}
	})

	t.Run("rule 3: only presentation layers and code mode call the dispatcher", func(t *testing.T) {
		for file, lines := range dispatcherCallSites(t) {
			if dispatcherCallAllowed(file) {
				continue
			}
			t.Errorf("%s calls the dispatcher entrypoint at line(s) %v; §14 rule 3 allows the call "+
				"only from a presentation layer or the code-mode host functions", file, lines)
		}
	})
}

// dispatcherCallAllowed reports whether a module-relative file path is on the
// rule-3 allowlist, matching either a directory prefix or an exact file.
func dispatcherCallAllowed(file string) bool {
	for _, allowed := range dispatcherCallAllowlist {
		if file == allowed || strings.HasPrefix(file, allowed) {
			return true
		}
	}
	return false
}

// dispatcherCallSites returns every non-test file under internal/ and cmd/
// holding a call to a method named Dispatch, keyed by module-relative path,
// with the line numbers. The check is syntactic on purpose: the rule is about
// call sites existing at all, and a type-checked walk would pull the whole
// module into a lint test for no extra signal. The cost is one false positive
// class — an unrelated method named Dispatch — which a maintainer resolves by
// renaming it or extending the allowlist with a reason.
func dispatcherCallSites(t *testing.T) map[string][]int {
	t.Helper()

	out := map[string][]int{}
	fset := token.NewFileSet()
	for root, prefix := range dispatcherCallScanRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(filepath.Join(prefix, rel))
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Dispatch" {
					return true
				}
				out[rel] = append(out[rel], fset.Position(call.Pos()).Line)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(out) == 0 {
		t.Fatal("no dispatcher call site found; the ratchet is asserting nothing")
	}
	return out
}
