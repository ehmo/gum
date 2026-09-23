// toolregistration_test.go — keep MCP tool registration inside internal/mcp.
//
// TestTierARegistrationScan (internal/mcp/registration_scan_test.go) parses the
// internal/mcp package and asserts every *sdkmcp.Tool literal declares an input
// schema and an output schema. That scan is complete only while every literal
// lives in that one package: a Tool built in cmd/gum or in an adapter would be
// registered, served and never scanned.
//
// spec §13 states the containment rule this gate enforces. It replaced a
// normative design the tree never had — a `// gum:registration-helper` marker
// comment, a five-directory scan list and two named error codes — none of which
// appeared in a single file.
package lint_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sdkMCPImportPath is the MCP SDK package whose Tool type registers a tool.
const sdkMCPImportPath = "github.com/modelcontextprotocol/go-sdk/mcp"

// registrationPackage is the one package allowed to build or register a Tool.
const registrationPackage = "internal/mcp"

// TestNoToolRegistrationOutsideMCPPackage walks every non-test Go file outside
// internal/mcp and fails on a Tool composite literal or an AddTool call.
func TestNoToolRegistrationOutsideMCPPackage(t *testing.T) {
	root := moduleRoot(t)
	allowed := filepath.Join(root, filepath.FromSlash(registrationPackage))

	var offenses []string
	var scanned int

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == allowed {
				return fs.SkipDir
			}
			if path != root && skipCiteDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, src, parser.AllErrors)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		scanned++

		for _, bad := range toolRegistrationOffenses(fset, f) {
			offenses = append(offenses, relPath(root, bad))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if scanned == 0 {
		t.Fatalf("scanned no Go files under %s; the walk or its skip list changed", root)
	}
	if len(offenses) > 0 {
		t.Errorf("%d MCP tool registration site(s) outside %s. TestTierARegistrationScan "+
			"never sees them, so their schemas are unchecked. Move the registration into %s:\n%s",
			len(offenses), registrationPackage, registrationPackage, strings.Join(offenses, "\n"))
	}
	t.Logf("scanned %d non-test Go files outside %s", scanned, registrationPackage)
}

// toolRegistrationOffenses reports every Tool literal and AddTool call in one
// file, each as "<file>:<line>: <what>". It resolves the SDK package's local
// name per file, so an aliased import is caught and a same-named type from
// another package is not.
func toolRegistrationOffenses(fset *token.FileSet, f *ast.File) []string {
	sdkNames := map[string]bool{}
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != sdkMCPImportPath {
			continue
		}
		if spec.Name != nil {
			sdkNames[spec.Name.Name] = true
			continue
		}
		sdkNames["mcp"] = true
	}

	var out []string
	at := func(pos token.Pos, what string) {
		p := fset.Position(pos)
		out = append(out, fmt.Sprintf("%s:%d: %s", p.Filename, p.Line, what))
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Tool" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && sdkNames[pkg.Name] {
				at(node.Pos(), "builds an MCP Tool literal")
			}
		case *ast.CallExpr:
			// AddTool is a method on the SDK server value, so the receiver's
			// type is not resolvable from the AST alone. No other API in this
			// module is named AddTool, so the name is the whole test.
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddTool" {
				at(node.Pos(), "calls AddTool")
			}
		}
		return true
	})

	return out
}

// TestToolRegistrationOffensesCatchesEachForm pins the matcher. The walk above
// passes on a clean tree whatever the matcher does, which is how the marker
// machinery the spec used to describe read as enforced for so long.
func TestToolRegistrationOffensesCatchesEachForm(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			"aliased Tool literal",
			`package p
import sdkmcp "` + sdkMCPImportPath + `"
func f() any { return &sdkmcp.Tool{Name: "gum.read"} }`,
			1,
		},
		{
			"default-name Tool literal",
			`package p
import "` + sdkMCPImportPath + `"
func f() any { return mcp.Tool{Name: "gum.read"} }`,
			1,
		},
		{
			"AddTool call",
			`package p
func f(srv any) { srv.AddTool(nil, nil) }`,
			1,
		},
		{
			"literal and call together",
			`package p
import sdkmcp "` + sdkMCPImportPath + `"
func f(srv *sdkmcp.Server) { srv.AddTool(&sdkmcp.Tool{Name: "x"}, nil) }`,
			2,
		},
		{
			// A Tool type from somewhere else is not an MCP registration.
			"same-named type from another package",
			`package p
import "example.com/other"
func f() any { return other.Tool{Name: "x"} }`,
			0,
		},
		{
			// Naming the SDK without building a Tool is how most of the tree
			// uses it: params, results, transports.
			"other SDK types",
			`package p
import sdkmcp "` + sdkMCPImportPath + `"
func f() any { return &sdkmcp.CallToolParams{Name: "gum.read"} }`,
			0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", tc.body, parser.AllErrors)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			got := toolRegistrationOffenses(fset, f)
			if len(got) != tc.want {
				t.Errorf("toolRegistrationOffenses = %v (%d); want %d", got, len(got), tc.want)
			}
		})
	}
}
