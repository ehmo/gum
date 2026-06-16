package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestHandleCodeForwardsAllowFlags pins the two bool-extract arms of
// handleCode: when the caller passes allow_write=true / allow_destructive=true
// as args, the handler MUST read them and stamp the kernel Invocation so the
// dispatcher sees the elevated risk authorization. Without this the
// allow_*=true assignment arms are dead code.
func TestHandleCodeForwardsAllowFlags(t *testing.T) {
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, minimalCatalog())

	raw, _ := json.Marshal(map[string]any{
		"language":           "risor",
		"source":             "1+1",
		"allow_write":        true,
		"allow_destructive":  true,
		"confirmed":          true,
		"confirmation_token": "tok-code",
	})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.code",
			Arguments: raw,
		},
	}
	if _, err := srv.handleCode(context.Background(), req); err != nil {
		t.Fatalf("handleCode go err=%v", err)
	}
	if len(cd.Calls) != 1 {
		t.Fatalf("dispatcher Calls=%d; want 1", len(cd.Calls))
	}
	inv := cd.Calls[0]
	if inv.OpID != "gum.code" {
		t.Errorf("OpID=%q; want gum.code", inv.OpID)
	}
	if !inv.AllowWrite {
		t.Error("AllowWrite=false; want true (allow_write arg was true)")
	}
	if !inv.AllowDestructive {
		t.Error("AllowDestructive=false; want true (allow_destructive arg was true)")
	}
	if !inv.Confirmed {
		t.Error("Confirmed=false; want true")
	}
	if inv.ConfirmationToken != "tok-code" {
		t.Errorf("ConfirmationToken=%q; want tok-code", inv.ConfirmationToken)
	}
	if _, ok := inv.Args["confirmation_token"]; ok {
		t.Fatal("confirmation_token leaked into Invocation.Args")
	}
	if _, ok := inv.Args["confirmed"]; ok {
		t.Fatal("confirmed leaked into Invocation.Args")
	}
}

// TestHandleCodeIgnoresNonBoolAllowFlags pins the ok=false arm of the
// type assertion: when allow_write is a string (operator typo) the handler
// must NOT stamp AllowWrite — the inv stays at its zero value.
func TestHandleCodeIgnoresNonBoolAllowFlags(t *testing.T) {
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, minimalCatalog())

	raw, _ := json.Marshal(map[string]any{
		"allow_write":       "yes", // wrong type
		"allow_destructive": 1,     // wrong type
	})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.code", Arguments: raw},
	}
	if _, err := srv.handleCode(context.Background(), req); err != nil {
		t.Fatalf("handleCode go err=%v", err)
	}
	if len(cd.Calls) != 1 {
		t.Fatalf("dispatcher Calls=%d; want 1", len(cd.Calls))
	}
	inv := cd.Calls[0]
	if inv.AllowWrite {
		t.Error("AllowWrite=true; want false (string is not bool)")
	}
	if inv.AllowDestructive {
		t.Error("AllowDestructive=true; want false (int is not bool)")
	}
}

// Silence unused import when catalog isn't referenced directly.
var _ = catalog.RiskClassRead

func TestHandleCodeElevatedRequiresConfirmationToken(t *testing.T) {
	snap := minimalCatalog(gumCodeOpForConfirmationTest())
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	})
	srv := NewServerWithCatalog(disp, snap)

	first := gumCodeRequest(map[string]any{
		"language":    "risor",
		"source":      `gum_print("executed")`,
		"allow_write": true,
	})
	res, err := srv.handleCode(context.Background(), first)
	if err != nil {
		t.Fatalf("handleCode first: %v", err)
	}
	m := parseErrorResult(t, res)
	if m["error_code"] != "REQUIRES_CONFIRMATION" {
		t.Fatalf("error_code=%v; want REQUIRES_CONFIRMATION", m["error_code"])
	}
	token, _ := m["confirmation_token"].(string)
	if token == "" {
		t.Fatal("missing confirmation_token")
	}

	second := gumCodeRequest(map[string]any{
		"language":           "risor",
		"source":             `gum_print("executed")`,
		"allow_write":        true,
		"confirmed":          true,
		"confirmation_token": token,
	})
	res, err = srv.handleCode(context.Background(), second)
	if err != nil {
		t.Fatalf("handleCode confirmed: %v", err)
	}
	if res.IsError {
		t.Fatalf("confirmed handleCode returned error: %#v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("confirmed handleCode returned no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0]=%T; want TextContent", res.Content[0])
	}
	if tc.Text != "executed" {
		t.Fatalf("confirmed output=%q; want executed", tc.Text)
	}
}

func gumCodeRequest(args map[string]any) *sdkmcp.CallToolRequest {
	raw, _ := json.Marshal(args)
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.code",
			Arguments: raw,
		},
	}
}

func gumCodeOpForConfirmationTest() catalog.Op {
	return catalog.Op{
		OpID:             "gum.code",
		OpSchemaVersion:  1,
		Title:            "Run code",
		Summary:          "test gum.code op",
		DefaultVariantID: "gum.code.v1.test",
		Variants: []catalog.Variant{
			{
				VariantID:     "gum.code.v1.test",
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindSDKNative,
				BackendKind:   catalog.BackendKindTypedRestSDK,
				RiskClass:     catalog.RiskClassRead,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "code.risor",
					OperationKey:         "gum.code",
				},
			},
		},
	}
}
