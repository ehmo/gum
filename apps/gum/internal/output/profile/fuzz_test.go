package profile_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// FuzzParse asserts the expression-profile DSL parser never panics on arbitrary
// (untrusted) source. Profile expressions can come from user/agent input
// (--profile-expr, config), so a panic in Parse is a DoS. Errors are an
// acceptable outcome; a panic is not. Seeded with the DSL's real shapes
// (projection lists, truncation maps, [[tests]] sections, field-mask modes).
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		``,
		`format = "toon"`,
		`fields = ["id","from","subject"]`,
		`fields = {snippet=180}`,
		`field_mask_mode = "upstream"`,
		`collapse_arrays = true`,
		"format = \"toon\"\nfields = [\"id\"]\n",
		"[[tests]]\nname = \"t1\"\n",
		"[[tests]]\n[[tests]]\n",
		`unknown_field = "bad"`,
		`fields = [`,
		`= "novalue"`,
		`fields = {`,
		`[[`,
		"\x00 = \x01",
		`fields = ["a", "b", ]`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_, _ = profile.Parse(src) // must not panic on any input
	})
}
