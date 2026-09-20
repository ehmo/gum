package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsSchemaHashRejectsNonHex pins the plugin-catalog schema_hashes value to
// lowercase sha256 hex. The value becomes a path segment under
// <profile>/plugin-schemas/<ref>.<hash>.json, so a row carrying `../` there was
// a read primitive for any file the process could reach.
func TestIsSchemaHashRejectsNonHex(t *testing.T) {
	good := "a2c799262a3ce3c19ef5cdd983bf3d12b43ab3c426227091b909dcb7054738c0"
	if !isSchemaHash(good) {
		t.Fatalf("isSchemaHash(%q) = false; want true", good)
	}

	bad := []string{
		"",
		"deadbeef",
		"../../../../etc/passwd",
		"../../../../../../etc/passwd\x00",
		good[:63] + "/",
		good[:63] + "G",
		strings.ToUpper(good),
		good + "0",
	}
	for _, h := range bad {
		if isSchemaHash(h) {
			t.Errorf("isSchemaHash(%q) = true; want false", h)
		}
	}
}

// TestLoadPluginSchemaRefusesTraversalHash proves the guard end to end: a
// plugin-catalog row whose schema_hashes value is a relative path must not
// serve a file from outside plugin-schemas/.
func TestLoadPluginSchemaRefusesTraversalHash(t *testing.T) {
	dir := loadPluginSchemaProfileDir(t)
	// Plant a readable JSON file one level above plugin-schemas/.
	secret := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"token":"s3cret"}`), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	writeJSONFile(t, filepath.Join(dir, "plugin-catalog.json"), map[string]any{
		"variants": []any{
			map[string]any{
				"owner_plugin": "rogue",
				// ref+"."+hash+".json" renders "x.json/../../secret.json", which
				// filepath.Join cleans to <profileDir>/secret.json.
				"schema_hashes": map[string]any{"x": "json/../../secret"},
			},
		},
	})
	writeJSONFile(t, filepath.Join(dir, "plugin-state.json"), map[string]any{
		"plugins": []any{map[string]any{"name": "rogue", "status": "active"}},
	})

	s := &Server{profile: "default"}
	body, ok := s.loadPluginSchema("x")
	if ok || body != nil {
		t.Errorf("loadPluginSchema with traversal hash = (%d bytes, %v); want (nil, false)", len(body), ok)
	}
}
