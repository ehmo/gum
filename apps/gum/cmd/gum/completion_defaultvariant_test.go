package main

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestDefaultVariant covers the three branches of the completion helper:
//   - Default variant is present in the op → return it.
//   - Default is missing but at least one variant exists → fall back to
//     variants[0] (deterministic by catalog ordering).
//   - No variants at all → return nil (caller suppresses completion).
func TestDefaultVariant(t *testing.T) {
	t.Run("matches_default_variant_id", func(t *testing.T) {
		op := &catalog.Op{
			DefaultVariantID: "v2",
			Variants: []catalog.Variant{
				{VariantID: "v1"},
				{VariantID: "v2"},
				{VariantID: "v3"},
			},
		}
		got := defaultVariant(op)
		if got == nil || got.VariantID != "v2" {
			t.Errorf("got %+v, want VariantID=v2", got)
		}
	})

	t.Run("default_missing_falls_back_to_first", func(t *testing.T) {
		op := &catalog.Op{
			DefaultVariantID: "absent",
			Variants: []catalog.Variant{
				{VariantID: "first"},
				{VariantID: "second"},
			},
		}
		got := defaultVariant(op)
		if got == nil || got.VariantID != "first" {
			t.Errorf("got %+v, want VariantID=first", got)
		}
	})

	t.Run("no_variants_returns_nil", func(t *testing.T) {
		op := &catalog.Op{DefaultVariantID: "v1"}
		if got := defaultVariant(op); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}
