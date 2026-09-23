package mcp

import (
	"log/slog"
	"strconv"

	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/output/profile"
)

// searchAPIsTuning captures the admin tuning knobs for gum.search_apis
// (spec §9.4). Values come from the active profile's config; each
// knob has a default and a clamp range applied at request time. A clamped
// value emits one slog.Warn so operators learn their setting was rejected.
type searchAPIsTuning struct {
	k             int // collapse_arrays.max_items
	defaultChars  int // truncate_strings.default_chars
	maxItems      int // collapse_arrays.max_items override (rarely used; binds k)
	maxItemsBound bool
}

// maxItemsKey is the admin key that overrides collapse_arrays.max_items. It
// is read and reported in two places, so it is named once.
const maxItemsKey = "meta_tools.search_apis.collapse_arrays.max_items"

// loadSearchAPIsTuning reads spec §9.4 admin keys from the active
// profile's config.toml and clamps each to its documented range. Missing or
// unparseable keys fall back to the spec defaults. Errors loading the config
// itself are non-fatal: handlers continue with defaults.
//
// Clamp ranges:
//   - meta_tools.search_apis.k                      default 5,   range 1-20
//   - meta_tools.search_apis.truncate_strings.default_chars  default 120, range 60-400
//   - meta_tools.search_apis.collapse_arrays.max_items       default = k (bound to k), range 1-50
//
// An unparseable max_items leaves the knob unbound, so the caller's k
// still decides collapse_arrays.max_items.
func loadSearchAPIsTuning(activeProfile string, log *slog.Logger) searchAPIsTuning {
	log = loggerOrDefault(log)
	t := searchAPIsTuning{k: 5, defaultChars: 120}

	c, _, err := config.Load(activeProfile)
	if err != nil || c == nil {
		return t
	}

	if v, ok := c.Get("meta_tools.search_apis.k"); ok {
		t.k = clampInt("meta_tools.search_apis.k", v, t.k, 1, 20, log)
	}
	if v, ok := c.Get("meta_tools.search_apis.truncate_strings.default_chars"); ok {
		t.defaultChars = clampInt("meta_tools.search_apis.truncate_strings.default_chars", v, t.defaultChars, 60, 400, log)
	}
	if v, ok := c.Get(maxItemsKey); ok {
		// An unparseable value must leave the knob unbound. Binding it to a
		// fallback would override the caller's k, so a k=12 request would
		// silently collapse to the config default.
		if n, parsed := clampIntOK(maxItemsKey, v, 1, 50, log); parsed {
			t.maxItems = n
			t.maxItemsBound = true
		} else {
			log.Warn("admin tuning: unparseable integer; ignoring override",
				"key", maxItemsKey, "raw", v)
		}
	}
	return t
}

// clampInt parses raw as an integer and clamps it to [lo, hi]. On parse
// failure, returns def. On out-of-range, emits one slog.Warn with the key
// and the clamped value, and returns the clamped value.
func clampInt(key, raw string, def, lo, hi int, log *slog.Logger) int {
	log = loggerOrDefault(log)
	v, ok := clampIntOK(key, raw, lo, hi, log)
	if !ok {
		log.Warn("admin tuning: unparseable integer; using default",
			"key", key, "raw", raw, "default", def)
		return def
	}
	return v
}

// clampIntOK parses raw and clamps it to [lo, hi], reporting false when raw
// is not an integer. It exists so a caller with no usable default can tell a
// clamped value from an absent one.
func clampIntOK(key, raw string, lo, hi int, log *slog.Logger) (int, bool) {
	log = loggerOrDefault(log)
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if v < lo {
		log.Warn("admin tuning: value below range; clamped",
			"key", key, "raw", v, "min", lo)
		return lo, true
	}
	if v > hi {
		log.Warn("admin tuning: value above range; clamped",
			"key", key, "raw", v, "max", hi)
		return hi, true
	}
	return v, true
}

// searchAPIsProfile returns the spec §9.4 implicit output profile for
// gum.search_apis. The caller's k binds CollapseArrays.MaxItems so result
// pages grow with the user request. Profile is NOT user-overridable
// (spec §9.4) — Tier A meta-tools carry hardcoded implicit profiles, but
// admin tuning keys (§9.4) may override the defaults when present.
//
// Fields (spec §9.4):
//   - format = "toon"
//   - collapse_arrays.max_items = k (or admin-tuned override when set)
//   - truncate_strings.default_chars = 120 (or admin-tuned)
//   - truncate_strings.fields.summary = 80
//   - on_empty = "No matching operations found. Try a broader query."
//   - recovery = "none"
//
// searchAPIsKMin and searchAPIsKMax are the spec §9.4 bounds for the
// gum.search_apis k argument, mirrored in the registered input schema.
const (
	searchAPIsKMin = 1
	searchAPIsKMax = 20
)

// searchAPIsProfileName is the profile name gum.search_apis reports in its
// §13 envelope.
const searchAPIsProfileName = "_meta.search_apis"

// SearchNoResultsMessage is the §9.4 gum.search_apis on_empty string. It is
// exported so `gum search` reports the same sentence for the same empty query;
// two copies drifted apart the first time either was reworded.
const SearchNoResultsMessage = "No matching operations found. Try a broader query."

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
		// §9.4 profiles are hardcoded and not overridable, so they have no
		// file to take a name from. The envelope still has to report one, and
		// an empty string would read as "no profile ran". The leading
		// underscore follows the "_raw" sentinel convention (§13).
		Name:          searchAPIsProfileName,
		DefaultFormat: "toon",
		CollapseArrays: &profile.CollapseArraysSpec{
			MaxItems: maxItems,
		},
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: defaultChars,
			Fields:       map[string]int{"summary": 80},
		},
		OnEmpty:  SearchNoResultsMessage,
		Recovery: "none",
	}
}

// loggerOrDefault resolves an injected logger to a usable one. The helpers
// above take the logger as a parameter rather than off a receiver, because
// they run before any Server method has a tuning value to hang onto; a nil
// argument from a direct in-package call falls back the same way the Server
// does (spec §14.1 rule 2).
func loggerOrDefault(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.Default()
}

// describeOpTuning captures the admin tuning knobs for gum.describe_op
// (spec §9.4): the variants[] collapse threshold and the string truncation
// limit. Both carry a spec default and a clamp range applied at request time.
type describeOpTuning struct {
	maxVariants int
	maxChars    int
}

// loadDescribeOpTuning reads the two §9.4 gum.describe_op admin keys from the
// active profile's config.toml and clamps each to its documented range.
// Missing, unparseable, or unloadable config falls back to the spec defaults.
//
// Clamp ranges:
//   - meta_tools.describe_op.max_variants  default 5,   range 1-50
//   - meta_tools.describe_op.max_chars     default 400, range 100-2000
func loadDescribeOpTuning(activeProfile string, log *slog.Logger) describeOpTuning {
	log = loggerOrDefault(log)
	t := describeOpTuning{maxVariants: defaultMaxVariants, maxChars: defaultDescribeOpMaxChars}

	c, _, err := config.Load(activeProfile)
	if err != nil || c == nil {
		return t
	}

	if v, ok := c.Get(describeOpMaxVariantsKey); ok {
		t.maxVariants = clampInt(describeOpMaxVariantsKey, v, t.maxVariants,
			describeOpMaxVariantsMin, describeOpMaxVariantsMax, log)
	}
	if v, ok := c.Get(describeOpMaxCharsKey); ok {
		t.maxChars = clampInt(describeOpMaxCharsKey, v, t.maxChars,
			describeOpMaxCharsMin, describeOpMaxCharsMax, log)
	}

	return t
}
