package genai

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestStringArgBranches covers nil args, present-string, missing-key, and
// wrong-type — the four shape outcomes the executor must tolerate from
// JSON-unmarshalled input.
func TestStringArgBranches(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		want string
	}{
		{"nil_args", nil, "k", ""},
		{"missing_key", map[string]any{}, "k", ""},
		{"present_string", map[string]any{"k": "v"}, "k", "v"},
		{"wrong_type_int", map[string]any{"k": 1}, "k", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringArg(tc.args, tc.key); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// TestBindingBranches covers the three nil-guard layers (nil rv, nil
// Variant, nil Binding) plus the happy path that surfaces AdapterKey.
// The error message in Execute leans on binding() — drift here would
// produce an empty adapter_key in operator-facing errors.
func TestBindingBranches(t *testing.T) {
	if got := binding(nil); got != "" {
		t.Errorf("nil rv: got %q; want \"\"", got)
	}
	if got := binding(&dispatch.ResolvedVariant{}); got != "" {
		t.Errorf("nil Variant: got %q; want \"\"", got)
	}
	if got := binding(&dispatch.ResolvedVariant{Variant: &catalog.Variant{}}); got != "" {
		t.Errorf("nil Binding: got %q; want \"\"", got)
	}
	if got := binding(&dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "genai.models.generate_content"}},
	}); got != "genai.models.generate_content" {
		t.Errorf("happy path: got %q", got)
	}
}

// TestGenaiOp covers the prefix-strip — non-genai keys return ""; a
// proper genai. prefix returns the tail. The Execute switch keys off
// this value so a typo would silently route everything to "default".
func TestGenaiOp(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		want    string
	}{
		{"empty_key", "", ""},
		{"missing_prefix", "models.generate_content", ""},
		{"just_prefix_too_short", "genai.", ""}, // len 6, prefix 6 — fails len > prefix
		{"genai_prefix_stripped", "genai.models.generate_content", "models.generate_content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rv := &dispatch.ResolvedVariant{
				Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: tc.key}},
			}
			if got := genaiOp(rv); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}
