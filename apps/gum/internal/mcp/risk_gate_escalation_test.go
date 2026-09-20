// Package mcp — risk-tier escalation tests for gum.read / gum.write.
//
// Spec anchors:
//   - spec.md §4.1 risk-gate algorithm: the gate runs against the variant the
//     call will execute, which is the pinned variant_id when one is present.
//   - spec.md §4.1 gum.read parameter table: 8 params, and neither allow_write
//     nor allow_destructive is one of them.
package mcp

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mixedRiskOp builds an op whose default variant is read-class and whose second
// variant carries riskClass. The embedded catalog has no such op today, but a
// plugin-contributed catalog can declare one, and the risk gate must not depend
// on that absence.
func mixedRiskOp(opID string, riskClass catalog.RiskClass) catalog.Op {
	op := minimalReadOp(opID, catalog.RiskClassRead)
	op.Variants = append(op.Variants, catalog.Variant{
		VariantID:     opID + ".v2.escalated",
		Stability:     catalog.StabilityStable,
		InterfaceKind: catalog.InterfaceKindDiscoveryREST,
		BackendKind:   catalog.BackendKindDiscoveryREST,
		RiskClass:     riskClass,
	})
	return op
}

func makeToolRequest(tool string, args map[string]any) *sdkmcp.CallToolRequest {
	req := makeReadRequest(args)
	req.Params.Name = tool
	return req
}

// TestGumReadRejectsPinnedHigherRiskVariant pins a destructive variant through
// gum.read. The MCP risk gate read op.DefaultVariantID, so the pin walked past
// it and the call reached the kernel, where only the caller-supplied
// allow_destructive flag stood between the pin and execution.
func TestGumReadRejectsPinnedHigherRiskVariant(t *testing.T) {
	const opID = "drive.files.watch"
	op := mixedRiskOp(opID, catalog.RiskClassDestructive)
	pinned := op.Variants[1].VariantID
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, minimalCatalog(op))

	req := makeReadRequest(map[string]any{"op_id": opID, "variant_id": pinned})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned a Go error: %v", err)
	}

	m := parseErrorResult(t, res)
	if code, _ := m["error_code"].(string); code != "RISK_TOOL_MISMATCH" {
		t.Errorf("error_code=%q; want RISK_TOOL_MISMATCH", code)
	}
	if v, _ := m["variant_id"].(string); v != pinned {
		t.Errorf("variant_id=%q; want the pinned variant %q", v, pinned)
	}
	if v, _ := m["variant_risk_class"].(string); v != "destructive" {
		t.Errorf("variant_risk_class=%q; want \"destructive\"", v)
	}
	if v, _ := m["required_tool"].(string); v != "gum.destructive" {
		t.Errorf("required_tool=%q; want \"gum.destructive\"", v)
	}
	if len(cd.Calls) != 0 {
		t.Errorf("dispatcher called %d time(s); want 0", len(cd.Calls))
	}
}

// TestGumWriteRejectsPinnedDestructiveVariant is the same escalation one tier up:
// a destructive variant pinned through gum.write.
func TestGumWriteRejectsPinnedDestructiveVariant(t *testing.T) {
	const opID = "drive.files.update"
	op := minimalReadOp(opID, catalog.RiskClassWrite)
	op.Variants = append(op.Variants, catalog.Variant{
		VariantID:     opID + ".v2.purge",
		Stability:     catalog.StabilityStable,
		InterfaceKind: catalog.InterfaceKindDiscoveryREST,
		BackendKind:   catalog.BackendKindDiscoveryREST,
		RiskClass:     catalog.RiskClassDestructive,
	})
	pinned := op.Variants[1].VariantID
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, minimalCatalog(op))

	req := makeToolRequest("gum.write", map[string]any{"op_id": opID, "variant_id": pinned, "args": map[string]any{}})
	res, err := srv.handleWrite(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWrite returned a Go error: %v", err)
	}

	m := parseErrorResult(t, res)
	if code, _ := m["error_code"].(string); code != "RISK_TOOL_MISMATCH" {
		t.Errorf("error_code=%q; want RISK_TOOL_MISMATCH", code)
	}
	if v, _ := m["required_tool"].(string); v != "gum.destructive" {
		t.Errorf("required_tool=%q; want \"gum.destructive\"", v)
	}
	if len(cd.Calls) != 0 {
		t.Errorf("dispatcher called %d time(s); want 0", len(cd.Calls))
	}
}

// TestRiskTierIgnoresCallerRiskFlags proves the tier, not the caller, sets the
// policy flags. gum.read, gum.write and gum.destructive all declare
// additionalProperties:false and none of them lists allow_write or
// allow_destructive, yet handleRiskTier copied both out of the args map. No
// runtime input-schema validation exists to stop the undeclared argument, so
// `gum.read {allow_destructive:true}` reached the kernel policy gate with the
// destructive flag already granted.
func TestRiskTierIgnoresCallerRiskFlags(t *testing.T) {
	cases := []struct {
		tool           string
		risk           catalog.RiskClass
		handler        func(*Server) func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error)
		wantWrite      bool
		wantDestructve bool
	}{
		{
			tool: "gum.read",
			risk: catalog.RiskClassRead,
			handler: func(s *Server) func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return s.handleRead
			},
		},
		{
			tool: "gum.write",
			risk: catalog.RiskClassWrite,
			handler: func(s *Server) func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return s.handleWrite
			},
			wantWrite: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			opID := "test." + string(tc.risk)
			cd := &captureDispatcher{}
			srv := NewServerWithCatalog(cd, minimalCatalog(minimalReadOp(opID, tc.risk)))

			req := makeToolRequest(tc.tool, map[string]any{
				"op_id":             opID,
				"args":              map[string]any{},
				"allow_write":       true,
				"allow_destructive": true,
			})
			if _, err := tc.handler(srv)(context.Background(), req); err != nil {
				t.Fatalf("%s handler returned a Go error: %v", tc.tool, err)
			}
			if len(cd.Calls) != 1 {
				t.Fatalf("dispatcher called %d time(s); want 1", len(cd.Calls))
			}
			inv := cd.Calls[0]
			if inv.AllowWrite != tc.wantWrite {
				t.Errorf("AllowWrite=%v; want %v (the tier sets it, not the caller)", inv.AllowWrite, tc.wantWrite)
			}
			if inv.AllowDestructive != tc.wantDestructve {
				t.Errorf("AllowDestructive=%v; want %v (the tier sets it, not the caller)", inv.AllowDestructive, tc.wantDestructve)
			}
		})
	}
}
