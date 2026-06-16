package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// TestHandleWriteVariantIDAndFlagsPromoteToInvocation pins handleRiskTier's
// arg-promotion arms at handlers.go:264-280 + 285-286:
//   - variant_id stripped from Args, promoted to inv.RequestedVariantID
//   - allow_write/allow_destructive/confirmed/confirmation_token mirrored
//   - switch RiskClassWrite → AllowWrite=true (the policy gate would
//     otherwise reject the write-class op).
//
// Drives all four arms through handleWrite with a write-class op so the
// dispatch reaches the captureDispatcher (no risk mismatch). The
// captureDispatcher records the Invocation we then introspect.
func TestHandleWriteVariantIDAndFlagsPromoteToInvocation(t *testing.T) {
	const opID = "drive.files.update"
	const customVariantID = opID + ".v2.preview"

	// Build a write-class op with two variants so RequestedVariantID
	// has somewhere to land.
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            opID,
		Summary:          "test write op",
		DefaultVariantID: opID + ".v1.test",
		Variants: []catalog.Variant{
			{
				VariantID:     opID + ".v1.test",
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind:   catalog.BackendKindDiscoveryREST,
				RiskClass:     catalog.RiskClassWrite,
			},
			{
				VariantID:     customVariantID,
				Stability:     catalog.StabilityBeta,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind:   catalog.BackendKindDiscoveryREST,
				RiskClass:     catalog.RiskClassWrite,
			},
		},
	}
	snap := minimalCatalog(op)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	raw, _ := json.Marshal(map[string]any{
		"op_id":              opID,
		"variant_id":         customVariantID,
		"allow_destructive":  true,
		"confirmed":          true,
		"confirmation_token": "ct-abc",
		"format":             "json",
	})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.write", Arguments: raw},
	}
	if _, err := srv.handleWrite(context.Background(), req); err != nil {
		t.Fatalf("handleWrite go err: %v", err)
	}
	if len(cd.Calls) != 1 {
		t.Fatalf("dispatcher Calls=%d; want 1", len(cd.Calls))
	}
	inv := cd.Calls[0]

	if inv.RequestedVariantID != customVariantID {
		t.Errorf("RequestedVariantID=%q; want %q (variant_id arg must promote)", inv.RequestedVariantID, customVariantID)
	}
	if _, present := inv.Args["variant_id"]; present {
		t.Errorf("Args still contains variant_id; must be deleted after promotion")
	}
	if !inv.AllowWrite {
		t.Errorf("AllowWrite=false; want true (RiskClassWrite switch case must default it)")
	}
	if !inv.AllowDestructive {
		t.Errorf("AllowDestructive=false; want true (caller arg must mirror)")
	}
	if !inv.Confirmed {
		t.Errorf("Confirmed=false; want true (caller arg must mirror)")
	}
	if inv.ConfirmationToken != "ct-abc" {
		t.Errorf("ConfirmationToken=%q; want ct-abc (caller arg must mirror)", inv.ConfirmationToken)
	}
	if inv.Format != "json" {
		t.Errorf("Format=%q; want json", inv.Format)
	}
}
