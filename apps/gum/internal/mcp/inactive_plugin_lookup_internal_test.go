package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// plantPluginCatalog writes a plugin-catalog.json with the given
// variants slice under <XDG_DATA_HOME>/gum/<profile>/.
func plantPluginCatalog(t *testing.T, profile string, variants []any) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	dir := filepath.Join(tmp, "gum", profile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, err := json.Marshal(map[string]any{"variants": variants})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"), body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

// TestLookupOpOwnerPluginScansVariantsAndReturnsOwner pins
// lookupOpOwnerPlugin's loop arms (inactive_plugin_resource.go:115-127):
//   - non-map element → continue (117-118)
//   - opID mismatch → continue (120-121)
//   - matching row with non-empty owner_plugin → return owner (123-125)
//   - exhausted loop → return ("", false) (127)
func TestLookupOpOwnerPluginScansVariantsAndReturnsOwner(t *testing.T) {
	plantPluginCatalog(t, "default", []any{
		"not-a-map", // 117-118: non-map skip
		map[string]any{"op_id": "other.op", "owner_plugin": "wrong"}, // 120-121: mismatch
		map[string]any{"op_id": "target.op", "owner_plugin": "right-plugin"}, // match
	})
	s := &Server{profile: "default", snapshot: &catalog.Catalog{}}

	owner, ok := s.lookupOpOwnerPlugin("target.op")
	if !ok {
		t.Fatalf("lookupOpOwnerPlugin(target.op) ok=false; want true")
	}
	if owner != "right-plugin" {
		t.Errorf("owner=%q; want 'right-plugin'", owner)
	}

	// Exhaust-loop case: op_id with no matching row → return ("", false).
	owner, ok = s.lookupOpOwnerPlugin("unknown.op")
	if ok {
		t.Errorf("lookupOpOwnerPlugin(unknown)=(%q, true); want (\"\", false)", owner)
	}
}

// TestLookupVariantOwnerPluginScansVariantsAndReturnsOwner is the
// variant_id counterpart — pins inactive_plugin_resource.go:137-149
// (non-map, mismatch, hit, exhaust).
func TestLookupVariantOwnerPluginScansVariantsAndReturnsOwner(t *testing.T) {
	plantPluginCatalog(t, "default", []any{
		42, // 139-140: non-map (number) skip
		map[string]any{"variant_id": "v.other", "owner_plugin": "wrong"}, // 142-143: mismatch
		map[string]any{"variant_id": "v.target", "owner_plugin": "right-plugin"}, // match
	})
	s := &Server{profile: "default", snapshot: &catalog.Catalog{}}

	owner, ok := s.lookupVariantOwnerPlugin("v.target")
	if !ok {
		t.Fatalf("lookupVariantOwnerPlugin(v.target) ok=false; want true")
	}
	if owner != "right-plugin" {
		t.Errorf("owner=%q; want 'right-plugin'", owner)
	}

	owner, ok = s.lookupVariantOwnerPlugin("v.unknown")
	if ok {
		t.Errorf("lookupVariantOwnerPlugin(unknown)=(%q, true); want (\"\", false)", owner)
	}
}
