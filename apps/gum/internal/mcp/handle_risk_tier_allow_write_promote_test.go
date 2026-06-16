package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// TestHandleAllowWriteExplicitArgMirrorsToInvocation pins
// handlers.go:269-271 — the explicit `allow_write` arg-mirror arm.
// The companion test (TestHandleWriteVariantIDAndFlagsPromote)
// exercises allow_destructive/confirmed/confirmation_token but NOT
// allow_write (the switch-case default covers AllowWrite=true for
// write-class ops). This test pins the explicit-override path so a
// future refactor that drops the if-block (relying solely on the
// switch default) is caught.
func TestHandleAllowWriteExplicitArgMirrorsToInvocation(t *testing.T) {
	const opID = "drive.files.list"
	// Read-class op so the switch default doesn't set AllowWrite —
	// the assignment must come from the explicit if-block.
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            opID,
		Summary:          "test read op",
		DefaultVariantID: opID + ".v1",
		Variants: []catalog.Variant{
			{
				VariantID:     opID + ".v1",
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind:   catalog.BackendKindDiscoveryREST,
				RiskClass:     catalog.RiskClassRead,
			},
		},
	}
	snap := minimalCatalog(op)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	raw, _ := json.Marshal(map[string]any{
		"op_id":       opID,
		"allow_write": true,
	})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.read", Arguments: raw},
	}
	if _, err := srv.handleRead(context.Background(), req); err != nil {
		t.Fatalf("handleRead: %v", err)
	}
	if len(cd.Calls) != 1 {
		t.Fatalf("Calls=%d; want 1", len(cd.Calls))
	}
	if !cd.Calls[0].AllowWrite {
		t.Errorf("AllowWrite=false; want true (explicit allow_write arg must mirror to Invocation)")
	}
}
