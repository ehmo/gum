package adapters_test

// Spec §5.8 "Capability classes", checklist item 6: every capability atom that
// is not generic-executable needs a contract test proving where it does
// execute. `code_execution` is the one atom in the typed-executor group. This
// file is that proof for it.
//
// The test is fixture-backed against the shipped catalog rather than a
// hand-built variant. A hand-built variant would prove only that CodeRunner
// runs Risor, which the rest of internal/adapters already covers. What §5.8
// needs proven is the binding: the atom the catalog declares, the executor
// that claims it, and the absence of an HTTP request.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

// codeExecutionVariantID is the one shipped variant that declares the atom.
const codeExecutionVariantID = "gum.code.v1.risor"

// loadCodeExecutionVariant returns the shipped catalog's code_execution
// variant. It fails rather than skips: the atom is documented in
// docs/catalog-abi.md as carried by this variant, so its disappearance is a
// regression, not an environment difference.
func loadCodeExecutionVariant(t *testing.T) (*catalog.Op, *catalog.Variant) {
	t.Helper()

	if len(embedded.CatalogJSON) == 0 {
		t.Fatal("embedded.CatalogJSON is empty; build the catalog before running this test")
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for j := range op.Variants {
			v := &op.Variants[j]
			if v.VariantID == codeExecutionVariantID {
				return op, v
			}
		}
	}
	t.Fatalf("variant %s is not in the shipped catalog", codeExecutionVariantID)
	return nil, nil
}

// TestCapabilityClassCodeExecution is the §5.8 contract test for the
// code_execution atom.
func TestCapabilityClassCodeExecution(t *testing.T) {
	op, variant := loadCodeExecutionVariant(t)

	t.Run("the shipped variant declares the atom and nothing else", func(t *testing.T) {
		if got := variant.Capabilities; !slices.Equal(got, []string{catalog.CapabilityCodeExecution}) {
			t.Fatalf("capabilities = %v, want [%s]", got, catalog.CapabilityCodeExecution)
		}
	})

	t.Run("the atom is executable, so the variant stays full", func(t *testing.T) {
		// §5.8 puts code_execution in the executable group: the typed executor
		// runs it. An executable atom must not downgrade execution_support, and
		// §925 forbids unsupported_capabilities on a full variant.
		if variant.ExecutionSupport != "" && variant.ExecutionSupport != catalog.ExecutionSupportFull {
			t.Fatalf("execution_support = %q, want %q or absent", variant.ExecutionSupport, catalog.ExecutionSupportFull)
		}
		if len(variant.UnsupportedCapabilities) != 0 {
			t.Fatalf("unsupported_capabilities = %v, want empty", variant.UnsupportedCapabilities)
		}
	})

	t.Run("no HTTP request is built from it", func(t *testing.T) {
		// The atom's whole point is that generic dispatch never sees it. A
		// binding.http on this variant would mean the generic request builder
		// could reach it, which no adapter would honour.
		if variant.Binding != nil && variant.Binding.HTTP != nil {
			t.Fatalf("variant %s carries binding.http; code_execution builds no request", variant.VariantID)
		}
		if variant.Binding == nil || variant.Binding.AdapterKey != "code.risor" {
			t.Fatalf("adapter key = %+v, want code.risor", variant.Binding)
		}
	})

	t.Run("the typed executor runs the script", func(t *testing.T) {
		rv := &dispatch.ResolvedVariant{
			OpID:       op.OpID,
			Variant:    variant,
			AdapterKey: variant.Binding.AdapterKey,
		}
		inv := &dispatch.Invocation{
			OpID: op.OpID,
			Args: map[string]any{
				"language": "risor",
				"source":   `gum_print(1 + 1)`,
			},
		}

		resp, err := adapters.NewCodeRunner().Execute(context.Background(), inv, rv, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got := strings.TrimSpace(string(resp.Body)); got != "2" {
			t.Fatalf("body = %q, want %q", got, "2")
		}
	})
}
