// Package mcp — acceptance tests for gum-8h46 (admin tuning surface).
//
// Spec §2139-§2145 defines an admin-tuning layer that overrides the §2129
// hardcoded defaults for the gum.search_apis implicit profile. The keys live
// in the active profile's config.toml:
//
//	meta_tools.search_apis.k                                   default 5,   range 1-20
//	meta_tools.search_apis.truncate_strings.default_chars      default 120, range 60-400
//	meta_tools.search_apis.collapse_arrays.max_items           default = k, range 1-50
//
// Out-of-range values are clamped at runtime; clamping logs a warning so
// operators learn their setting was rejected.
package mcp

import (
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TestMetaToolAdminTuningSearchAPIsK verifies that meta_tools.search_apis.k
// in the active profile's config overrides the default k=5.
func TestMetaToolAdminTuningSearchAPIsK(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{"meta_tools.search_apis.k": "12"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.k != 12 {
		t.Errorf("tuning.k = %d; want 12 (from config)", tuning.k)
	}
}

// TestMetaToolAdminTuningClampsK verifies that an out-of-range k is clamped
// to the documented bound (k=99 → 20) and that the clamped value is the one
// used by searchAPIsProfile.
func TestMetaToolAdminTuningClampsK(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{"meta_tools.search_apis.k": "99"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.k != 20 {
		t.Errorf("tuning.k = %d; want 20 (clamped to max)", tuning.k)
	}
}

// TestMetaToolAdminTuningTruncateChars verifies the truncate_strings.default_chars
// admin override threads through to searchAPIsProfile.
func TestMetaToolAdminTuningTruncateChars(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{
		"meta_tools.search_apis.truncate_strings.default_chars": "200",
	}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	prof := searchAPIsProfile(5, tuning)
	if prof.TruncateStrings == nil {
		t.Fatal("TruncateStrings is nil")
	}
	if prof.TruncateStrings.DefaultChars != 200 {
		t.Errorf("DefaultChars = %d; want 200 (admin override)", prof.TruncateStrings.DefaultChars)
	}
}

// TestMetaToolAdminTuningClampsTruncateChars verifies an out-of-range
// default_chars is clamped (1000 → 400).
func TestMetaToolAdminTuningClampsTruncateChars(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{
		"meta_tools.search_apis.truncate_strings.default_chars": "1000",
	}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.defaultChars != 400 {
		t.Errorf("defaultChars = %d; want 400 (clamped to max)", tuning.defaultChars)
	}
}

// TestMetaToolAdminTuningCollapseMaxItems verifies that the
// collapse_arrays.max_items override unbinds it from k.
func TestMetaToolAdminTuningCollapseMaxItems(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{
		"meta_tools.search_apis.collapse_arrays.max_items": "30",
	}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	prof := searchAPIsProfile(5, tuning) // caller k=5, but admin override → 30
	if prof.CollapseArrays == nil {
		t.Fatal("CollapseArrays is nil")
	}
	if prof.CollapseArrays.MaxItems != 30 {
		t.Errorf("CollapseArrays.MaxItems = %d; want 30 (admin override unbinds k)",
			prof.CollapseArrays.MaxItems)
	}
}

// TestMetaToolAdminTuningDefaultsWhenAbsent verifies that an empty config
// returns the spec §2129 defaults: k=5, default_chars=120, MaxItems=k.
func TestMetaToolAdminTuningDefaultsWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.k != 5 {
		t.Errorf("tuning.k = %d; want 5 (spec §2129 default)", tuning.k)
	}
	if tuning.defaultChars != 120 {
		t.Errorf("tuning.defaultChars = %d; want 120 (spec §2129 default)", tuning.defaultChars)
	}
	if tuning.maxItemsBound {
		t.Error("tuning.maxItemsBound = true; want false (no override)")
	}
}

// TestMetaToolAdminTuningUnparseable verifies that a non-integer value
// falls back to the default (and does not crash).
func TestMetaToolAdminTuningUnparseable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{"meta_tools.search_apis.k": "not-an-int"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.k != 5 {
		t.Errorf("tuning.k = %d; want 5 (default on parse failure)", tuning.k)
	}
}

// TestMetaToolAdminTuningClampsCollapseMaxItems drives
// collapse_arrays.max_items outside its documented 1-50 range in both
// directions. The existing override test only used 30, which is inside the
// range, so neither clamp bound was exercised.
func TestMetaToolAdminTuningClampsCollapseMaxItems(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"above max", "500", 50},
		{"below min", "0", 1},
		{"negative", "-7", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			c := &config.Config{Values: map[string]string{
				"meta_tools.search_apis.collapse_arrays.max_items": tc.raw,
			}}
			if err := config.Save("default", c); err != nil {
				t.Fatalf("config.Save: %v", err)
			}

			tuning := loadSearchAPIsTuning("default", nil)
			if !tuning.maxItemsBound {
				t.Fatal("maxItemsBound = false; want true (key is present)")
			}
			if tuning.maxItems != tc.want {
				t.Errorf("maxItems = %d; want %d (clamped)", tuning.maxItems, tc.want)
			}
			prof := searchAPIsProfile(5, tuning)
			if prof.CollapseArrays == nil {
				t.Fatal("CollapseArrays is nil")
			}
			if prof.CollapseArrays.MaxItems != tc.want {
				t.Errorf("CollapseArrays.MaxItems = %d; want %d", prof.CollapseArrays.MaxItems, tc.want)
			}
		})
	}
}

// TestMetaToolAdminTuningUnparseableTruncateChars verifies a non-integer
// default_chars falls back to the spec §2129 default of 120 rather than 0,
// which would disable truncation entirely.
func TestMetaToolAdminTuningUnparseableTruncateChars(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{
		"meta_tools.search_apis.truncate_strings.default_chars": "wide",
	}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.defaultChars != 120 {
		t.Errorf("defaultChars = %d; want 120 (default on parse failure)", tuning.defaultChars)
	}
	prof := searchAPIsProfile(5, tuning)
	if prof.TruncateStrings.DefaultChars != 120 {
		t.Errorf("DefaultChars = %d; want 120", prof.TruncateStrings.DefaultChars)
	}
}

// TestMetaToolAdminTuningUnparseableCollapseMaxItems verifies a non-integer
// max_items leaves the knob unbound, so collapse_arrays.max_items keeps
// tracking the caller's k (spec §2129: "max_items = k"). Treating the bad
// value as an override would silently shrink a k=12 request to the config
// default.
func TestMetaToolAdminTuningUnparseableCollapseMaxItems(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{
		"meta_tools.search_apis.collapse_arrays.max_items": "lots",
	}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.maxItemsBound {
		t.Error("maxItemsBound = true; want false (unparseable value is not an override)")
	}

	const requestK = 12
	prof := searchAPIsProfile(requestK, tuning)
	if prof.CollapseArrays == nil {
		t.Fatal("CollapseArrays is nil")
	}
	if prof.CollapseArrays.MaxItems != requestK {
		t.Errorf("CollapseArrays.MaxItems = %d; want %d (caller k, override ignored)",
			prof.CollapseArrays.MaxItems, requestK)
	}
}
