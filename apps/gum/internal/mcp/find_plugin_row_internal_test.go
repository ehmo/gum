package mcp

import "testing"

// TestFindPluginRowBranches pins every observable miss reason — nil
// envelope, missing plugins key, plugins-not-array, non-map row in
// plugins[], no name match — plus the happy path. The catalog
// resource handler keys its 404 decision on a nil return, so a
// regression that confuses "missing" with "empty struct" would flip
// a legitimate miss into a corrupt-looking 200.
func TestFindPluginRowBranches(t *testing.T) {
	row := map[string]any{"name": "wanted", "version": "1.0.0"}

	t.Run("nil_envelope", func(t *testing.T) {
		if got := findPluginRow(nil, "wanted"); got != nil {
			t.Errorf("got=%v; want nil", got)
		}
	})

	t.Run("missing_plugins_key", func(t *testing.T) {
		top := map[string]any{"other": "x"}
		if got := findPluginRow(top, "wanted"); got != nil {
			t.Errorf("got=%v; want nil", got)
		}
	})

	t.Run("plugins_not_array", func(t *testing.T) {
		top := map[string]any{"plugins": "not-an-array"}
		if got := findPluginRow(top, "wanted"); got != nil {
			t.Errorf("got=%v; want nil", got)
		}
	})

	t.Run("non_map_row_skipped", func(t *testing.T) {
		top := map[string]any{"plugins": []any{"garbage", row}}
		got := findPluginRow(top, "wanted")
		if got == nil {
			t.Fatalf("got=nil; want row after skipping garbage")
		}
		if got["name"] != "wanted" {
			t.Errorf("matched wrong row: %v", got)
		}
	})

	t.Run("no_name_match", func(t *testing.T) {
		top := map[string]any{"plugins": []any{row}}
		if got := findPluginRow(top, "missing"); got != nil {
			t.Errorf("got=%v; want nil for unknown name", got)
		}
	})

	t.Run("happy_path", func(t *testing.T) {
		top := map[string]any{"plugins": []any{row}}
		got := findPluginRow(top, "wanted")
		if got == nil {
			t.Fatalf("got=nil; want row")
		}
		if got["version"] != "1.0.0" {
			t.Errorf("got version=%v; want 1.0.0", got["version"])
		}
	})
}
