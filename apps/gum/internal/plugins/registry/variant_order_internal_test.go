package registry

import (
	"context"
	"testing"
)

// TestWriteTransactionSortsCatalogVariants pins spec §8.7 line 1884: "All three
// objects use arrays sorted by plugin name (and, inside plugin-catalog.json,
// variants sorted by variant_id) before JCS hashing or golden tests."
//
// The publish sorted Lock.Plugins and State.Plugins and left Catalog.Variants
// in append order, so two profiles that installed the same plugins in a
// different order produced byte-different catalogs with identical content.
func TestWriteTransactionSortsCatalogVariants(t *testing.T) {
	dir := t.TempDir()
	r := New(dir)

	// Install order is the reverse of variant_id order, and one plugin
	// contributes two variants so intra-plugin order is covered too.
	if err := r.WriteTransaction(context.Background(), func(f *Files) error {
		f.Catalog.Variants = append(f.Catalog.Variants,
			map[string]any{"variant_id": "zeta.v1.plugin.search"},
			map[string]any{"variant_id": "alpha.v2.plugin.get"},
			map[string]any{"variant_id": "alpha.v1.plugin.get"},
		)
		return nil
	}); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	files, err := r.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"alpha.v1.plugin.get", "alpha.v2.plugin.get", "zeta.v1.plugin.search"}
	if len(files.Catalog.Variants) != len(want) {
		t.Fatalf("variant count = %d; want %d", len(files.Catalog.Variants), len(want))
	}
	for i, w := range want {
		got := variantIDOf(files.Catalog.Variants[i])
		if got != w {
			t.Errorf("variants[%d] = %q; want %q", i, got, w)
		}
	}
}
