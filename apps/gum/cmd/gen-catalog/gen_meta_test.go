package main

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildMetaOpsEmitsGumCode pins that BuildMetaOps emits the gum.code
// meta-op with the binding the Risor adapter is registered under
// (adapter_key=code.risor) and auth_strategy=none. This op is what makes the
// flagship `gum code` CLI verb and the gum.code MCP tool resolvable in the
// shipped binary — without it the embedded catalog has no gum.code and every
// invocation returns OP_NOT_FOUND (gum-7ras P0).
func TestBuildMetaOpsEmitsGumCode(t *testing.T) {
	ops := BuildMetaOps()

	var code *catalog.Op
	for i := range ops {
		if ops[i].OpID == "gum.code" {
			code = &ops[i]
			break
		}
	}
	if code == nil {
		t.Fatal("BuildMetaOps did not emit a gum.code op")
	}

	if code.DefaultVariantID == "" {
		t.Error("gum.code: default_variant_id is empty")
	}
	if len(code.Variants) != 1 {
		t.Fatalf("gum.code: got %d variants, want 1", len(code.Variants))
	}
	v := code.Variants[0]
	if v.VariantID != code.DefaultVariantID {
		t.Errorf("gum.code: default_variant_id=%q does not match variant_id=%q", code.DefaultVariantID, v.VariantID)
	}
	if v.AuthStrategy != catalog.AuthStrategyNone {
		t.Errorf("gum.code: auth_strategy=%q, want none", v.AuthStrategy)
	}
	if v.InterfaceKind != catalog.InterfaceKindSDKNative {
		t.Errorf("gum.code: interface_kind=%q, want sdk-native", v.InterfaceKind)
	}
	if v.Binding == nil {
		t.Fatal("gum.code: nil binding")
	}
	if v.Binding.AdapterKey != "code.risor" {
		t.Errorf("gum.code: adapter_key=%q, want code.risor", v.Binding.AdapterKey)
	}
	if v.Binding.OperationKey == "" {
		t.Error("gum.code: operation_key is empty")
	}

	// The op must declare its language/source params so the dispatcher's
	// param validation accepts the CLI/MCP payloads.
	hasParam := func(params [][]string, name string) bool {
		for _, p := range params {
			if len(p) > 0 && p[0] == name {
				return true
			}
		}
		return false
	}
	if !hasParam(code.ParamsRequired, "language") {
		t.Error("gum.code: missing required param language")
	}
	if !hasParam(code.ParamsRequired, "source") {
		t.Error("gum.code: missing required param source")
	}

	// And the whole op must pass catalog validation in isolation.
	if err := code.Validate(); err != nil {
		t.Errorf("gum.code op failed catalog validation: %v", err)
	}
}
