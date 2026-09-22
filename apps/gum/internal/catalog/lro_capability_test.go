package catalog_test

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestVariantReturnsLRO(t *testing.T) {
	cases := []struct {
		name         string
		capabilities []string
		want         bool
	}{
		{"declares_the_atom", []string{"json_response", catalog.CapabilityLROReturn}, true},
		{"declares_other_atoms", []string{"json_response", "pagination"}, false},
		{"declares_nothing", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := catalog.Variant{VariantID: "v", Capabilities: tc.capabilities}
			if got := v.ReturnsLRO(); got != tc.want {
				t.Fatalf("ReturnsLRO() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestOpDefaultVariantReturnsLRO(t *testing.T) {
	op := catalog.Op{
		OpID:             "svc.act",
		DefaultVariantID: "rest",
		Variants: []catalog.Variant{
			{VariantID: "sdk", Capabilities: []string{catalog.CapabilityLROReturn}},
			{VariantID: "rest", Capabilities: []string{"json_response"}},
		},
	}

	// Only the default variant counts: "sdk" declares the atom and must not
	// pull the op into the code-mode refusal.
	if op.DefaultVariantReturnsLRO() {
		t.Fatal("DefaultVariantReturnsLRO() = true; want false when only a non-default variant declares the atom")
	}

	op.Variants[1].Capabilities = append(op.Variants[1].Capabilities, catalog.CapabilityLROReturn)
	if !op.DefaultVariantReturnsLRO() {
		t.Fatal("DefaultVariantReturnsLRO() = false; want true once the default variant declares the atom")
	}

	op.DefaultVariantID = "absent"
	if op.DefaultVariantReturnsLRO() {
		t.Fatal("DefaultVariantReturnsLRO() = true for a dangling default_variant_id; want false")
	}
}
