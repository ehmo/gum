package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadPluginSchemaProfileDir wires XDG_DATA_HOME to a temp dir and
// returns the resolved <data>/gum/<profile> path so each test can
// plant fixture files into it. Tests use profile="default" so the
// path matches what profilePluginDir would compute.
func loadPluginSchemaProfileDir(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(filepath.Join(dir, "plugin-schemas"), 0o755); err != nil {
		t.Fatalf("mkdir plugin-schemas: %v", err)
	}
	return dir
}

func writeJSONFile(t *testing.T, path string, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLoadPluginSchemaNoMatchingRefReturnsFalse pins
// loadPluginSchema's `lookupPluginSchemaHash !ok → return nil, false`
// arm (schema_resource.go:136-138). plugin-catalog.json is well-formed
// and has variants[] but none of them carry the requested ref in
// schema_hashes, so the inventory says "this ref isn't ours" and the
// resolver must return false BEFORE touching plugin-state.json.
func TestLoadPluginSchemaNoMatchingRefReturnsFalse(t *testing.T) {
	dir := loadPluginSchemaProfileDir(t)
	writeJSONFile(t, filepath.Join(dir, "plugin-catalog.json"), map[string]any{
		"variants": []any{
			map[string]any{
				"owner_plugin":  "p1",
				"schema_hashes": map[string]any{"other.ref": "abc123"},
			},
		},
	})
	s := &Server{profile: "default"}
	body, ok := s.loadPluginSchema("missing.ref")
	if ok || body != nil {
		t.Errorf("loadPluginSchema(missing-ref) = (%d bytes, %v); want (nil, false)", len(body), ok)
	}
}

// TestLoadPluginSchemaMissingStateRowReturnsFalse pins the
// `stateRow == nil → return nil, false` arm (schema_resource.go:140-142).
// Catalog claims plugin "ghost" owns ref X, but plugin-state.json has
// no row for "ghost". The resolver must NOT serve the schema for a
// plugin that isn't represented in the state inventory.
func TestLoadPluginSchemaMissingStateRowReturnsFalse(t *testing.T) {
	dir := loadPluginSchemaProfileDir(t)
	writeJSONFile(t, filepath.Join(dir, "plugin-catalog.json"), map[string]any{
		"variants": []any{
			map[string]any{
				"owner_plugin":  "ghost",
				"schema_hashes": map[string]any{"orphan.ref": "deadbeef"},
			},
		},
	})
	writeJSONFile(t, filepath.Join(dir, "plugin-state.json"), map[string]any{
		"plugins": []any{
			map[string]any{"name": "other-plugin"},
		},
	})
	s := &Server{profile: "default"}
	body, ok := s.loadPluginSchema("orphan.ref")
	if ok || body != nil {
		t.Errorf("loadPluginSchema(no-state) = (%d bytes, %v); want (nil, false)", len(body), ok)
	}
}

// TestLoadPluginSchemaBodyFileMissingReturnsFalse pins the
// `os.ReadFile err → return nil, false` arm (schema_resource.go:151-153).
// Catalog + active state row both point at ref X under
// plugin-schemas/X.<hash>.json, but the file isn't on disk — the
// resolver must surface RESOURCE_NOT_FOUND instead of panicking.
func TestLoadPluginSchemaBodyFileMissingReturnsFalse(t *testing.T) {
	dir := loadPluginSchemaProfileDir(t)
	writeJSONFile(t, filepath.Join(dir, "plugin-catalog.json"), map[string]any{
		"variants": []any{
			map[string]any{
				"owner_plugin":  "active-plugin",
				"schema_hashes": map[string]any{"present.ref": "hash1"},
			},
		},
	})
	writeJSONFile(t, filepath.Join(dir, "plugin-state.json"), map[string]any{
		"plugins": []any{
			map[string]any{"name": "active-plugin"},
		},
	})
	// Intentionally do NOT write plugin-schemas/present.ref.hash1.json.
	s := &Server{profile: "default"}
	body, ok := s.loadPluginSchema("present.ref")
	if ok || body != nil {
		t.Errorf("loadPluginSchema(missing-body) = (%d bytes, %v); want (nil, false)", len(body), ok)
	}
}

// TestLoadPluginSchemaBodyFileBadJSONReturnsFalse pins the
// `jcsCanonicaliseBytes err → return nil, false` arm
// (schema_resource.go:155-157). Catalog + state agree, body file
// exists but contains junk — canonicalisation fails and the resolver
// must NOT return invalid bytes downstream.
func TestLoadPluginSchemaBodyFileBadJSONReturnsFalse(t *testing.T) {
	dir := loadPluginSchemaProfileDir(t)
	writeJSONFile(t, filepath.Join(dir, "plugin-catalog.json"), map[string]any{
		"variants": []any{
			map[string]any{
				"owner_plugin":  "active-plugin",
				"schema_hashes": map[string]any{"corrupt.ref": "hash2"},
			},
		},
	})
	writeJSONFile(t, filepath.Join(dir, "plugin-state.json"), map[string]any{
		"plugins": []any{
			map[string]any{"name": "active-plugin"},
		},
	})
	bodyPath := filepath.Join(dir, "plugin-schemas", "corrupt.ref.hash2.json")
	if err := os.WriteFile(bodyPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt body: %v", err)
	}
	s := &Server{profile: "default"}
	body, ok := s.loadPluginSchema("corrupt.ref")
	if ok || body != nil {
		t.Errorf("loadPluginSchema(bad-json) = (%d bytes, %v); want (nil, false)", len(body), ok)
	}
}
