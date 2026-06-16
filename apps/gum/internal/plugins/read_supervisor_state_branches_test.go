package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestReadSupervisorStateLoadErrorPropagates pins the reg.Load() error
// arm: a malformed plugin-state.json (unsupported schema version) must
// surface as an error to ReadSupervisorState callers, not be silently
// swallowed as a "no state" result.
func TestReadSupervisorStateLoadErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, registry.StateFilename),
		[]byte(`{"plugin_state_schema_version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := registry.New(dir)
	_, err := plugins.ReadSupervisorState(reg, "anyplugin")
	if err == nil {
		t.Fatal("want schema error; got nil")
	}
}

// TestReadSupervisorStateNonMapEntrySkipped pins the type-assertion
// skip arm: when plugins[] contains a non-object element (a stray
// string or array from hand-edit), the loop must `continue` rather
// than panic. The named plugin is therefore "not found" → zero state.
func TestReadSupervisorStateNonMapEntrySkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, registry.StateFilename),
		[]byte(`{"plugin_state_schema_version":1,"plugins":["a stray string"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := registry.New(dir)
	state, err := plugins.ReadSupervisorState(reg, "anyplugin")
	if err != nil {
		t.Fatalf("err=%v; want nil (non-map entry should be skipped)", err)
	}
	if state.Quarantined || state.RetryCount != 0 {
		t.Errorf("state=%+v; want zero (plugin not present)", state)
	}
}

// TestReadSupervisorStatePluginNotFoundReturnsZero pins the
// fall-through return at end of loop: when the named plugin has no
// row, the result is the zero-value SupervisorState (not quarantined),
// NOT an error.
func TestReadSupervisorStatePluginNotFoundReturnsZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, registry.StateFilename),
		[]byte(`{"plugin_state_schema_version":1,"plugins":[{"name":"someone-else"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := registry.New(dir)
	state, err := plugins.ReadSupervisorState(reg, "missing")
	if err != nil {
		t.Fatalf("err=%v; want nil", err)
	}
	if state.Quarantined || state.RetryCount != 0 || state.LastErrorCode != "" {
		t.Errorf("state=%+v; want zero", state)
	}
}
