// Package mcp — acceptance tests for gum-8h46 (admin tuning surface).
//
// Spec §9.4 defines an admin-tuning layer that overrides the hardcoded
// defaults for the gum.search_apis implicit profile. The keys live
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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
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
// returns the spec §9.4 defaults: k=5, default_chars=120, MaxItems=k.
func TestMetaToolAdminTuningDefaultsWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	tuning := loadSearchAPIsTuning("default", nil)
	if tuning.k != 5 {
		t.Errorf("tuning.k = %d; want 5 (spec §9.4 default)", tuning.k)
	}
	if tuning.defaultChars != 120 {
		t.Errorf("tuning.defaultChars = %d; want 120 (spec §9.4 default)", tuning.defaultChars)
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
// default_chars falls back to the spec §9.4 default of 120 rather than 0,
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
// tracking the caller's k (spec §9.4: "max_items = k"). Treating the bad
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

// ---- gum.describe_op admin tuning (spec §9.4) -------------------------------
//
// §9.4 documented meta_tools.describe_op.max_variants and
// meta_tools.describe_op.max_chars, plus a truncate_strings default of 400,
// while nothing in internal/mcp read either key and handleDescribeOp returned
// the untruncated struct. The tests below pin both knobs and the default.

// TestMetaToolAdminTuningDescribeOpMaxVariants verifies that
// meta_tools.describe_op.max_variants overrides the default cap of 5.
func TestMetaToolAdminTuningDescribeOpMaxVariants(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{describeOpMaxVariantsKey: "12"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	if got := loadDescribeOpTuning("default", nil).maxVariants; got != 12 {
		t.Errorf("maxVariants = %d; want 12", got)
	}
}

// TestMetaToolAdminTuningDescribeOpMaxChars verifies that
// meta_tools.describe_op.max_chars overrides the default of 400.
func TestMetaToolAdminTuningDescribeOpMaxChars(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{describeOpMaxCharsKey: "1200"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	if got := loadDescribeOpTuning("default", nil).maxChars; got != 1200 {
		t.Errorf("maxChars = %d; want 1200", got)
	}
}

// TestMetaToolAdminTuningClampsDescribeOp pins both §9.4 ranges at each edge.
// An unparseable value keeps the spec default rather than zeroing the knob: a
// maxChars of 0 disables truncation and a maxVariants of 0 returns every
// variant, so a typo would silently widen the response.
func TestMetaToolAdminTuningClampsDescribeOp(t *testing.T) {
	cases := []struct {
		name              string
		key, raw          string
		wantVar, wantChar int
	}{
		{"variants below range", describeOpMaxVariantsKey, "0", describeOpMaxVariantsMin, defaultDescribeOpMaxChars},
		{"variants above range", describeOpMaxVariantsKey, "500", describeOpMaxVariantsMax, defaultDescribeOpMaxChars},
		{"variants unparseable", describeOpMaxVariantsKey, "many", defaultMaxVariants, defaultDescribeOpMaxChars},
		{"chars below range", describeOpMaxCharsKey, "10", defaultMaxVariants, describeOpMaxCharsMin},
		{"chars above range", describeOpMaxCharsKey, "9000", defaultMaxVariants, describeOpMaxCharsMax},
		{"chars unparseable", describeOpMaxCharsKey, "wide", defaultMaxVariants, defaultDescribeOpMaxChars},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			c := &config.Config{Values: map[string]string{tc.key: tc.raw}}
			if err := config.Save("default", c); err != nil {
				t.Fatalf("config.Save: %v", err)
			}

			got := loadDescribeOpTuning("default", nil)
			if got.maxVariants != tc.wantVar {
				t.Errorf("maxVariants = %d; want %d", got.maxVariants, tc.wantVar)
			}
			if got.maxChars != tc.wantChar {
				t.Errorf("maxChars = %d; want %d", got.maxChars, tc.wantChar)
			}
		})
	}
}

// TestMetaToolAdminTuningDescribeOpDefaultsWhenAbsent pins the §9.4 defaults an
// empty config resolves to.
func TestMetaToolAdminTuningDescribeOpDefaultsWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got := loadDescribeOpTuning("default", nil)
	if got.maxVariants != defaultMaxVariants {
		t.Errorf("maxVariants = %d; want %d", got.maxVariants, defaultMaxVariants)
	}
	if got.maxChars != defaultDescribeOpMaxChars {
		t.Errorf("maxChars = %d; want %d", got.maxChars, defaultDescribeOpMaxChars)
	}
}

// TestDescribeOpTruncatesStringsAtDefault runs the handler on an op whose
// summary is 900 characters and asserts the §9.4 truncate_strings stage fired
// on the wire, not just in the struct.
func TestDescribeOpTruncatesStringsAtDefault(t *testing.T) {
	op := makeMinimalOp("test.op.long", 1)
	op.Summary = strings.Repeat("s", 900)
	op.Title = strings.Repeat("t", 900)

	got := callDescribeOp(t, makeMinimalCatalog(op), "test.op.long")

	for _, field := range []string{"summary", "title"} {
		s, ok := got[field].(string)
		if !ok {
			t.Fatalf("%s is not a string; got %T", field, got[field])
		}
		if n := len([]rune(s)); n != defaultDescribeOpMaxChars {
			t.Errorf("%s length = %d runes; want %d (§9.4 truncate_strings.default_chars)",
				field, n, defaultDescribeOpMaxChars)
		}
		if !strings.HasSuffix(s, "…") {
			t.Errorf("%s does not end in the truncation ellipsis: %q", field, s[max(0, len(s)-8):])
		}
	}
}

// TestDescribeOpHonoursTunedMaxChars proves the admin key reaches the wire: the
// same 900-character summary survives to 800 runes when max_chars says 800.
func TestDescribeOpHonoursTunedMaxChars(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{describeOpMaxCharsKey: "800"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	op := makeMinimalOp("test.op.long", 1)
	op.Summary = strings.Repeat("s", 900)

	got := callDescribeOpInEnvConfig(t, makeMinimalCatalog(op), "test.op.long")

	summary, ok := got["summary"].(string)
	if !ok {
		t.Fatalf("summary is not a string; got %T", got["summary"])
	}
	if n := len([]rune(summary)); n != 800 {
		t.Errorf("summary length = %d runes; want 800 (tuned max_chars)", n)
	}
}

// TestDescribeOpHonoursTunedMaxVariants proves the second key reaches the wire.
// The op carries 7 variants and the config caps the array at 2.
func TestDescribeOpHonoursTunedMaxVariants(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Values: map[string]string{describeOpMaxVariantsKey: "2"}}
	if err := config.Save("default", c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	got := callDescribeOpInEnvConfig(t, makeMinimalCatalog(makeMinimalOp("test.op.many", 7)), "test.op.many")

	variants, ok := got["variants"].([]any)
	if !ok {
		t.Fatalf("variants is not a JSON array; got %T", got["variants"])
	}
	if len(variants) != 2 {
		t.Errorf("variants length = %d; want 2 (tuned max_variants)", len(variants))
	}
	if oc, ok := got["variants_omitted_count"].(float64); !ok || int(oc) != 5 {
		t.Errorf("variants_omitted_count = %v; want 5 (7 - 2)", got["variants_omitted_count"])
	}
}

// TestTruncateDescribeOpResultLeavesCatalogAlone is the aliasing guard.
// buildDescribeOpResult assigns the catalog's own Scopes slice into the result,
// so a truncation that wrote through the alias would shorten the loaded
// catalog's scopes for every later request in the process.
func TestTruncateDescribeOpResultLeavesCatalogAlone(t *testing.T) {
	longScope := "https://www.googleapis.com/auth/" + strings.Repeat("z", 200)
	op := makeMinimalOp("test.op.scope", 1)
	op.Variants[0].Scopes = []string{longScope}

	res := truncateDescribeOpResult(buildDescribeOpResult(&op, defaultMaxVariants), 100)

	if got := op.Variants[0].Scopes[0]; got != longScope {
		t.Errorf("catalog scope was rewritten to %q; the truncation must clone before writing", got)
	}
	if n := len([]rune(res.Scopes[0])); n != 100 {
		t.Errorf("result scope length = %d runes; want 100", n)
	}
}

// TestTruncateDescribeOpResultKeepsEmptyUnsupportedList pins the §13 tri-state:
// a partial variant declaring no blocking atoms reports an empty array, and the
// truncation pass must not turn that present-and-empty list into null.
func TestTruncateDescribeOpResultKeepsEmptyUnsupportedList(t *testing.T) {
	op := makeMinimalOp("test.op.partial", 1)
	op.Variants[0].ExecutionSupport = catalog.ExecutionSupportPartial

	res := truncateDescribeOpResult(buildDescribeOpResult(&op, defaultMaxVariants), defaultDescribeOpMaxChars)

	if res.UnsupportedCapabilities == nil {
		t.Fatal("UnsupportedCapabilities = nil after truncation; §13 requires the field on a non-full branch")
	}
	if len(*res.UnsupportedCapabilities) != 0 {
		t.Errorf("UnsupportedCapabilities = %v; want empty", *res.UnsupportedCapabilities)
	}
}

// TestTruncateDescribeOpResultCoversEveryStringField walks the truncated result
// with reflection and fails on any string above the limit. The field-by-field
// pass above only proves the fields it names; a field added to
// describeOpResult later would otherwise ship untruncated.
func TestTruncateDescribeOpResultCoversEveryStringField(t *testing.T) {
	const limit = 100
	long := strings.Repeat("q", 400)

	op := makeMinimalOp(long, 2)
	op.Title = long
	op.Summary = long
	op.DefaultVariantID = long
	op.Variants[0].VariantID = long
	op.Variants[0].Scopes = []string{long, long}
	op.Variants[0].OutputProfile = long
	op.Variants[0].ExecutionSupport = catalog.ExecutionSupportPartial
	op.Variants[0].UnsupportedCapabilities = []string{long}
	op.Variants[0].RiskOverride = true
	op.Variants[0].RiskOverrideReason = long
	op.Variants[0].Binding = &catalog.Binding{RequestRef: long, ResponseRef: long}

	res := truncateDescribeOpResult(buildDescribeOpResult(&op, defaultMaxVariants), limit)

	var over []string
	var walk func(path string, v reflect.Value)
	walk = func(path string, v reflect.Value) {
		switch v.Kind() {
		case reflect.String:
			if n := len([]rune(v.String())); n > limit {
				over = append(over, fmt.Sprintf("%s (%d runes)", path, n))
			}
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(path, v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(fmt.Sprintf("%s[%d]", path, i), v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(fmt.Sprintf("%s[%v]", path, k), v.MapIndex(k))
			}
		case reflect.Struct:
			for i := range v.NumField() {
				walk(path+"."+v.Type().Field(i).Name, v.Field(i))
			}
		}
	}
	walk("result", reflect.ValueOf(res))

	if len(over) > 0 {
		t.Errorf("%d string(s) above the %d-rune §9.4 limit: %s",
			len(over), limit, strings.Join(over, ", "))
	}
}
