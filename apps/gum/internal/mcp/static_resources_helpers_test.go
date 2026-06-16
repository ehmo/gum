// Package mcp — coverage for static_resources.go / plugin_resource.go
// pure helpers: credentialDescriptors, intFromRow, profilePluginDir.
// Each driver targets one helper directly; no MCP transport involved.
package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCredentialDescriptors covers the four branches:
//   - nil row → nil
//   - missing/empty descriptor slice → nil
//   - mixed slice items (some maps, some scrap) → only maps pass through
//   - happy path: only the four whitelisted fields survive; env (raw env
//     name, spec §13 line 3165) MUST NOT pass through.
func TestCredentialDescriptors(t *testing.T) {
	t.Run("nil_row", func(t *testing.T) {
		if got := credentialDescriptors(nil); got != nil {
			t.Errorf("got %v; want nil", got)
		}
	})

	t.Run("missing_key_returns_nil", func(t *testing.T) {
		if got := credentialDescriptors(map[string]any{}); got != nil {
			t.Errorf("got %v; want nil", got)
		}
	})

	t.Run("empty_slice_returns_nil", func(t *testing.T) {
		row := map[string]any{"credential_descriptors": []any{}}
		if got := credentialDescriptors(row); got != nil {
			t.Errorf("got %v; want nil", got)
		}
	})

	t.Run("non_map_items_skipped", func(t *testing.T) {
		row := map[string]any{"credential_descriptors": []any{
			"junk",
			42,
			map[string]any{"alias": "ok", "env": "GUM_SECRET"},
		}}
		out := credentialDescriptors(row)
		if len(out) != 1 {
			t.Fatalf("len=%d; want 1 (non-maps skipped)", len(out))
		}
		safe := out[0].(map[string]any)
		if _, has := safe["env"]; has {
			t.Errorf("env leaked: %v", safe)
		}
	})

	t.Run("strips_env_passes_whitelist", func(t *testing.T) {
		row := map[string]any{"credential_descriptors": []any{
			map[string]any{
				"alias":        "session",
				"env":          "GUM_SECRET", // raw env name — must be stripped
				"kind":         "session",
				"display_name": "Session",
				"setup_hint":   "see docs",
				"private":      "must_strip",
			},
		}}
		out := credentialDescriptors(row)
		if len(out) != 1 {
			t.Fatalf("len=%d; want 1", len(out))
		}
		safe := out[0].(map[string]any)
		if _, has := safe["env"]; has {
			t.Errorf("env field leaked into resource output: %v", safe)
		}
		if _, has := safe["private"]; has {
			t.Errorf("unknown field leaked: %v", safe)
		}
		for _, want := range []string{"alias", "kind", "display_name", "setup_hint"} {
			if _, has := safe[want]; !has {
				t.Errorf("whitelist field %q missing: %v", want, safe)
			}
		}
	})
}

// TestIntFromRow covers the four numeric type branches plus the nil-row
// guard and the wrong-type fallthrough. Source rows can arrive as int
// (Go-built), int64 (some adapters), or float64 (default JSON decode).
func TestIntFromRow(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		key  string
		want int
	}{
		{"nil_row", nil, "x", 0},
		{"int", map[string]any{"x": 3}, "x", 3},
		{"int64", map[string]any{"x": int64(4)}, "x", 4},
		{"float64", map[string]any{"x": float64(5)}, "x", 5},
		{"missing_key", map[string]any{}, "x", 0},
		{"wrong_type_returns_zero", map[string]any{"x": "str"}, "x", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intFromRow(tc.row, tc.key); got != tc.want {
				t.Errorf("got %d; want %d", got, tc.want)
			}
		})
	}
}

// TestIntFromTop is the thin top-level wrapper. It must return 0 for nil
// and otherwise delegate to intFromRow.
func TestIntFromTop(t *testing.T) {
	if got := intFromTop(nil, "k"); got != 0 {
		t.Errorf("nil top got %d; want 0", got)
	}
	if got := intFromTop(map[string]any{"k": 7}, "k"); got != 7 {
		t.Errorf("got %d; want 7", got)
	}
}

// TestProfilePluginDir covers all three branches: XDG_DATA_HOME set wins;
// XDG empty → derives from HOME via UserHomeDir; profile default fallback
// when s.profile is empty. The unrecoverable HOME=missing path is hard
// to hit on a real test runner (UserHomeDir defers to OS APIs) so we
// trust the err-branch by inspection.
func TestProfilePluginDir(t *testing.T) {
	t.Run("xdg_set_wins", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/xdg")
		s := &Server{profile: "prod"}
		if got := s.profilePluginDir(); got != filepath.Join("/xdg", "gum", "prod") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("xdg_empty_uses_home_default_profile", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		// Force a stable HOME so we don't depend on the runner's user.
		t.Setenv("HOME", "/home/test")
		s := &Server{} // empty profile → "default"
		want := filepath.Join("/home/test", ".local", "share", "gum", "default")
		if got := s.profilePluginDir(); got != want {
			t.Errorf("got %q; want %q", got, want)
		}
	})

	t.Run("xdg_empty_home_missing_returns_empty", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		// Unset HOME so os.UserHomeDir returns an error on Unix.
		// Cross-platform note: on Windows USERPROFILE drives this; skip there.
		if os.Getenv("USERPROFILE") != "" {
			t.Skip("Windows USERPROFILE makes UserHomeDir succeed")
		}
		t.Setenv("HOME", "")
		s := &Server{profile: "p"}
		if got := s.profilePluginDir(); got != "" {
			t.Errorf("got %q; want empty when HOME unset", got)
		}
	})
}
