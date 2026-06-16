package profile

import (
	"strings"
	"testing"
)

// TestParseTestFixtureKeyErrorBranches drives the per-key error paths
// in parseTestFixtureKey so a regression that silently accepts a
// malformed [[tests]] block (e.g. missing-quote name, negative ceiling,
// unknown key) is caught.
func TestParseTestFixtureKeyErrorBranches(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		rawVal  string
		wantSub string
	}{
		{"name_bad_literal", "name", `unquoted`, "tests.name:"},
		{"profile_bad_literal", "profile", `unquoted`, "tests.profile:"},
		{"fixture_bad_literal", "fixture", `unquoted`, "tests.fixture:"},
		{"expect_format_bad_literal", "expect_format", `unquoted`, "tests.expect_format:"},
		{"expect_format_invalid_enum", "expect_format", `"yaml"`, "must be toon, json, or raw"},
		{"expect_max_tokens_not_int", "expect_max_tokens", `abc`, "tests.expect_max_tokens:"},
		{"expect_max_tokens_negative", "expect_max_tokens", `-1`, "must be >= 0"},
		{"expect_lossy_not_bool", "expect_lossy", `maybe`, "tests.expect_lossy:"},
		{"expect_result_count_not_int", "expect_result_count", `xx`, "tests.expect_result_count:"},
		{"expect_omitted_count_not_int", "expect_omitted_count", `yy`, "tests.expect_omitted_count:"},
		{"expect_fields_bad_array", "expect_fields", `not-an-array`, "tests.expect_fields:"},
		{"unknown_key", "expect_universe", `42`, "unknown key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f TestFixture
			err := parseTestFixtureKey(&f, tc.key, tc.rawVal, 7)
			if err == nil {
				t.Fatalf("want error for %s=%q; got nil", tc.key, tc.rawVal)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err=%v; want substring %q", err, tc.wantSub)
			}
			if !strings.Contains(err.Error(), "line 7") {
				t.Errorf("err=%v; want line-7 attribution", err)
			}
		})
	}
}

// TestParseTestFixtureKeyHappyValues pins the success branches: each
// supported key assigns the right field with the right type.
func TestParseTestFixtureKeyHappyValues(t *testing.T) {
	var f TestFixture
	steps := []struct {
		key, raw string
	}{
		{"name", `"my fix"`},
		{"profile", `"compact"`},
		{"fixture", `"path.json"`},
		{"expect_format", `"toon"`},
		{"expect_max_tokens", `1024`},
		{"expect_lossy", `true`},
		{"expect_result_count", `3`},
		{"expect_omitted_count", `0`},
		{"expect_fields", `["id","name"]`},
	}
	for _, s := range steps {
		if err := parseTestFixtureKey(&f, s.key, s.raw, 1); err != nil {
			t.Fatalf("%s=%q: %v", s.key, s.raw, err)
		}
	}
	if f.Name != "my fix" || f.Profile != "compact" || f.Fixture != "path.json" {
		t.Errorf("string fields wrong: %+v", f)
	}
	if f.ExpectFormat != "toon" || f.ExpectMaxTokens != 1024 {
		t.Errorf("format/tokens wrong: %+v", f)
	}
	if !f.ExpectLossy || !f.ExpectLossySet {
		t.Errorf("lossy flags wrong: %+v", f)
	}
	if f.ExpectResultCount != 3 || !f.ExpectResultCountSet {
		t.Errorf("result_count wrong: %+v", f)
	}
	if f.ExpectOmittedCount != 0 || !f.ExpectOmittedCountSet {
		t.Errorf("omitted_count wrong: %+v", f)
	}
	if len(f.ExpectFields) != 2 || f.ExpectFields[0] != "id" {
		t.Errorf("fields wrong: %+v", f.ExpectFields)
	}
}
