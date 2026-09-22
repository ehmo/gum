package mcp

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTierARegistrationScan AST-scans the mcp package and asserts that every
// *sdkmcp.Tool composite literal declares both an input schema and an output
// schema. Required by docs/test-matrix.md: "All Tier A tool registrations
// include outputSchema".
//
// The scan walks every Tool literal in the package rather than only the ones
// passed inline to AddTool. Two of the three registration sites build the
// literal first and pass an identifier to AddTool, so an arguments-only scan
// never saw them.
//
// A literal satisfies the output-schema rule in one of two ways: it sets the
// OutputSchema field directly, or it is wrapped in setOutputSchema(lit, expr),
// which assigns the field only when the schema is non-empty (bead gum-tq5v).
// The wrapper form is syntax, not a runtime value, so it proves the site
// supplies a schema expression, not that the expression is non-nil. The wire
// shape is gated separately by TestToolsListNeverPutsNullOutputSchemaOnTheWire
// and the pairing gate.
func TestTierARegistrationScan(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read mcp pkg dir: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, parser.AllErrors)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no go files found in current dir")
	}

	// First pass: record every Tool literal that setOutputSchema wraps.
	wrapped := map[token.Pos]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "setOutputSchema" {
				return true
			}
			if lit := unwrapToolLiteral(call.Args[0]); lit != nil {
				wrapped[lit.Pos()] = true
			}
			return true
		})
	}

	// Second pass: every Tool literal in the package is a registration site.
	var sites []registrationSite
	for _, f := range files {
		fname := fset.Position(f.Pos()).Filename
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !typeIsTool(lit.Type) {
				return true
			}
			pos := fset.Position(lit.Pos())
			site := registrationSite{
				File:        filepath.Base(fname),
				Line:        pos.Line,
				ToolLiteral: lit,
			}
			site.HasInputSchema, site.HasOutputSchema = inspectToolLiteral(lit)
			site.HasOutputSchema = site.HasOutputSchema || wrapped[lit.Pos()]
			sites = append(sites, site)
			return true
		})
	}

	// Three sites register tools: meta, skills and convenience. A refactor
	// that drops below that is a scan that stopped seeing its subject.
	const wantSites = 3
	if len(sites) < wantSites {
		t.Fatalf("AST scan found %d *sdkmcp.Tool literals, want at least %d; the scan stopped matching its subject and would silently pass", len(sites), wantSites)
	}

	var failures []string
	for _, s := range sites {
		if !s.HasInputSchema {
			failures = append(failures, fmt.Sprintf("%s:%d: AddTool literal missing InputSchema field", s.File, s.Line))
		}
		if !s.HasOutputSchema {
			failures = append(failures, fmt.Sprintf("%s:%d: Tool literal declares no output schema: set the OutputSchema field or wrap the literal in setOutputSchema (spec §4: every Tier A tool must declare outputSchema)", s.File, s.Line))
		}
	}
	if len(failures) > 0 {
		t.Fatalf("Tier A registration scan failed (%d sites checked):\n  %s",
			len(sites), strings.Join(failures, "\n  "))
	}
}

// TestTierARegistrationOutputSchemasValid ensures the per-tool output schemas
// returned by metaToolOutputSchema / convenienceToolOutputSchema are
// well-formed JSON Schemas with root type "object" (go-sdk requirement,
// spec §4: oneOf branches still root-type object). Also verifies $defs
// preservation per spec §13.
func TestTierARegistrationOutputSchemasValid(t *testing.T) {
	check := func(toolName string, raw json.RawMessage) {
		t.Helper()
		if len(raw) == 0 {
			t.Errorf("%s: outputSchema is empty", toolName)
			return
		}
		var s map[string]any
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Errorf("%s: outputSchema not valid JSON: %v", toolName, err)
			return
		}
		if typ, _ := s["type"].(string); typ != "object" {
			t.Errorf("%s: outputSchema root type=%q want \"object\"", toolName, typ)
		}
		if _, ok := s["$defs"]; !ok {
			t.Errorf("%s: outputSchema missing $defs (spec §13 schema reference fragment)", toolName)
		}
	}
	for _, name := range metaToolNames {
		check(name, metaToolOutputSchema(name))
	}
	for _, name := range tierAConvenienceToolNamesList {
		check(name, convenienceToolOutputSchema(name))
	}
}

type registrationSite struct {
	File            string
	Line            int
	ToolLiteral     *ast.CompositeLit
	HasInputSchema  bool
	HasOutputSchema bool
}

// unwrapToolLiteral returns the &sdkmcp.Tool{...} composite literal, if the
// expression is a unary & on a composite literal whose type name is "Tool".
// Returns nil otherwise.
func unwrapToolLiteral(e ast.Expr) *ast.CompositeLit {
	un, ok := e.(*ast.UnaryExpr)
	if !ok || un.Op != token.AND {
		return nil
	}
	cl, ok := un.X.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	if !typeIsTool(cl.Type) {
		return nil
	}
	return cl
}

func typeIsTool(t ast.Expr) bool {
	switch v := t.(type) {
	case *ast.Ident:
		return v.Name == "Tool"
	case *ast.SelectorExpr:
		return v.Sel.Name == "Tool"
	}
	return false
}

func inspectToolLiteral(cl *ast.CompositeLit) (hasInput, hasOutput bool) {
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch ident.Name {
		case "InputSchema":
			hasInput = true
		case "OutputSchema":
			hasOutput = true
		}
	}
	return hasInput, hasOutput
}
