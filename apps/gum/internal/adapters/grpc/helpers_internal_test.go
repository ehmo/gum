package grpc

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestBindingKeyBranches mirrors the genai test: the three nil-guard
// layers (nil rv, nil Variant, nil Binding) plus the happy-path
// AdapterKey readback. Drift here surfaces as a blank adapter_key in
// the unsupported-op error in Execute.
func TestBindingKeyBranches(t *testing.T) {
	if got := bindingKey(nil); got != "" {
		t.Errorf("nil rv got %q; want \"\"", got)
	}
	if got := bindingKey(&dispatch.ResolvedVariant{}); got != "" {
		t.Errorf("nil Variant got %q; want \"\"", got)
	}
	if got := bindingKey(&dispatch.ResolvedVariant{Variant: &catalog.Variant{}}); got != "" {
		t.Errorf("nil Binding got %q; want \"\"", got)
	}
	got := bindingKey(&dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.foo.bar"}},
	})
	if got != "grpc.foo.bar" {
		t.Errorf("happy path got %q", got)
	}
}

// TestArgsForInvokerBranches: nil invocation and nil-Args invocation
// must both produce a usable empty map so downstream code can do
// args["x"] without a separate nil check.
func TestArgsForInvokerBranches(t *testing.T) {
	if got := argsForInvoker(nil); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("nil inv got %v; want empty map", got)
	}
	if got := argsForInvoker(&dispatch.Invocation{}); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("nil Args got %v; want empty map", got)
	}
	want := map[string]any{"k": "v"}
	got := argsForInvoker(&dispatch.Invocation{Args: want})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("populated got %v; want %v", got, want)
	}
}
