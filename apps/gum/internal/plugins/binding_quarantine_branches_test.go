package plugins_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestQuarantinePluginSkipsNonMapAndNameMismatch pins the two loop
// `continue` arms inside QuarantinePlugin (binding.go:143-144 non-map
// skip, 146-147 name-mismatch skip). Seeds the state with a junk
// element, a different-name row, then the target row, and asserts the
// target row is the one mutated.
func TestQuarantinePluginSkipsNonMapAndNameMismatch(t *testing.T) {
	t.Parallel()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.State.Plugins = append(f.State.Plugins,
			"not-a-map", // 143-144 non-map skip
			map[string]any{ // 146-147 name mismatch skip
				"name":        "other-plugin",
				"quarantined": false,
			},
			map[string]any{ // target row
				"name":        "target-plugin",
				"quarantined": false,
			},
		)
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := plugins.QuarantinePlugin(context.Background(), reg, "target-plugin", "TEST_CODE"); err != nil {
		t.Fatalf("QuarantinePlugin: %v", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The non-map element should be unchanged.
	if s, _ := files.State.Plugins[0].(string); s != "not-a-map" {
		t.Errorf("plugin[0]=%v; want 'not-a-map' preserved", files.State.Plugins[0])
	}
	// The other-plugin row should be unchanged.
	other, _ := files.State.Plugins[1].(map[string]any)
	if q, _ := other["quarantined"].(bool); q {
		t.Errorf("other-plugin quarantined=true; want untouched")
	}
	// The target row should be quarantined.
	target, _ := files.State.Plugins[2].(map[string]any)
	if q, _ := target["quarantined"].(bool); !q {
		t.Errorf("target-plugin quarantined=%v; want true after QuarantinePlugin", target["quarantined"])
	}
	if c, _ := target["last_error_code"].(string); c != "TEST_CODE" {
		t.Errorf("last_error_code=%q; want TEST_CODE", c)
	}
}

// TestQuarantinePluginUnknownNameReturnsNil pins the loop-exhausted
// arm (binding.go:155). When no row matches the requested name, the
// docstring specifies the function returns nil (operator presumably
// removed the install).
func TestQuarantinePluginUnknownNameReturnsNil(t *testing.T) {
	t.Parallel()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name": "some-other-plugin",
		})
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := plugins.QuarantinePlugin(context.Background(), reg, "absent", "CODE"); err != nil {
		t.Errorf("QuarantinePlugin(absent) err=%v; want nil per docstring", err)
	}
}
