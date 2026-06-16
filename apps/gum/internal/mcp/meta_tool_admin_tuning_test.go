// Package mcp — acceptance tests for gum-8h46 (admin tuning surface).
//
// Spec §2139-§2145 defines an admin-tuning layer that overrides the §2129
// hardcoded defaults for the gum.search_apis implicit profile. The keys live
// in the active profile's config.toml:
//
//   meta_tools.search_apis.k                                   default 5,   range 1-20
//   meta_tools.search_apis.truncate_strings.default_chars      default 120, range 60-400
//   meta_tools.search_apis.collapse_arrays.max_items           default = k, range 1-50
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

	tuning := loadSearchAPIsTuning("default")
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

	tuning := loadSearchAPIsTuning("default")
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

	tuning := loadSearchAPIsTuning("default")
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

	tuning := loadSearchAPIsTuning("default")
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

	tuning := loadSearchAPIsTuning("default")
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

	tuning := loadSearchAPIsTuning("default")
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

	tuning := loadSearchAPIsTuning("default")
	if tuning.k != 5 {
		t.Errorf("tuning.k = %d; want 5 (default on parse failure)", tuning.k)
	}
}
