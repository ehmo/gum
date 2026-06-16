package mcp

import (
	"log/slog"
	"strconv"

	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/output/profile"
)

// searchAPIsTuning captures the admin tuning knobs for gum.search_apis
// (spec §2139-§2145). Values come from the active profile's config; each
// knob has a default and a clamp range applied at request time. A clamped
// value emits one slog.Warn so operators learn their setting was rejected.
type searchAPIsTuning struct {
	k             int // collapse_arrays.max_items
	defaultChars  int // truncate_strings.default_chars
	maxItems      int // collapse_arrays.max_items override (rarely used; binds k)
	maxItemsBound bool
}

// loadSearchAPIsTuning reads spec §2139-§2145 admin keys from the active
// profile's config.toml and clamps each to its documented range. Missing or
// unparseable keys fall back to the spec defaults. Errors loading the config
// itself are non-fatal: handlers continue with defaults.
//
// Clamp ranges:
//   - meta_tools.search_apis.k                      default 5,   range 1-20
//   - meta_tools.search_apis.truncate_strings.default_chars  default 120, range 60-400
//   - meta_tools.search_apis.collapse_arrays.max_items       default = k (bound to k), range 1-50
func loadSearchAPIsTuning(activeProfile string) searchAPIsTuning {
	t := searchAPIsTuning{k: 5, defaultChars: 120}

	c, _, err := config.Load(activeProfile)
	if err != nil || c == nil {
		return t
	}

	if v, ok := c.Get("meta_tools.search_apis.k"); ok {
		t.k = clampInt("meta_tools.search_apis.k", v, t.k, 1, 20)
	}
	if v, ok := c.Get("meta_tools.search_apis.truncate_strings.default_chars"); ok {
		t.defaultChars = clampInt("meta_tools.search_apis.truncate_strings.default_chars", v, t.defaultChars, 60, 400)
	}
	if v, ok := c.Get("meta_tools.search_apis.collapse_arrays.max_items"); ok {
		t.maxItems = clampInt("meta_tools.search_apis.collapse_arrays.max_items", v, t.k, 1, 50)
		t.maxItemsBound = true
	}
	return t
}

// clampInt parses raw as an integer and clamps it to [lo, hi]. On parse
// failure, returns def. On out-of-range, emits one slog.Warn with the key
// and the clamped value, and returns the clamped value.
func clampInt(key, raw string, def, lo, hi int) int {
	v, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("admin tuning: unparseable integer; using default",
			"key", key, "raw", raw, "default", def)
		return def
	}
	if v < lo {
		slog.Warn("admin tuning: value below range; clamped",
			"key", key, "raw", v, "min", lo)
		return lo
	}
	if v > hi {
		slog.Warn("admin tuning: value above range; clamped",
			"key", key, "raw", v, "max", hi)
		return hi
	}
	return v
}

// searchAPIsProfile returns the spec §2129 implicit output profile for
// gum.search_apis. The caller's k binds CollapseArrays.MaxItems so result
// pages grow with the user request. Profile is NOT user-overridable
// (spec §9.4) — Tier A meta-tools carry hardcoded implicit profiles, but
// admin tuning keys (§2139-§2145) may override the defaults when present.
//
// Fields (spec §2129):
//   - default_format = "toon"
//   - collapse_arrays.max_items = k (or admin-tuned override when set)
//   - truncate_strings.default_chars = 120 (or admin-tuned)
//   - truncate_strings.fields.summary = 80
//   - on_empty = "No matching operations found. Try a broader query."
//   - recovery = "none"
func searchAPIsProfile(k int, tuning searchAPIsTuning) *profile.Profile {
	maxItems := k
	if tuning.maxItemsBound {
		maxItems = tuning.maxItems
	}
	defaultChars := tuning.defaultChars
	if defaultChars == 0 {
		defaultChars = 120
	}
	return &profile.Profile{
		DefaultFormat: "toon",
		CollapseArrays: &profile.CollapseArraysSpec{
			MaxItems: maxItems,
		},
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: defaultChars,
			Fields:       map[string]int{"summary": 80},
		},
		OnEmpty:  "No matching operations found. Try a broader query.",
		Recovery: "none",
	}
}
