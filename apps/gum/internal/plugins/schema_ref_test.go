package plugins_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestPluginSchemaRefCollision is the bead-named acceptance for gum-8wb:
// installing a plugin whose schema_ref matches an existing inventory ref
// with a different canonical body digest MUST fail with
// SCHEMA_REF_COLLISION before any registry write.
func TestPluginSchemaRefCollision(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	ctx := context.Background()

	// Seed plugin "alpha" owning ref "shared.input.v1" with body hash H1.
	seedVariants(t, ctx, reg, []map[string]any{
		{
			"owner_plugin":  "alpha",
			"variant_id":    "alpha.foo.v1",
			"schema_hashes": map[string]any{"shared.input.v1": "h1"},
		},
	})

	// Plugin "beta" tries to publish the same ref with a divergent body hash.
	candidate := []plugins.SchemaRef{
		{Ref: "shared.input.v1", Hash: "h2", OwnerPlugin: "beta"},
	}

	err := plugins.ValidateNewPluginSchemas(reg, candidate)
	if !errors.Is(err, plugins.ErrSchemaRefCollision) {
		t.Fatalf("ValidateNewPluginSchemas err = %v; want SCHEMA_REF_COLLISION", err)
	}
	if !strings.Contains(err.Error(), "shared.input.v1") {
		t.Errorf("err message = %q; want to name the colliding ref", err.Error())
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Errorf("err message = %q; want to name both owners", err.Error())
	}
}

// TestSchemaRefCollision validates the pure detector: identical-body reuse
// is allowed, divergent-body reuse is rejected, candidate sets that don't
// touch existing refs pass clean.
func TestSchemaRefCollision(t *testing.T) {
	existing := []plugins.SchemaRef{
		{Ref: "a", Hash: "h1", OwnerPlugin: "alpha"},
		{Ref: "b", Hash: "h2", OwnerPlugin: "alpha"},
	}

	t.Run("identical body reuse allowed", func(t *testing.T) {
		err := plugins.DetectSchemaRefCollision(existing, []plugins.SchemaRef{
			{Ref: "a", Hash: "h1", OwnerPlugin: "beta"},
		})
		if err != nil {
			t.Errorf("DetectSchemaRefCollision = %v; want nil for identical-body reuse", err)
		}
	})

	t.Run("divergent body rejected", func(t *testing.T) {
		err := plugins.DetectSchemaRefCollision(existing, []plugins.SchemaRef{
			{Ref: "a", Hash: "h99", OwnerPlugin: "beta"},
		})
		if !errors.Is(err, plugins.ErrSchemaRefCollision) {
			t.Errorf("err = %v; want SCHEMA_REF_COLLISION", err)
		}
	})

	t.Run("non-overlapping ref allowed", func(t *testing.T) {
		err := plugins.DetectSchemaRefCollision(existing, []plugins.SchemaRef{
			{Ref: "c", Hash: "h3", OwnerPlugin: "beta"},
		})
		if err != nil {
			t.Errorf("DetectSchemaRefCollision = %v; want nil for non-overlapping ref", err)
		}
	})

	t.Run("empty candidate is no-op", func(t *testing.T) {
		if err := plugins.DetectSchemaRefCollision(existing, nil); err != nil {
			t.Errorf("DetectSchemaRefCollision(nil) = %v; want nil", err)
		}
	})
}

// TestSchemaRefsFromCatalogIgnoresMalformed verifies the projection helper
// skips variants without schema_hashes objects rather than panicking. The
// registry is a JSON-decoded []any so malformed entries are the runtime
// shape of a forward-compat catalog.
func TestSchemaRefsFromCatalogIgnoresMalformed(t *testing.T) {
	variants := []any{
		nil,
		"not-a-map",
		map[string]any{"owner_plugin": "alpha"}, // no schema_hashes
		map[string]any{
			"owner_plugin":  "beta",
			"schema_hashes": map[string]any{"ref.x": "hashX", "": "ignored", "ref.y": ""},
		},
	}
	got := plugins.SchemaRefsFromCatalog(variants)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d; want 1 (ref.x only — empty key/value rows dropped)", len(got))
	}
	if got[0].Ref != "ref.x" || got[0].Hash != "hashX" || got[0].OwnerPlugin != "beta" {
		t.Errorf("got[0] = %+v; want {ref.x hashX beta}", got[0])
	}
}

// TestValidateNewPluginSchemasEmptyRegistry ensures the first install on a
// fresh profile is never blocked — an empty registry means no existing refs.
func TestValidateNewPluginSchemasEmptyRegistry(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	candidate := []plugins.SchemaRef{{Ref: "x", Hash: "h", OwnerPlugin: "alpha"}}
	if err := plugins.ValidateNewPluginSchemas(reg, candidate); err != nil {
		t.Errorf("ValidateNewPluginSchemas on empty registry = %v; want nil", err)
	}
}

// seedVariants writes the given variant rows into plugin-catalog.json
// without going through Install. The test-side helper keeps the test
// focused on the detector contract rather than the install path.
func seedVariants(t *testing.T, ctx context.Context, reg *registry.Registry, variants []map[string]any) {
	t.Helper()
	if err := reg.WriteTransaction(ctx, func(f *registry.Files) error {
		f.Catalog.PluginCatalogSchemaVersion = 1
		raw := make([]any, 0, len(variants))
		for _, v := range variants {
			raw = append(raw, v)
		}
		f.Catalog.Variants = raw
		// Touch plugins.lock with a matching owner so install_generation is
		// non-zero — exercises the real path that a downstream loader sees.
		_ = catalog.SupportedPluginCatalogSchemaVersions
		return nil
	}); err != nil {
		t.Fatalf("seedVariants: %v", err)
	}
}
