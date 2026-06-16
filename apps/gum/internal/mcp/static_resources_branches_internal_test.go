package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadPluginRowsFromFileMalformedJSONReturnsNil pins
// loadPluginRowsFromFile's `json.Unmarshal err → return nil` arm
// (static_resources.go:273-275). A corrupted plugin-state.json or
// plugins.lock MUST degrade silently to "no rows" so the MCP
// resource layer reports an empty inventory rather than failing the
// entire resources/read call — matching loadPluginFileEnvelope's
// fault-tolerance contract for the same files.
func TestLoadPluginRowsFromFileMalformedJSONReturnsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin-state.json")
	// "{" is incomplete JSON — Unmarshal returns SyntaxError.
	if err := os.WriteFile(path, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadPluginRowsFromFile(path); got != nil {
		t.Errorf("loadPluginRowsFromFile(malformed)=%v; want nil", got)
	}
}

// TestLoadPluginInventoryRowsSkipsNamelessRows pins
// loadPluginInventoryRows's `name == "" → continue` arm
// (static_resources.go:225-226). A state row without a "name" field
// (or with name set to a non-string type) MUST be silently dropped
// from the inventory: such rows have no addressable
// gum://plugin/{name} URI, so emitting them would surface untargetable
// stubs to MCP clients.
func TestLoadPluginInventoryRowsSkipsNamelessRows(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Row 1: missing "name" entirely → skipped via the name=="" guard.
	// Row 2: name is non-string (number) → also yields "" via the
	//        `_ := row["name"].(string)` assertion, also skipped.
	// Row 3: real name → surfaced.
	state := `{"plugins":[
		{"status":"active"},
		{"name":42, "status":"active"},
		{"name":"good.example", "status":"active"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	rows := s.loadPluginInventoryRows()
	if len(rows) != 1 {
		t.Fatalf("rows=%v; want exactly 1 (nameless rows must be skipped)", rows)
	}
	if rows[0].Name != "good.example" {
		t.Errorf("rows[0].Name=%q; want good.example", rows[0].Name)
	}
}
