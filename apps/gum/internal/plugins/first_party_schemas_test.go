package plugins_test

import (
	"errors"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestFirstPartySchemaRefsCoverTheEmbeddedStore pins the inventory the
// collision check now reads. The store ships one request schema per op that
// declares request_fields, so the walk must return a populated, sorted,
// hashed list rather than the empty slice it returned while the store held
// only a placeholder.
func TestFirstPartySchemaRefsCoverTheEmbeddedStore(t *testing.T) {
	defer goleak.VerifyNone(t)

	refs := plugins.FirstPartySchemaRefs()
	if len(refs) < 2 {
		t.Fatalf("FirstPartySchemaRefs returned %d refs; want the generated store", len(refs))
	}

	seen := map[string]bool{}
	for i, r := range refs {
		if r.Ref == "" {
			t.Fatalf("refs[%d] has an empty ref", i)
		}
		if len(r.Hash) != 64 {
			t.Errorf("refs[%d] (%s) hash = %q; want 64 hex chars", i, r.Ref, r.Hash)
		}
		if r.OwnerPlugin != plugins.FirstPartySchemaOwner {
			t.Errorf("refs[%d] (%s) owner = %q; want %q", i, r.Ref, r.OwnerPlugin, plugins.FirstPartySchemaOwner)
		}
		if seen[r.Ref] {
			t.Errorf("refs[%d] repeats ref %s", i, r.Ref)
		}
		seen[r.Ref] = true

		if i > 0 && refs[i-1].Ref > r.Ref {
			t.Errorf("refs are unsorted at %d: %s before %s", i, refs[i-1].Ref, r.Ref)
		}
	}

	if !seen["gmail.users.messages.list.request"] {
		t.Error("store is missing gmail.users.messages.list.request")
	}
}

// TestPluginCannotClaimAFirstPartyRef is the step-3 acceptance for gum-wzmb.
// Both stores are served through one gum://schema/{ref} namespace, so a
// plugin publishing a first-party ref with a different body must fail
// install with SCHEMA_REF_COLLISION, on an empty registry, where the
// plugin-catalog inventory alone would have said yes.
func TestPluginCannotClaimAFirstPartyRef(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())

	candidate := []plugins.SchemaRef{
		{Ref: "gmail.users.messages.list.request", Hash: strings.Repeat("a", 64), OwnerPlugin: "impostor"},
	}

	err := plugins.ValidateNewPluginSchemas(reg, "impostor", candidate)
	if !errors.Is(err, plugins.ErrSchemaRefCollision) {
		t.Fatalf("ValidateNewPluginSchemas err = %v; want SCHEMA_REF_COLLISION", err)
	}
	if !strings.Contains(err.Error(), plugins.FirstPartySchemaOwner) {
		t.Errorf("err = %q; want it to name %q so the operator knows no uninstall clears it",
			err.Error(), plugins.FirstPartySchemaOwner)
	}
}

// TestPluginMayMirrorAFirstPartyRefByteForByte keeps the contract's
// identical-body reuse rule intact across the widened inventory: same ref,
// same canonical digest is not a collision.
func TestPluginMayMirrorAFirstPartyRefByteForByte(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())

	var mirrored plugins.SchemaRef
	for _, r := range plugins.FirstPartySchemaRefs() {
		if r.Ref == "gmail.users.messages.list.request" {
			mirrored = r
			break
		}
	}
	if mirrored.Ref == "" {
		t.Fatal("embedded store is missing gmail.users.messages.list.request")
	}

	candidate := []plugins.SchemaRef{
		{Ref: mirrored.Ref, Hash: mirrored.Hash, OwnerPlugin: "mirror"},
	}
	if err := plugins.ValidateNewPluginSchemas(reg, "mirror", candidate); err != nil {
		t.Fatalf("identical-body reuse = %v; want nil", err)
	}
}
