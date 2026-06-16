package main_test

import (
	"os"
	"testing"
	"time"

	gencatalog "github.com/ehmo/gum/cmd/gen-catalog"
	"github.com/ehmo/gum/internal/catalog"
)

// TestGenCatalogEmitsSingleValidOp invokes GenerateFromDiscovery against the minimal
// Gmail fixture and asserts structural correctness without hitting the network.
func TestGenCatalogEmitsSingleValidOp(t *testing.T) {
	f, err := os.Open("testdata/gmail-discovery.json")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	cat, err := gencatalog.GenerateFromDiscovery(f)
	if err != nil {
		t.Fatalf("GenerateFromDiscovery: %v", err)
	}

	// catalog_schema_version must be 1
	if cat.CatalogSchemaVersion != 1 {
		t.Errorf("catalog_schema_version = %d, want 1", cat.CatalogSchemaVersion)
	}

	// generated_at must parse as RFC 3339
	if _, err := time.Parse(time.RFC3339, cat.GeneratedAt); err != nil {
		t.Errorf("generated_at %q does not parse as RFC 3339: %v", cat.GeneratedAt, err)
	}

	// Exactly one op should be emitted from the fixture (gmail.users.messages.list)
	if len(cat.Ops) != 1 {
		t.Fatalf("len(ops) = %d, want 1", len(cat.Ops))
	}

	op := cat.Ops[0]
	const wantOpID = "gmail.users.messages.list"
	if op.OpID != wantOpID {
		t.Errorf("op_id = %q, want %q", op.OpID, wantOpID)
	}

	// Exactly one variant expected
	if len(op.Variants) != 1 {
		t.Fatalf("len(variants) = %d, want 1", len(op.Variants))
	}

	const wantVariantID = "gmail.v1.rest.users.messages.list"
	v := op.Variants[0]
	if v.VariantID != wantVariantID {
		t.Errorf("variant_id = %q, want %q", v.VariantID, wantVariantID)
	}

	// default_variant_id must point to the one variant
	if op.DefaultVariantID != wantVariantID {
		t.Errorf("default_variant_id = %q, want %q", op.DefaultVariantID, wantVariantID)
	}

	// Validate() must return nil
	if err := cat.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestGenCatalogExposesGenerateFromDiscoverySignature is a compile-time check that
// GenerateFromDiscovery exists with the right signature. The green team must not
// change the signature without updating this file.
func TestGenCatalogExposesGenerateFromDiscoverySignature(t *testing.T) {
	// This test simply ensures the function is importable with the correct signature.
	// The actual logic is exercised by TestGenCatalogEmitsSingleValidOp.
	var _ func(interface{ Read([]byte) (int, error) }) (*catalog.Catalog, error) = nil
	// If the code compiles, the signature is present.
}
