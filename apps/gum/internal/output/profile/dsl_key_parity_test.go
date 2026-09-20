// dsl_key_parity_test.go — the parser's key set against the published schema.
//
// gum-tthf: five keys parsed in Go (sort_by, limit, projection,
// flatten_singletons, omit_zero_counts) appeared in neither
// docs/expression-profile-dsl.md nor docs/expression-profile-dsl.json, whose
// $defs/profile sets additionalProperties:false — so a profile that used one
// parsed in Go and failed schema validation. field_mask ran the other way: the
// schema and the spec field table declare it, the parser had no case for it.
package profile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// dslKeys is every key the profile DSL defines, with a TOML value and the
// matching JSON value. Both halves of the table must be accepted: the parser
// loads the TOML, the published schema validates the JSON.
var dslKeys = []struct {
	key  string
	toml string
	json any
}{
	{"format", `format = "json"`, "json"},
	{"field_mask", `field_mask = "results.text"`, "results.text"},
	{"field_mask_mode", `field_mask_mode = "none"`, "none"},
	{"keep_fields", `keep_fields = ["a.b"]`, []any{"a.b"}},
	{"drop_fields", `drop_fields = ["a.c"]`, []any{"a.c"}},
	{"strip_nulls", `strip_nulls = true`, true},
	{"flatten", `flatten = true`, true},
	{"collapse_arrays", `collapse_arrays = { max_items = 20 }`, map[string]any{"max_items": 20}},
	{"truncate_strings", `truncate_strings = { default_chars = 80 }`, map[string]any{"default_chars": 80}},
	{"dedupe", `dedupe = { by = ["id"] }`, map[string]any{"by": []any{"id"}}},
	{"recovery", `recovery = "local_artifact"`, "local_artifact"},
	{"inherits", `inherits = "_base.list_ops"`, "_base.list_ops"},
	{"on_empty", `on_empty = "Nothing found."`, "Nothing found."},
	{"tee_mode", `tee_mode = "always"`, "always"},
	{"projection", `projection = ["id"]`, []any{"id"}},
	{"flatten_singletons", `flatten_singletons = true`, true},
	{"omit_zero_counts", `omit_zero_counts = true`, true},
	{"sort_by", `sort_by = "date"`, "date"},
	{"limit", `limit = 50`, 50},
}

// TestParserAcceptsEveryDSLKey walks the table from the parser side.
func TestParserAcceptsEveryDSLKey(t *testing.T) {
	for _, tc := range dslKeys {
		src := tc.toml + "\n"
		if tc.key == "collapse_arrays" || tc.key == "inherits" {
			src = tc.toml + "\non_empty = \"Nothing found.\"\n"
		}
		if _, err := profile.Parse(src); err != nil {
			t.Errorf("Parse(%s): %v", tc.key, err)
		}
	}
}

// TestSchemaAcceptsEveryDSLKey walks the same table from the schema side.
func TestSchemaAcceptsEveryDSLKey(t *testing.T) {
	for _, tc := range dslKeys {
		raw, err := json.Marshal(map[string]any{
			"output_profiles": map[string]any{"p": map[string]any{tc.key: tc.json}},
		})
		if err != nil {
			t.Fatalf("marshal %s: %v", tc.key, err)
		}
		if err := profile.ValidateRawProfileFile(raw); err != nil {
			t.Errorf("ValidateRawProfileFile(%s): %v", tc.key, err)
		}
	}
}

// TestParseReadsFieldMask pins the key the spec field table calls stage 1.
func TestParseReadsFieldMask(t *testing.T) {
	p, err := profile.Parse("field_mask = \"results.text,results.metrics\"\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.FieldMask != "results.text,results.metrics" {
		t.Errorf("FieldMask = %q", p.FieldMask)
	}
}

// TestSerializeRoundTripsFieldMask keeps the writer and the reader in step.
func TestSerializeRoundTripsFieldMask(t *testing.T) {
	src := &profile.Profile{FieldMask: "results.text", DefaultFormat: "json"}
	out := src.Serialize()
	if !strings.Contains(out, `field_mask = "results.text"`) {
		t.Fatalf("Serialize() = %q; want a field_mask line", out)
	}
	back, err := profile.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Serialize()): %v", err)
	}
	if back.FieldMask != src.FieldMask {
		t.Errorf("round trip FieldMask = %q; want %q", back.FieldMask, src.FieldMask)
	}
}

// TestMergeProfilesInheritsFieldMask: an undeclared key falls through to the
// lower-precedence layer, like every other profile field (spec §9.3).
func TestMergeProfilesInheritsFieldMask(t *testing.T) {
	high := &profile.Profile{DefaultFormat: "json"}
	low := &profile.Profile{FieldMask: "results.text"}
	merged := profile.MergeProfiles(high, low)
	if merged.FieldMask != "results.text" {
		t.Errorf("merged FieldMask = %q; want the base's value", merged.FieldMask)
	}
}

// TestReferenceDocExamplesHold runs the input/output pairs printed in
// docs/profile-dsl-reference.md §2 for the five operators that doc gained with
// gum-tthf. A printed example a reader copies is a claim about the runtime.
func TestReferenceDocExamplesHold(t *testing.T) {
	cases := []struct {
		name string
		p    *profile.Profile
		in   string
		want string
	}{
		{
			name: "2.2 projection",
			p:    &profile.Profile{DefaultFormat: "json", Projection: []string{"messages", "nextPageToken"}},
			in:   `{"messages":[{"id":"m1"}],"nextPageToken":"t","resultSizeEstimate":1}`,
			want: `{"messages":[{"id":"m1"}],"nextPageToken":"t"}`,
		},
		{
			name: "2.6 flatten_singletons",
			p:    &profile.Profile{DefaultFormat: "json", Flatten: true, FlattenSingletons: true},
			in:   `{"items":[{"id":"e1"}]}`,
			want: `{"id":"e1"}`,
		},
		{
			name: "2.10 sort_by",
			p:    &profile.Profile{DefaultFormat: "json", SortBy: "ts"},
			in:   `[{"id":"b","ts":2},{"id":"a","ts":1}]`,
			want: `[{"id":"a","ts":1},{"id":"b","ts":2}]`,
		},
	}
	for _, tc := range cases {
		out, err := profile.Apply(tc.p, profile.ApplyInput{Body: []byte(tc.in)})
		if err != nil {
			t.Errorf("%s: Apply: %v", tc.name, err)
			continue
		}
		if string(out.Body) != tc.want {
			t.Errorf("%s: body = %s; the doc prints %s", tc.name, out.Body, tc.want)
		}
	}
}

// TestOmitZeroCountsIsAToonOnlyOption pins the scope §2.13 states: the option
// changes the TOON bytes and leaves every other encoder alone.
func TestOmitZeroCountsIsAToonOnlyOption(t *testing.T) {
	const body = `{"clicks":0,"impressions":7}`

	toonOut, err := profile.Apply(
		&profile.Profile{DefaultFormat: "toon", OmitZeroCounts: true},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply (toon): %v", err)
	}
	if strings.Contains(string(toonOut.Body), "clicks") {
		t.Errorf("toon body = %q; omit_zero_counts drops the zero field", toonOut.Body)
	}

	jsonOut, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", OmitZeroCounts: true},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply (json): %v", err)
	}
	if !strings.Contains(string(jsonOut.Body), `"clicks":0`) {
		t.Errorf("json body = %q; omit_zero_counts is a toon-only option", jsonOut.Body)
	}
}

// TestLimitKeepsTheTopNBySortKey is the §2.11 and §4 ordering claim: sort_by
// runs first, so limit keeps the top N by that key, not the first N upstream
// sent.
func TestLimitKeepsTheTopNBySortKey(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", SortBy: "ts", Limit: 2},
		profile.ApplyInput{Body: []byte(`{"items":[{"ts":9},{"ts":1},{"ts":5}]}`)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(string(out.Body), `{"ts":1}`) || !strings.Contains(string(out.Body), `{"ts":5}`) {
		t.Errorf("body = %s; want the two lowest ts values", out.Body)
	}
	if strings.Contains(string(out.Body), `{"ts":9}`) {
		t.Errorf("body = %s; ts=9 sorts last and limit=2 removes it", out.Body)
	}
}

// TestSortByPutsMissingKeysFirst pins the §2.10 edge the docs state: a row
// without the sort key sorts as null, which is ahead of every present value,
// and a non-object element is never reordered.
func TestSortByPutsMissingKeysFirst(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", SortBy: "ts"},
		profile.ApplyInput{Body: []byte(`{"items":[{"id":"a","ts":5},{"id":"b"},{"id":"c","ts":1},"scalar"]}`)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := `{"items":[{"id":"b"},{"id":"c","ts":1},{"id":"a","ts":5},"scalar"]}`
	if string(out.Body) != want {
		t.Errorf("body = %s; want %s", out.Body, want)
	}
}
