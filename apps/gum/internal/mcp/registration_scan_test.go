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
// AddTool registration uses a *sdkmcp.Tool composite literal that explicitly
// sets both InputSchema AND OutputSchema. Required by docs/test-matrix.md:
// "All Tier A tool registrations include outputSchema".
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

	var sites []registrationSite
	for _, f := range files {
		fname := fset.Position(f.Pos()).Filename
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "AddTool" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			// First arg should be the tool descriptor. Unwrap &Tool{...}.
			toolLit := unwrapToolLiteral(call.Args[0])
			if toolLit == nil {
				return true
			}
			pos := fset.Position(call.Pos())
			site := registrationSite{
				File:        filepath.Base(fname),
				Line:        pos.Line,
				ToolLiteral: toolLit,
			}
			site.HasInputSchema, site.HasOutputSchema = inspectToolLiteral(toolLit)
			sites = append(sites, site)
			return true
		})
	}

	if len(sites) == 0 {
		t.Fatal("no AddTool registrations found via AST scan; test would silently pass")
	}

	var failures []string
	for _, s := range sites {
		if !s.HasInputSchema {
			failures = append(failures, fmt.Sprintf("%s:%d: AddTool literal missing InputSchema field", s.File, s.Line))
		}
		if !s.HasOutputSchema {
			failures = append(failures, fmt.Sprintf("%s:%d: AddTool literal missing OutputSchema field (spec §4: every Tier A tool must declare outputSchema)", s.File, s.Line))
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
// expression is a unary & on a composite literal whose type name ends in
// "Tool". Returns nil otherwise.
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

